package domain_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/luizflm/payment-processor/internal/fraud/domain"
)

func TestFeatureVector(t *testing.T) {
	tests := []struct {
		name              string
		hour              int
		country, category string
		want              []float64
	}{
		{"common attempt", 12, "BR", "groceries", []float64{2500, 2, 0, 0, 0}},
		{"before night boundary", 5, "BR", "groceries", []float64{2500, 2, 1, 0, 0}},
		{"at night boundary", 6, "BR", "groceries", []float64{2500, 2, 0, 0, 0}},
		{"after night boundary", 7, "BR", "groceries", []float64{2500, 2, 0, 0, 0}},
		{"midnight", 0, "BR", "groceries", []float64{2500, 2, 1, 0, 0}},
		{"last hour", 23, "BR", "groceries", []float64{2500, 2, 0, 0, 0}},
		{"empty country", 12, "", "groceries", []float64{2500, 2, 0, 0, 0}},
		{"one character country", 12, "B", "groceries", []float64{2500, 2, 0, 1, 0}},
		{"foreign country", 12, "US", "groceries", []float64{2500, 2, 0, 1, 0}},
		{"empty category", 12, "BR", "", []float64{2500, 2, 0, 0, 0}},
		{"gambling", 12, "BR", "gambling", []float64{2500, 2, 0, 0, 1}},
		{"crypto", 12, "BR", "crypto", []float64{2500, 2, 0, 0, 1}},
		{"gift cards", 12, "BR", "gift_cards", []float64{2500, 2, 0, 0, 1}},
		{"money transfer", 12, "BR", "money_transfer", []float64{2500, 2, 0, 0, 1}},
		{"combined flags", 3, "US", "crypto", []float64{2500, 2, 1, 1, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.FeatureVector(domain.Attempt{
				AmountCents:      2500,
				VelocityLastMin:  2,
				OccurredAt:       time.Date(2026, time.January, 1, tt.hour, 0, 0, 0, time.UTC),
				CountryCode:      tt.country,
				MerchantCategory: tt.category,
			})
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("FeatureVector() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVerdictFromScore(t *testing.T) {
	tests := []struct {
		name  string
		score float64
		want  domain.Verdict
	}{
		{"approve", 0.1, domain.VerdictApprove},
		{"manual review", 0.5, domain.VerdictManualReview},
		{"deny", 0.9, domain.VerdictDeny},
		{"below approve threshold", math.Nextafter(domain.ApproveBelow, math.Inf(-1)), domain.VerdictApprove},
		{"at approve threshold", domain.ApproveBelow, domain.VerdictManualReview},
		{"above approve threshold", math.Nextafter(domain.ApproveBelow, math.Inf(1)), domain.VerdictManualReview},
		{"below deny threshold", math.Nextafter(domain.DenyAbove, math.Inf(-1)), domain.VerdictManualReview},
		{"at deny threshold", domain.DenyAbove, domain.VerdictManualReview},
		{"above deny threshold", math.Nextafter(domain.DenyAbove, math.Inf(1)), domain.VerdictDeny},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domain.VerdictFromScore(tt.score); got != tt.want {
				t.Fatalf("VerdictFromScore(%g) = %q, want %q", tt.score, got, tt.want)
			}
		})
	}
}
