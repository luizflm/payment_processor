package usecase_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/luizflm/payment-processor/internal/fraud/domain"
	"github.com/luizflm/payment-processor/internal/fraud/usecase"
	"github.com/luizflm/payment-processor/pkg/fsm"
	"github.com/luizflm/payment-processor/pkg/memcache"
	"github.com/stretchr/testify/mock"
)

type mockGeoResolver struct {
	mock.Mock
}

func (m *mockGeoResolver) CountryCode(ctx context.Context, ip string) (string, error) {
	args := m.Called(ctx, ip)
	return args.String(0), args.Error(1)
}

type mockEmbedder struct {
	mock.Mock
}

func (m *mockEmbedder) Embed(text string) []float64 {
	args := m.Called(text)
	return args.Get(0).([]float64)
}

type mockSemanticCache struct {
	mock.Mock
}

func (m *mockSemanticCache) Lookup(vector []float64) (domain.Verdict, float64, bool) {
	args := m.Called(vector)
	return args.Get(0).(domain.Verdict), args.Get(1).(float64), args.Bool(2)
}

func (m *mockSemanticCache) Store(vector []float64, verdict domain.Verdict) {
	m.Called(vector, verdict)
}

func TestPipelineScoreSuccess(t *testing.T) {
	tests := []struct {
		name         string
		card         string
		amount       int64
		priorScores  int
		baseline     float64
		cacheHit     bool
		cacheVerdict domain.Verdict
		want         usecase.Decision
		states       []fsm.State
		wantLookups  int
		wantStored   []domain.Verdict
	}{
		{
			name: "blocklisted card ends at fast rules",
			card: "fp_stolen_card_001", amount: 2500,
			want: usecase.Decision{Verdict: domain.VerdictDeny, RiskScore: 0,
				Reasons: []string{"card_blocklisted"}, Degraded: false},
			states: []fsm.State{usecase.StateEnrich, usecase.StateFastRules, usecase.StateDone},
		},
		{
			name: "velocity limit ends at fast rules",
			card: "fp_normal", amount: 2500, priorScores: domain.VelocityPerMinLimit,
			baseline: -2,
			want: usecase.Decision{Verdict: domain.VerdictDeny, RiskScore: 0,
				Reasons: []string{"velocity_limit_exceeded"}, Degraded: false},
			states: []fsm.State{usecase.StateEnrich, usecase.StateFastRules, usecase.StateDone},
		},
		{
			name: "amount limit ends at fast rules",
			card: "fp_normal", amount: domain.MaxAmountCents + 1,
			want: usecase.Decision{Verdict: domain.VerdictManualReview, RiskScore: 0,
				Reasons: []string{"amount_above_hard_limit"}, Degraded: false},
			states: []fsm.State{usecase.StateEnrich, usecase.StateFastRules, usecase.StateDone},
		},
		{
			name: "ML confidently approves below threshold",
			card: "fp_normal", amount: 2500, baseline: -2,
			want: usecase.Decision{Verdict: domain.VerdictApprove, RiskScore: 1 / (1 + math.Exp(2)),
				Reasons: []string{"fast_rules_passed", "ml_score_confident"}, Degraded: false},
			states: []fsm.State{usecase.StateEnrich, usecase.StateFastRules, usecase.StateMLScore,
				usecase.StateFinalize, usecase.StateDone},
		},
		{
			name: "ML confidently denies above threshold",
			card: "fp_normal", amount: 2500, baseline: 2,
			want: usecase.Decision{Verdict: domain.VerdictDeny, RiskScore: 1 / (1 + math.Exp(-2)),
				Reasons: []string{"fast_rules_passed", "ml_score_confident"}, Degraded: false},
			states: []fsm.State{usecase.StateEnrich, usecase.StateFastRules, usecase.StateMLScore,
				usecase.StateFinalize, usecase.StateDone},
		},
		{
			name: "ambiguous ML uses semantic cache hit",
			card: "fp_normal", amount: 2500, cacheHit: true, cacheVerdict: domain.VerdictApprove,
			want: usecase.Decision{Verdict: domain.VerdictApprove, RiskScore: 0.5,
				Reasons: []string{"fast_rules_passed", "semantic_cache_hit:0.92"}, Degraded: false},
			states: []fsm.State{usecase.StateEnrich, usecase.StateFastRules, usecase.StateMLScore,
				usecase.StateTiebreak, usecase.StateFinalize, usecase.StateDone},
			wantLookups: 1, wantStored: []domain.Verdict{domain.VerdictApprove},
		},
		{
			name: "ambiguous ML without cache hit requires manual review",
			card: "fp_normal", amount: 2500,
			want: usecase.Decision{Verdict: domain.VerdictManualReview, RiskScore: 0.5,
				Reasons: []string{"fast_rules_passed", "ambiguous_score_manual_review"}, Degraded: false},
			states: []fsm.State{usecase.StateEnrich, usecase.StateFastRules, usecase.StateMLScore,
				usecase.StateTiebreak, usecase.StateDone},
			wantLookups: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &domain.TreeModel{
				Features: domain.FeatureCount,
				Baseline: tt.baseline,
				Trees:    []domain.Tree{{Nodes: []domain.Node{{Left: -1, Value: 0}}}},
			}

			geo := &mockGeoResolver{}
			embedder := &mockEmbedder{}
			cache := &mockSemanticCache{}
			geo.Test(t)
			embedder.Test(t)
			cache.Test(t)
			t.Cleanup(func() {
				geo.AssertExpectations(t)
				embedder.AssertExpectations(t)
				cache.AssertExpectations(t)
			})

			geo.On("CountryCode", mock.MatchedBy(func(ctx context.Context) bool {
				return ctx != nil
			}), "127.0.0.1").Return("BR", nil).Times(tt.priorScores + 1)
			if tt.wantLookups > 0 {
				embedder.On("Embed", mock.AnythingOfType("string")).
					Return([]float64{1, 0}).Times(tt.wantLookups)
				cache.On("Lookup", []float64{1, 0}).
					Return(tt.cacheVerdict, 0.92, tt.cacheHit).Times(tt.wantLookups)
			}
			for _, verdict := range tt.wantStored {
				cache.On("Store", []float64{1, 0}, verdict).Return().Once()
			}

			pipeline := usecase.NewPipeline(model, geo, embedder, cache,
				slog.New(slog.NewTextHandler(io.Discard, nil)), 1, time.Second)
			t.Cleanup(pipeline.Close)

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			attempt := domain.Attempt{
				PaymentID: "payment-1", CardFingerPrint: tt.card, AmountCents: tt.amount,
				Currency: "BRL", ClientIP: "127.0.0.1", MerchantCategory: "retail",
				OccurredAt: time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC),
			}

			for i := 0; i < tt.priorScores; i++ {
				decision, _, err := pipeline.Score(ctx, attempt)
				if err != nil || decision.Verdict != domain.VerdictApprove {
					t.Fatalf("velocity setup Score %d = %+v, %v", i+1, decision, err)
				}
			}

			got, trace, err := pipeline.Score(ctx, attempt)
			if err != nil {
				t.Fatalf("Score() error = %v", err)
			}
			if got.Verdict != tt.want.Verdict || got.Degraded != tt.want.Degraded ||
				!reflect.DeepEqual(got.Reasons, tt.want.Reasons) ||
				math.IsNaN(got.RiskScore) || math.Abs(got.RiskScore-tt.want.RiskScore) > 1e-12 {
				t.Errorf("Score() decision = %+v, want %+v", got, tt.want)
			}
			if len(trace) != len(tt.states)-1 {
				t.Fatalf("Score() trace = %+v, want states %v", trace, tt.states)
			}
			for i, transition := range trace {
				if transition.From != tt.states[i] || transition.To != tt.states[i+1] || transition.Err != nil {
					t.Errorf("transition %d = %+v, want %s -> %s without error",
						i, transition, tt.states[i], tt.states[i+1])
				}
			}
		})
	}
}

