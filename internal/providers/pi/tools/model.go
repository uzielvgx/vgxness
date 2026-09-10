package tools

import (
	"encoding/json"
	"github.com/vgxness/vgxness/internal/modelplan"
)

type ModelPlanRequest struct {
	Catalog modelplan.Catalog `json:"catalog"`
	Plan    modelplan.Plan    `json:"plan"`
}

func ResolveModel(payload json.RawMessage) (modelplan.ResolvedPlan, error) {
	var request ModelPlanRequest
	if err := decode(payload, &request, "catalog", "plan"); err != nil {
		return modelplan.ResolvedPlan{}, err
	}
	result, err := modelplan.ResolveModelPlan(request.Catalog, request.Plan)
	for role := range result.Roles {
		switch role {
		case modelplan.RoleManager, modelplan.RoleResearch, modelplan.RoleImplementation, modelplan.RoleVerification, modelplan.RoleCAREReviewer, modelplan.RoleCARESpecialist, modelplan.RoleCAREChallenger:
		default:
			delete(result.Roles, role)
		}
	}
	return result, err
}
