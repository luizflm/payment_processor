package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/luizflm/payment-processor/internal/fraud/domain"
	"github.com/luizflm/payment-processor/pkg/batch"
	"github.com/luizflm/payment-processor/pkg/fsm"
	"github.com/luizflm/payment-processor/pkg/memcache"
)

// FSM states of the scoring pipeline
const (
	StateEnrich    = "enrich"
	StateFastRules = "fast_rules"
	StateMLScore   = "ml_score"
	StateTiebreak  = "Tiebreak"
	StateFinalize  = "finalize"
	StateDone      = "done"
)

// GeoResolver enriches the attempt with a country code
type GeoResolver interface {
	CountryCode(ctx context.Context, ip string) (string, error)
}

// Embedder converts text to a vector for the semantic cache
type Embedder interface {
	Embed(text string) []float64
}

// SemanticCache resolves ambiguous scores by similarity to recently
// decided attempts
type SemanticCache interface {
	Lookup(vector []float64) (domain.Verdict, float64, bool)
	Store(vector []float64, verdic domain.Verdict)
}

// Decision is the pipeline output
type Decision struct {
	Verdict   domain.Verdict
	RiskScore float64
	Reasons   []string
	Degraded  bool
}

// scoringData is the mutable payload driven through the FSM
type scoringData struct {
	attempt domain.Attempt
	verdict domain.Verdict
	final   bool
	vector  []float64
}

// Pipeline score payment attempts
type Pipeline struct {
	machine  *fsm.Machine[scoringData]
	model    *domain.TreeModel
	geo      GeoResolver
	embedder Embedder
	semCache SemanticCache
	velocity *memcache.Cache[int]
	batcher  *batch.Batcher[[]float64, float64]
	logger   *slog.Logger
}

// NewPipeline creates a scoring pipeline. batchSize and batchWait tune the
// dynamic batcher in front of the model.
func NewPipeline(
	model *domain.TreeModel,
	geo GeoResolver,
	embedder Embedder,
	semCache SemanticCache,
	logger *slog.Logger,
	batchSize int,
	batchWait time.Duration,
) *Pipeline {
	p := &Pipeline{
		model:    model,
		geo:      geo,
		embedder: embedder,
		semCache: semCache,
		logger:   logger,
		velocity: memcache.New[int](time.Minute, time.Minute),
	}

	p.batcher = batch.New(batchSize, batchWait, func(_ context.Context, vectors [][]float64) ([]float64, error) {
		scores := make([]float64, len(vectors))
		for i, vector := range vectors {
			s, err := p.model.Score(vector)
			if err != nil {
				p.logger.Warn("model scoring failed", "error", err)
				return nil, err
			}
			scores[i] = s
		}
		return scores, nil
	})

	p.machine = fsm.New[scoringData](StateEnrich).
		Handle(StateEnrich, p.enrich).
		Handle(StateFastRules, p.fastRules).
		Handle(StateMLScore, p.mlScore).
		FallbackTo(StateMLScore, StateFinalize).
		Handle(StateTiebreak, p.tiebreak).
		Handle(StateFinalize, p.finalize).
		Terminal(StateDone)

	return p
}

// Close releases the batcher worker
func (p *Pipeline) Close() {
	p.batcher.Close()
	err := p.velocity.Close()
	if err != nil {
		p.logger.Warn("failed to close velocity cache", "error", err)
	}
}

// Score runs one attempt through the machine and returns the decision
// together with the transition trace for observability
func (p *Pipeline) Score(ctx context.Context, attempt domain.Attempt) (Decision, []fsm.Transition, error) {
	data := scoringData{attempt: attempt}

	trace, err := p.machine.Run(ctx, &data)
	if err != nil {
		return Decision{}, trace, fmt.Errorf("fraud pipeline: %w", err)
	}

	return Decision{
		Verdict:   data.verdict,
		RiskScore: data.attempt.RiskScore,
		Reasons:   data.attempt.Reasons,
		Degraded:  data.attempt.Degraded,
	}, trace, nil
}

func (p *Pipeline) enrich(ctx context.Context, d *scoringData) (fsm.State, error) {
	var err error
	d.attempt.VelocityLastMin, err = p.velocity.Increment(
		"card:"+d.attempt.CardFingerPrint,
		func(current int, exists bool) int {
			if !exists {
				return 1
			}

			return current + 1
		})
	if err != nil {
		return "", fmt.Errorf("velocity cache: %w", err)
	}

	code, err := p.geo.CountryCode(ctx, d.attempt.ClientIP)
	if err != nil {
		p.logger.Warn("geo lookup failed",
			slog.String("payment_id", d.attempt.PaymentID),
			slog.String("error", err.Error()))
		code = ""
	}
	d.attempt.CountryCode = code

	return StateFastRules, nil
}

func (p *Pipeline) fastRules(_ context.Context, d *scoringData) (fsm.State, error) {
	result := domain.ApplyFastRules(d.attempt)
	d.attempt.Reasons = append(d.attempt.Reasons, result.Reason)

	if result.Final {
		d.verdict = result.Verdict
		d.final = true
		return StateDone, nil
	}

	return StateMLScore, nil
}

func (p *Pipeline) mlScore(ctx context.Context, d *scoringData) (fsm.State, error) {
	features := domain.FeatureVector(d.attempt)

	score, err := p.batcher.Submit(ctx, features)
	if err != nil {
		d.attempt.Degraded = true
		d.attempt.Reasons = append(d.attempt.Reasons, "ml_stage_skipped")
		d.verdict = domain.VerdictManualReview
		return "", fmt.Errorf("ml score: %w", err)
	}

	d.attempt.RiskScore = score
	d.verdict = domain.VerdictFromScore(score)

	if d.verdict == domain.VerdictManualReview {
		return StateTiebreak, nil
	}

	d.attempt.Reasons = append(d.attempt.Reasons, "ml_score_confident")
	return StateFinalize, nil
}

func (p *Pipeline) finalize(_ context.Context, d *scoringData) (fsm.State, error) {
	if d.verdict == "" {
		d.verdict = domain.VerdictManualReview
	}
	if d.vector != nil {
		p.semCache.Store(d.vector, d.verdict)
	}

	return StateDone, nil
}

func (p *Pipeline) tiebreak(_ context.Context, d *scoringData) (fsm.State, error) {
	summary := fmt.Sprintf("%s %s bucket: %d country: %s",
		d.attempt.MerchantCategory,
		d.attempt.CardFingerPrint,
		d.attempt.AmountCents/50000,
		d.attempt.CountryCode)

	d.vector = p.embedder.Embed(summary)

	verdict, similarity, hit := p.semCache.Lookup(d.vector)
	if hit {
		d.verdict = verdict
		d.attempt.Reasons = append(d.attempt.Reasons, fmt.Sprintf("semantic_cache_hit:%.2f", similarity))
		return StateFinalize, nil
	}

	d.attempt.Reasons = append(d.attempt.Reasons, "ambiguous_score_manual_review")
	return StateDone, nil
}
