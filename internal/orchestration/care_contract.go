package orchestration

// CAREMission is the bounded manager-to-child contract.
type CAREMission struct {
	SchemaVersion       int           `json:"schemaVersion"`
	Role                CARERole      `json:"role"`
	MissionID           CAREID        `json:"missionId"`
	ReplayNonce         CAREID        `json:"replayNonce"`
	ReviewBinding       ReviewBinding `json:"reviewBinding"`
	CandidateIdentity   string        `json:"candidateIdentity"`
	CatalogRef          CatalogRef    `json:"catalogRef"`
	ChangeProfileDigest Digest        `json:"changeProfileDigest"`
	AssurancePlanDigest Digest        `json:"assurancePlanDigest"`
	Assignment          any           `json:"assignment"`
	Skills              []string      `json:"skills"`
	EvidenceScope       []string      `json:"evidenceScope"`
	CorrectionDelta     []string      `json:"correctionDelta"`
}
