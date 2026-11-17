package domain_test

import (
	"testing"

	"github.com/luizflm/payment-processor/internal/fraud/domain"
)

func TestApplyFastRules(t *testing.T) {
	passed := domain.RuleResult{Verdict: domain.VerdictApprove, Reason: "fast_rules_passed", Final: false}
	blocked := domain.RuleResult{Verdict: domain.VerdictDeny, Reason: "card_blocklisted", Final: true}
	amount := domain.RuleResult{Verdict: domain.VerdictManualReview, Reason: "amount_above_hard_limit", Final: true}
	velocity := domain.RuleResult{Verdict: domain.VerdictDeny, Reason: "velocity_limit_exceeded", Final: true}
	tests := []struct {
		name     string
		card     string
		amount   int64
		velocity int
		want     domain.RuleResult
	}{
		{"common attempt", "fp_normal", 2500, 2, passed},
		{"empty fingerprint", "", 2500, 2, passed},
		{"first blocked card", "fp_stolen_card_001", 2500, 2, blocked},
		{"second blocked card", "fp_stolen_card_002", 2500, 2, blocked},
		{"below amount limit", "fp_normal", domain.MaxAmountCents - 1, 2, passed},
		{"at amount limit", "fp_normal", domain.MaxAmountCents, 2, passed},
		{"above amount limit", "fp_normal", domain.MaxAmountCents + 1, 2, amount},
		{"below velocity limit", "fp_normal", 2500, domain.VelocityPerMinLimit - 1, passed},
		{"at velocity limit", "fp_normal", 2500, domain.VelocityPerMinLimit, passed},
		{"above velocity limit", "fp_normal", 2500, domain.VelocityPerMinLimit + 1, velocity},
		{"blocklist takes precedence", "fp_stolen_card_001", domain.MaxAmountCents + 1, domain.VelocityPerMinLimit + 1, blocked},
		{"amount takes precedence over velocity", "fp_normal", domain.MaxAmountCents + 1, domain.VelocityPerMinLimit + 1, amount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.ApplyFastRules(domain.Attempt{CardFingerPrint: tt.card, AmountCents: tt.amount, VelocityLastMin: tt.velocity})
			if got != tt.want {
				t.Fatalf("ApplyFastRules() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
