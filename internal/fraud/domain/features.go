// Package domain defines the fraude scoring model: input features,
// rules outcomes and the final decision contract.
package domain

import "time"

// Attempt is the payment attempt as seen by the fraud pipeline.
type Attempt struct {
	PaymentID        string
	AccountID        string
	CardFingerPrint  string
	AmountCents      int64
	Currency         string
	ClientIP         string
	MerchantCategory string
	OccurredAt       time.Time

	// Enriched during the pipeline run
	CountryCode     string
	VelocityLastMin int
	RiskScore       float64
	Reasons         []string
	Degraded        bool
}

// Feature names used by the decision tree model
const (
	FeatureAmountCents     = 0
	FeatureVelocityLastMin = 1
	FeatureNightTime       = 2
	FeatureForeignCountry  = 3
	FeatureHighRiskMCC     = 4
	FeatureCount           = 5
)

// HighRiskMerchantCategories flag segments with elevated chargeback
// rates in this fictional dataset
var HighRiskMerchantCategories = map[string]bool{
	"gambling":       true,
	"crypto":         true,
	"gift_cards":     true,
	"money_transfer": true,
}

// FeatureVector converts the enriched attempt into the numeric vector
// consumed by the tree model
func FeatureVector(a Attempt) []float64 {
	v := make([]float64, FeatureCount)
	v[FeatureAmountCents] = float64(a.AmountCents)
	v[FeatureVelocityLastMin] = float64(a.VelocityLastMin)

	hour := a.OccurredAt.Hour()
	if hour < 6 {
		v[FeatureNightTime] = 1
	}
	if a.CountryCode != "" && a.CountryCode != "BR" {
		v[FeatureForeignCountry] = 1
	}
	if HighRiskMerchantCategories[a.MerchantCategory] {
		v[FeatureHighRiskMCC] = 1
	}

	return v
}

// Verdict values returned by the pipeline
type Verdict string

const (
	VerdictApprove      Verdict = "approve"
	VerdictDeny         Verdict = "deny"
	VerdictManualReview Verdict = "manual_review"
)

// Thresholds convert a continuous risk score into a verdict. Scores
// in the ambiguous band go through the semantic tiebreak stage first.
const (
	ApproveBelow = 0.35
	DenyAbove    = 0.75
)

// VerdictFromScore applies the banding rule
func VerdictFromScore(score float64) Verdict {
	switch {
	case score < ApproveBelow:
		return VerdictApprove
	case score > DenyAbove:
		return VerdictDeny
	default:
		return VerdictManualReview
	}
}
