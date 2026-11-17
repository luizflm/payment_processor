package domain_test

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/luizflm/payment-processor/internal/fraud/domain"
)

func TestParseTreeModel(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*domain.TreeModel)
		invalid bool
	}{
		{"common model", func(m *domain.TreeModel) {}, false},
		{"negative features", func(m *domain.TreeModel) { m.Features = -1 }, true},
		{"zero features", func(m *domain.TreeModel) { m.Features = 0 }, true},
		{"one feature", func(m *domain.TreeModel) { m.Features = 1 }, false},
		{"missing trees", func(m *domain.TreeModel) { m.Trees = nil }, true},
		{"empty trees", func(m *domain.TreeModel) { m.Trees = []domain.Tree{} }, true},
		{"missing nodes", func(m *domain.TreeModel) { m.Trees[0].Nodes = nil }, true},
		{"empty nodes", func(m *domain.TreeModel) { m.Trees[0].Nodes = []domain.Node{} }, true},
		{"single leaf", func(m *domain.TreeModel) { m.Trees[0].Nodes = []domain.Node{{Left: -1, Value: 0.2}} }, false},
		{"negative feature index", func(m *domain.TreeModel) { m.Trees[0].Nodes[0].Feature = -1 }, true},
		{"feature index zero", func(m *domain.TreeModel) { m.Trees[0].Nodes[0].Feature = 0 }, false},
		{"feature index one", func(m *domain.TreeModel) { m.Trees[0].Nodes[0].Feature = 1 }, false},
		{"last feature index", func(m *domain.TreeModel) { m.Trees[0].Nodes[0].Feature = m.Features - 1 }, false},
		{"feature index at count", func(m *domain.TreeModel) { m.Trees[0].Nodes[0].Feature = m.Features }, true},
		{"feature index above count", func(m *domain.TreeModel) { m.Trees[0].Nodes[0].Feature = m.Features + 1 }, true},
		{"left below leaf sentinel", func(m *domain.TreeModel) { m.Trees[0].Nodes[0].Left = -2 }, true},
		{"leaf ignores split fields", func(m *domain.TreeModel) {
			m.Trees[0].Nodes[0] = domain.Node{Left: -1, Right: -2, Feature: -1, Value: 0.4}
		}, false},
		{"left above leaf sentinel", func(m *domain.TreeModel) { m.Trees[0].Nodes = []domain.Node{{Left: -1}, {Left: 0, Right: 0}} }, false},
		{"left last index", func(m *domain.TreeModel) { m.Trees[0].Nodes[0].Left = 2 }, false},
		{"left at node count", func(m *domain.TreeModel) { m.Trees[0].Nodes[0].Left = 3 }, true},
		{"left above node count", func(m *domain.TreeModel) { m.Trees[0].Nodes[0].Left = 4 }, true},
		{"negative right index", func(m *domain.TreeModel) { m.Trees[0].Nodes[0].Right = -1 }, true},
		{"right index zero", func(m *domain.TreeModel) { m.Trees[0].Nodes = []domain.Node{{Left: -1}, {Left: 0, Right: 0}} }, false},
		{"right index one", func(m *domain.TreeModel) { m.Trees[0].Nodes[0].Right = 1 }, false},
		{"right last index", func(m *domain.TreeModel) { m.Trees[0].Nodes[0].Right = 2 }, false},
		{"right at node count", func(m *domain.TreeModel) { m.Trees[0].Nodes[0].Right = 3 }, true},
		{"right above node count", func(m *domain.TreeModel) { m.Trees[0].Nodes[0].Right = 4 }, true},
		{"invalid second tree", func(m *domain.TreeModel) { m.Trees = append(m.Trees, domain.Tree{}) }, true},
		{"invalid later node", func(m *domain.TreeModel) { m.Trees[0].Nodes[2] = domain.Node{Feature: -1} }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := domain.TreeModel{Version: "v1", Features: domain.FeatureCount, Baseline: -0.5, Trees: []domain.Tree{{Nodes: []domain.Node{
				{Feature: 0, Threshold: 2500, Left: 1, Right: 2},
				{Left: -1, Value: -0.2}, {Left: -1, Value: 0.8},
			}}}}

			tt.change(&want)

			data, err := json.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}

			got, err := domain.ParseTreeModel(data)
			if tt.invalid {
				if !errors.Is(err, domain.ErrModelInvalid) || got != nil {
					t.Fatalf("ParseTreeModel() = %+v, %v; want nil, ErrModelInvalid", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, &want) {
				t.Fatalf("ParseTreeModel() = %+v, want %+v", got, &want)
			}
		})
	}
}

