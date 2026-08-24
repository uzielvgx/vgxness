package orchestration

const GovernedRiskCatalogID CAREID = "vgxness-care-risk-catalog/v1"

func GovernedRiskCatalog() (RiskCatalog, error) {
	return RiskCatalog{SchemaVersion: 1, CatalogID: GovernedRiskCatalogID, Risks: []CatalogRisk{{RiskID: "permissions", Materiality: MaterialityCritical}, {RiskID: "authentication", Materiality: MaterialityCritical}, {RiskID: "security", Materiality: MaterialityCritical}, {RiskID: "payments", Materiality: MaterialityCritical}, {RiskID: "lifecycle", Materiality: MaterialityCritical}, {RiskID: "data-loss", Materiality: MaterialityCritical}, {RiskID: "process", Materiality: MaterialityCritical}, {RiskID: "durability", Materiality: MaterialityCritical}, {RiskID: "provider-template", Materiality: MaterialityMedium}, {RiskID: "cross-platform", Materiality: MaterialityMedium}}}, nil
}
