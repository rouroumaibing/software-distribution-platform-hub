package main

import (
	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	"github.com/google/uuid"

	componentrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/component/repository"
	componentsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/component/service"
)

// runVarSource adapts the component config + variable-group services to the
// run service's VarSource interface (ui-12 §5.3 / N-26). It supplies the two
// lower tiers of the trigger-time param resolution:
//
//	declared defaults < group vars < component vars < run inputs
//
// Secret config rows are excluded — secrets reach tasks via the credential /
// secret-ref flow, never as plaintext run params.
type runVarSource struct {
	componentConfigRepo *componentrepo.ComponentConfigRepository
	variableGroups      *componentsvc.VariableGroupService
}

// ComponentParams flattens the component's non-secret config rows into
// trigger params (environment-unscoped global defaults).
func (a runVarSource) ComponentParams(componentID uuid.UUID) []runnerapi.Param {
	rows, err := a.componentConfigRepo.ListAll(componentID, nil)
	if err != nil {
		return nil
	}
	out := make([]runnerapi.Param, 0, len(rows))
	for _, r := range rows {
		if r.IsSecret || r.Key == "" {
			continue
		}
		out = append(out, runnerapi.Param{Name: r.Key, Value: r.Value})
	}
	return out
}

// GroupParams flattens the component's bound variable groups (ascending
// priority) into trigger params.
func (a runVarSource) GroupParams(componentID uuid.UUID) []runnerapi.Param {
	entries, err := a.variableGroups.ComponentGroupParams(componentID)
	if err != nil {
		return nil
	}
	out := make([]runnerapi.Param, 0, len(entries))
	for _, e := range entries {
		out = append(out, runnerapi.Param{Name: e.Key, Value: e.Value})
	}
	return out
}
