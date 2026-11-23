package service

import (
	"strings"
	"testing"
)

// The Options constructor must fail loudly when a policy is switched on
// without its dependency; the old setter chain silently degraded instead.
func TestNewPipelineRunServiceWithOptionsRequiresEnabledDependencies(t *testing.T) {
	cases := []struct {
		name string
		opts PipelineRunOptions
		want string
	}{
		{"rollout", PipelineRunOptions{RolloutRefluxEnabled: true}, "RolloutRefluxEnabled"},
		{"environment", PipelineRunOptions{EnvironmentAttributionEnabled: true}, "EnvironmentAttributionEnabled"},
		{"production", PipelineRunOptions{ProductionApprovalEnabled: true}, "ProductionApprovalEnabled"},
		{"artifact", PipelineRunOptions{ArtifactRegistrationEnabled: true}, "ArtifactRegistrationEnabled"},
		{"variable", PipelineRunOptions{VariableTierResolutionEnabled: true}, "VariableTierResolutionEnabled"},
		{"approval", PipelineRunOptions{ApprovalPolicyEnabled: true}, "ApprovalPolicyEnabled"},
		{"component", PipelineRunOptions{ComponentGovernanceEnabled: true}, "ComponentGovernanceEnabled"},
		{"change window", PipelineRunOptions{ChangeWindowGuardEnabled: true}, "ChangeWindowGuardEnabled"},
		{"finished hook", PipelineRunOptions{RunFinishedHookEnabled: true}, "RunFinishedHookEnabled"},
		{"notify", PipelineRunOptions{NotifyEnabled: true}, "NotifyEnabled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewPipelineRunServiceWithOptions(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error mentioning %s, got %v", tc.want, err)
			}
		})
	}
}

func TestNewPipelineRunServiceWithOptionsDefaults(t *testing.T) {
	svc, err := NewPipelineRunServiceWithOptions(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, PipelineRunOptions{})
	if err != nil {
		t.Fatalf("zero options must be valid: %v", err)
	}
	if svc.sweepInterval <= 0 {
		t.Fatalf("sweep interval must default to a positive duration, got %s", svc.sweepInterval)
	}
	if svc.rolloutStore != nil || svc.productionPolicy != nil || svc.notifySink != nil {
		t.Fatal("disabled policies must leave dependencies unset")
	}
	if !strings.Contains(svc.PolicySummary(), "productionApproval=false") {
		t.Fatalf("summary should expose effective switches, got %q", svc.PolicySummary())
	}
}
