package tools

import (
	"encoding/json"
	"github.com/vgxness/vgxness/internal/sdd"
)

type ModelPlanRequest struct {
	Catalog sdd.Catalog `json:"catalog"`
	Plan    sdd.Plan    `json:"plan"`
}

func ResolveModel(payload json.RawMessage) (sdd.ResolvedPlan, error) {
	var request ModelPlanRequest
	if err := decode(payload, &request, "catalog", "plan"); err != nil {
		return sdd.ResolvedPlan{}, err
	}
	return sdd.ResolveModelPlan(request.Catalog, request.Plan)
}
