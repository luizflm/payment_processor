package domain

const (
	MaxAmountCents      = 5_000_000
	VelocityPerMinLimit = 10
)

type RuleResult struct {
	Verdict Verdict
	Reason  string
	// Final prevents later stages from overriding the rule's decision.
	Final bool
}

// Blocklist of card fingerprints known to be compromised. A real
// system loads this from a feed. The values exist so the local demo
// and the tests can trigger the path.
var Blocklist = map[string]bool{
	"fp_stolen_card_001": true,
	"fp_stolen_card_002": true,
}

// ApplyFastRules runs the deterministic checks that need no model.
func ApplyFastRules(a Attempt) RuleResult {
	if Blocklist[a.CardFingerPrint] {
		return RuleResult{Verdict: VerdictDeny, Reason: "card_blocklisted", Final: true}
	}

	if a.AmountCents > MaxAmountCents {
		return RuleResult{Verdict: VerdictManualReview, Reason: "amount_above_hard_limit", Final: true}
	}

	if a.VelocityLastMin > VelocityPerMinLimit {
		return RuleResult{Verdict: VerdictDeny, Reason: "velocity_limit_exceeded", Final: true}
	}

	return RuleResult{Verdict: VerdictApprove, Reason: "fast_rules_passed", Final: false}
}