func TestPipelineScoreFallbacksAndErrors(t *testing.T) {
	geoErr := errors.New("geo unavailable")
	tests := []struct {
		name         string
		geoErr       error
		invalidModel bool
		emptyVerdict bool
		closed       bool
		canceled     bool
		want         usecase.Decision
		wantErr      error
		transitions  []fsm.Transition
	}{
		{
			name:   "geo failure continues with unknown country",
			geoErr: geoErr,
			want: usecase.Decision{Verdict: domain.VerdictManualReview, RiskScore: 0.5,
				Reasons: []string{"fast_rules_passed", "ambiguous_score_manual_review"}},
			transitions: []fsm.Transition{
				{From: usecase.StateEnrich, To: usecase.StateFastRules},
				{From: usecase.StateFastRules, To: usecase.StateMLScore},
				{From: usecase.StateMLScore, To: usecase.StateTiebreak},
				{From: usecase.StateTiebreak, To: usecase.StateDone},
			},
		},
		{
			name:         "invalid model degrades to manual review",
			invalidModel: true,
			want: usecase.Decision{Verdict: domain.VerdictManualReview,
				Reasons: []string{"fast_rules_passed", "ml_stage_skipped"}, Degraded: true},
			transitions: []fsm.Transition{
				{From: usecase.StateEnrich, To: usecase.StateFastRules},
				{From: usecase.StateFastRules, To: usecase.StateMLScore},
				// The trace records the failed handler's output before the FSM applies its fallback.
				{From: usecase.StateMLScore, To: "", Err: domain.ErrModelInvalid},
				{From: usecase.StateFinalize, To: usecase.StateDone},
			},
		},
		{
			name:         "empty cached verdict defaults to manual review",
			emptyVerdict: true,
			want: usecase.Decision{Verdict: domain.VerdictManualReview, RiskScore: 0.5,
				Reasons: []string{"fast_rules_passed", "semantic_cache_hit:0.92"}},
			transitions: []fsm.Transition{
				{From: usecase.StateEnrich, To: usecase.StateFastRules},
				{From: usecase.StateFastRules, To: usecase.StateMLScore},
				{From: usecase.StateMLScore, To: usecase.StateTiebreak},
				{From: usecase.StateTiebreak, To: usecase.StateFinalize},
				{From: usecase.StateFinalize, To: usecase.StateDone},
			},
		},
		{
			name:    "score after close returns cache error",
			closed:  true,
			wantErr: memcache.ErrClosed,
			transitions: []fsm.Transition{
				{From: usecase.StateEnrich, To: "", Err: memcache.ErrClosed},
			},
		},
		{
			name:     "canceled context stops before enrichment",
			canceled: true,
			wantErr:  context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			geo := &mockGeoResolver{}
			embedder := &mockEmbedder{}
			cache := &mockSemanticCache{}
			geo.Test(t)
			embedder.Test(t)
			cache.Test(t)
			t.Cleanup(func() {
				geo.AssertExpectations(t)
				embedder.AssertExpectations(t)
				cache.AssertExpectations(t)
			})

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			if !tt.closed && !tt.canceled {
				geo.On("CountryCode", ctx, "127.0.0.1").Return("BR", tt.geoErr).Once()
				if !tt.invalidModel {
					country := "BR"
					if tt.geoErr != nil {
						country = ""
					}
					embedder.On("Embed", "retail fp_normal bucket: 0 country: "+country).
						Return([]float64{1, 0}).Once()
					cache.On("Lookup", []float64{1, 0}).Return(domain.Verdict(""), 0.92, tt.emptyVerdict).Once()
					if tt.emptyVerdict {
						cache.On("Store", []float64{1, 0}, domain.VerdictManualReview).Return().Once()
					}
				}
			}

			model := &domain.TreeModel{
				Features: domain.FeatureCount,
				Trees:    []domain.Tree{{Nodes: []domain.Node{{Left: -1, Value: 0}}}},
			}
			if tt.invalidModel {
				model.Features = domain.FeatureCount + 1
			}

			pipeline := usecase.NewPipeline(model, geo, embedder, cache,
				slog.New(slog.NewTextHandler(io.Discard, nil)), 1, time.Second)
			t.Cleanup(pipeline.Close)

			if tt.closed {
				pipeline.Close()
			}
			if tt.canceled {
				cancel()
			}

			got, trace, err := pipeline.Score(ctx, domain.Attempt{
				PaymentID: "payment-1", CardFingerPrint: "fp_normal", AmountCents: 2500,
				Currency: "BRL", ClientIP: "127.0.0.1", MerchantCategory: "retail",
				OccurredAt: time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC),
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Score() error = %v, want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Score() decision = %+v, want %+v", got, tt.want)
			}
			if len(trace) != len(tt.transitions) {
				t.Fatalf("Score() trace = %+v, want %+v", trace, tt.transitions)
			}
			for i, transition := range trace {
				want := tt.transitions[i]
				if transition.From != want.From || transition.To != want.To || !errors.Is(transition.Err, want.Err) {
					t.Errorf("transition %d = %+v, want %+v", i, transition, want)
				}
			}
		})
	}
}
