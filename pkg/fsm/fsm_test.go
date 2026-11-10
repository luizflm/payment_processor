package fsm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/luizflm/payment-processor/pkg/fsm"
)

type payload struct {
	visited []string
	score   float64
}

func record(p *payload, name string) {
	p.visited = append(p.visited, name)
}

func TestRunHappyPath(t *testing.T) {
	tests := []struct {
		name      string
		wantHops  int
		wantScore float64
	}{
		{
			name:      "happy path",
			wantHops:  2,
			wantScore: 0.9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p payload
			m := fsm.New[payload]("start").
				Handle("start", func(ctx context.Context, p *payload) (fsm.State, error) {
					record(p, "start")
					return "score", nil
				}).
				Handle("score", func(ctx context.Context, p *payload) (fsm.State, error) {
					record(p, "score")
					p.score = 0.9
					return "done", nil
				}).
				Terminal("done")

			trace, err := m.Run(context.Background(), &p)
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			if len(trace) != tt.wantHops {
				t.Errorf("len(trace) = %d, want %d", len(trace), tt.wantHops)
			}

			if p.score != tt.wantScore {
				t.Errorf("score = %v, want %v", p.score, tt.wantScore)
			}
		})
	}
}

func TestFallbackKeepsPipelineAvailable(t *testing.T) {
	dependencyDown := errors.New("upstream timeout")
	var p payload
	m := fsm.New[payload]("start").
		Handle("start", func(ctx context.Context, p *payload) (fsm.State, error) {
			record(p, "start")
			return "ml", nil
		}).
		Handle("ml", func(ctx context.Context, p *payload) (fsm.State, error) {
			return "", dependencyDown
		}).
		FallbackTo("ml", "rules_only").
		Handle("rules_only", func(ctx context.Context, p *payload) (fsm.State, error) {
			record(p, "rules_only")
			return "done", nil
		}).
		Terminal("done")

	trace, err := m.Run(context.Background(), &p)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(p.visited) == 0 {
		t.Fatal("visited is empty")
	}
	if got := p.visited[len(p.visited)-1]; got != "rules_only" {
		t.Errorf("last visited = %q, want rules_only", got)
	}
	if len(trace) < 2 {
		t.Fatalf("len(trace) = %d, want at least 2", len(trace))
	}
	if trace[1].Err == nil {
		t.Error("trace[1].Err = nil, want original handler error")
	}
}

func TestErrorWithoutFallbackAborts(t *testing.T) {
	boom := errors.New("boom")
	var p payload
	m := fsm.New[payload]("start").
		Handle("start", func(ctx context.Context, p *payload) (fsm.State, error) {
			return "", boom
		}).
		Terminal("done")

	_, err := m.Run(context.Background(), &p)
	if !errors.Is(err, boom) {
		t.Errorf("Run() error = %v, want %v", err, boom)
	}
}
