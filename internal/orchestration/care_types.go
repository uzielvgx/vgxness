package orchestration

import "errors"

type CAREID string
type Digest string
type ClaimState string
type Outcome string
type ActivationLevel string
type CARERole string
type Materiality string
type ChallengeTargetKind string
type ChallengeOutcome string

const (
	ClaimSupported        ClaimState          = "SUPPORTED"
	ClaimViolated         ClaimState          = "VIOLATED"
	ClaimUnknown          ClaimState          = "UNKNOWN"
	ClaimNotApplicable    ClaimState          = "NOT_APPLICABLE"
	OutcomePass           Outcome             = "PASS"
	OutcomeFail           Outcome             = "FAIL"
	OutcomeInconclusive   Outcome             = "INCONCLUSIVE"
	CAREActivationExempt  ActivationLevel     = "exempt"
	ActivationStandard    ActivationLevel     = "standard"
	ActivationElevated    ActivationLevel     = "elevated"
	ActivationCritical    ActivationLevel     = "critical"
	CAREReviewer          CARERole            = "care-reviewer"
	CARESpecialist        CARERole            = "care-specialist"
	CAREChallenger        CARERole            = "care-challenger"
	MaterialityLow        Materiality         = "low"
	MaterialityMedium     Materiality         = "medium"
	MaterialityCritical   Materiality         = "critical"
	ChallengeClaim        ChallengeTargetKind = "claim"
	ChallengeFinding      ChallengeTargetKind = "finding"
	ChallengeEvidence     ChallengeTargetKind = "evidence"
	ChallengeScope        ChallengeTargetKind = "scope"
	ChallengeCorroborated ChallengeOutcome    = "corroborated"
	ChallengeRefuted      ChallengeOutcome    = "refuted"
	ChallengeInconclusive ChallengeOutcome    = "inconclusive"
)

var ErrCAREBlocked = errors.New("CARE input is invalid")

type CatalogRef struct {
	CatalogID CAREID `json:"catalogId"`
	Digest    Digest `json:"digest"`
}
type CatalogRisk struct {
	RiskID      CAREID      `json:"riskId"`
	Materiality Materiality `json:"materiality"`
}
type RiskCatalog struct {
	SchemaVersion int           `json:"schemaVersion"`
	CatalogID     CAREID        `json:"catalogId"`
	Risks         []CatalogRisk `json:"risks"`
}
type ChallengeTarget struct {
	Kind     ChallengeTargetKind `json:"kind"`
	TargetID CAREID              `json:"targetId"`
}
type ActivationDecision struct {
	Level   ActivationLevel `json:"level"`
	Reasons []string        `json:"reasons"`
}