func TestParseTreeModelInvalidJSON(t *testing.T) {
	tests := []struct{ name, data string }{
		{"malformed", `{"features":`},
		{"empty file", ""},
		{"wrong field type", `{"features":"five"}`},
		{"wrong root type", `[]`},
		{"null model", `null`},
		{"missing fields", `{}`},
		{"trailing document", `{} {}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.ParseTreeModel([]byte(tt.data))
			if !errors.Is(err, domain.ErrModelInvalid) || got != nil {
				t.Fatalf("ParseTreeModel() = %+v, %v; want nil, ErrModelInvalid", got, err)
			}
		})
	}
}

func TestTreeModelScore(t *testing.T) {
	model := domain.TreeModel{Features: 2, Baseline: 0.25, Trees: []domain.Tree{
		{Nodes: []domain.Node{
			{Feature: 0, Threshold: 100, Left: 1, Right: 2},
			{Left: -1, Value: -1},
			{Feature: 1, Threshold: 5, Left: 3, Right: 4},
			{Left: -1, Value: 0.5}, {Left: -1, Value: 2},
		}},
		{Nodes: []domain.Node{{Left: -1, Value: 0.25}}},
	}}
	tests := []struct {
		name     string
		features []float64
		total    float64
		invalid  bool
	}{
		{"common attempt", []float64{50, 2}, -0.5, false},
		{"below root threshold", []float64{math.Nextafter(100, math.Inf(-1)), 2}, -0.5, false},
		{"at root threshold", []float64{100, 2}, -0.5, false},
		{"above root threshold", []float64{math.Nextafter(100, math.Inf(1)), 2}, 1, false},
		{"below nested threshold", []float64{200, math.Nextafter(5, math.Inf(-1))}, 1, false},
		{"at nested threshold", []float64{200, 5}, 1, false},
		{"above nested threshold", []float64{200, math.Nextafter(5, math.Inf(1))}, 2.5, false},
		{"missing features", nil, 0, true},
		{"one fewer feature", []float64{50}, 0, true},
		{"one extra feature", []float64{50, 2, 0}, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := model.Score(tt.features)
			if tt.invalid {
				if !errors.Is(err, domain.ErrModelInvalid) || got != 0 {
					t.Fatalf("Score() = %g, %v; want 0, ErrModelInvalid", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := 1 / (1 + math.Exp(-tt.total))
			if math.IsNaN(got) || math.Abs(got-want) > 1e-14 {
				t.Fatalf("Score() = %.17g, want %.17g", got, want)
			}
		})
	}
}

func TestTreeModelScoreSigmoidBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		total float64
	}{
		{"negative common score", -2}, {"positive common score", 2},
		{"below zero", math.Nextafter(0, math.Inf(-1))}, {"zero", 0}, {"above zero", math.Nextafter(0, math.Inf(1))},
		{"below halving cutoff", math.Nextafter(0.5, math.Inf(-1))}, {"at halving cutoff", 0.5}, {"above halving cutoff", math.Nextafter(0.5, math.Inf(1))},
		{"below positive cutoff", math.Nextafter(30, math.Inf(-1))}, {"at positive cutoff", 30}, {"above positive cutoff", math.Nextafter(30, math.Inf(1))},
		{"below negative cutoff", math.Nextafter(-30, math.Inf(-1))}, {"at negative cutoff", -30}, {"above negative cutoff", math.Nextafter(-30, math.Inf(1))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := domain.TreeModel{Features: 1, Trees: []domain.Tree{{Nodes: []domain.Node{{Left: -1, Feature: -1, Right: -2, Value: tt.total}}}}}
			got, err := model.Score([]float64{0})
			if err != nil {
				t.Fatal(err)
			}
			if tt.total > 30 || tt.total < -30 {
				want := 0.0
				if tt.total > 30 {
					want = 1
				}
				if got != want {
					t.Fatalf("Score() = %.17g, want %g", got, want)
				}
				return
			}
			z := math.Exp(-math.Abs(tt.total))
			want := 1 / (1 + z)
			if tt.total < 0 {
				want = z / (1 + z)
			}
			if math.IsNaN(got) || math.Abs(got-want) > 1e-14*math.Abs(want) {
				t.Fatalf("Score() = %.17g, want %.17g", got, want)
			}
		})
	}
}
