package runnerproto

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// RepoSource describes where to check out source code from for script-based
// tasks (build.sh / run.sh / test.sh style execution). The hub resolves
// branch/tag names to a commit SHA at trigger time so every task in the same
// PipelineRun checks out the exact same commit.
type RepoSource struct {
	// URL is the git remote, e.g. https://github.com/org/repo.git
	URL string `json:"url"`
	// Ref is a commit SHA (recommended), resolved by the hub before dispatch.
	Ref string `json:"ref"`
	// Path is the subdirectory to treat as the working directory for monorepos
	// where one component lives in a subfolder.
	Path string `json:"path,omitempty"`
	// SecretRef names a Secret (already present in TargetNamespace) holding
	// git credentials, for private repos.
	SecretRef string `json:"secretRef,omitempty"`
}

// RetryPolicy defines retry behavior for a failed task.
type RetryPolicy struct {
	MaxRetries int32 `json:"maxRetries,omitempty"`
	// Backoff between retries, e.g. "30s". Empty means retry immediately.
	Backoff metav1.Duration `json:"backoff,omitempty"`
}

// PipelineTaskSpec defines a single node in the pipeline DAG.
type PipelineTaskSpec struct {
	// Name must be unique within the PipelineRun; referenced by DependsOn.
	Name string `json:"name"`
	// Type is the task category.
	Type PipelineTaskType `json:"type"`
	// Stage groups this task for UI display and sequencing. Tasks sharing a
	// Stage run in parallel (no implicit dependency between them); the hub
	// auto-generates DependsOn so every task in a later Stage depends on
	// every task in the immediately preceding Stage.
	Stage string `json:"stage,omitempty"`
	// ExecutionMode overrides how siblings within the same Stage are ordered
	// (defaults to ExecutionModeParallel).
	ExecutionMode ExecutionMode `json:"executionMode,omitempty"`
	// Image is the execution environment the script runs in.
	Image string `json:"image,omitempty"`
	// ScriptPath is a path relative to the checked-out repo root.
	ScriptPath string `json:"scriptPath,omitempty"`
	// ScriptArgs are passed positionally to the script.
	ScriptArgs []string `json:"scriptArgs,omitempty"`
	// Command/Args remain available as an escape hatch for tasks that don't
	// follow the script convention.
	Command []string `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	// Repo overrides PipelineRunSpec.Repo for this task only; leave nil to
	// inherit the run-level checkout. Approval tasks ignore this field.
	Repo *RepoSource `json:"repo,omitempty"`
	// Produces lists artifact keys this task uploads to the artifact store
	// after ScriptPath exits 0.
	Produces []string `json:"produces,omitempty"`
	// Consumes lists artifact keys (produced by any upstream task in this
	// same PipelineRun) that the runner downloads into the working directory
	// before ScriptPath runs.
	Consumes []string `json:"consumes,omitempty"`
	// DependsOn lists upstream task names that must reach Succeeded before
	// this task becomes runnable.
	DependsOn []string `json:"dependsOn,omitempty"`
	// Privileged (G-4) runs the task container with
	// securityContext.privileged=true so docker-in-docker / containerd-in-
	// containerd build images work. Opt-in per task.
	Privileged  bool         `json:"privileged,omitempty"`
	RetryPolicy *RetryPolicy `json:"retryPolicy,omitempty"`
	// TimeoutSeconds bounds how long the corresponding TaskRun may run before
	// being marked Failed. 0 means use the pipeline-level default.
	TimeoutSeconds int64 `json:"timeoutSeconds,omitempty"`
	// ApprovalConfig is required when Type == Approval, ignored otherwise.
	ApprovalConfig *ApprovalConfig `json:"approvalConfig,omitempty"`
	// RolloutSpec, when set, drives a progressive rollout (canary) for a
	// Release task instead of a one-shot apply. Ignored for Build/Approval.
	RolloutSpec *RolloutSpec `json:"rolloutSpec,omitempty"`
	// ReleaseSpec is required when Type == Release; it describes the chart or
	// manifest to apply plus the values injected from parameter management.
	ReleaseSpec *ReleaseSpec `json:"releaseSpec,omitempty"`
}

// ApprovalConfig describes who must approve an Approval-type task.
type ApprovalConfig struct {
	// RequiredApprovals is the number of distinct approvers needed to proceed.
	RequiredApprovals int32 `json:"requiredApprovals"`
	// AllowedApprovers restricts who may approve by identity (email/user ID,
	// resolved and enforced by the hub). Empty means anyone with project
	// access may approve.
	AllowedApprovers []string `json:"allowedApprovers,omitempty"`
	// TimeoutSeconds auto-fails the PipelineRun if no decision is made in
	// time. 0 means wait indefinitely.
	TimeoutSeconds int64 `json:"timeoutSeconds,omitempty"`
}

// PipelineRunSpec defines the desired state of a PipelineRun. A PipelineRun
// is created by the hub (via the runner's long-lived connection) whenever a
// pipeline is triggered against a specific environment on this cluster.
type PipelineRunSpec struct {
	// PipelineRef optionally names a reusable pipeline definition owned by the
	// hub, for display/audit purposes only — the runner never fetches it;
	// Tasks below is always the full, already-resolved DAG.
	PipelineRef string `json:"pipelineRef,omitempty"`
	// Tasks is the fully resolved DAG definition to execute.
	Tasks []PipelineTaskSpec `json:"tasks"`
	// Repo is the default checkout source for every script-based task in this
	// run; individual tasks may override via PipelineTaskSpec.Repo.
	Repo   *RepoSource `json:"repo,omitempty"`
	Params []Param     `json:"params,omitempty"`
	// TenantID / ProjectID / EnvironmentID carry multi-tenancy context down to
	// the runner so it can apply namespace routing, quotas, and RBAC scoping.
	TenantID      string `json:"tenantID"`
	ProjectID     string `json:"projectID"`
	EnvironmentID string `json:"environmentID"`
	// TargetNamespace is the namespace the runner creates Jobs/TaskRuns in.
	// Convention: {tenant}-{project}-{env}.
	TargetNamespace    string `json:"targetNamespace"`
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
	// TimeoutSeconds bounds the whole pipeline run; 0 means no overall limit.
	TimeoutSeconds int64 `json:"timeoutSeconds,omitempty"`
	// TriggeredBy records the hub-side user/system identity that started this
	// run, for audit display via kubectl.
	TriggeredBy string `json:"triggeredBy,omitempty"`
}

// ReleaseSourceType selects how a Release task applies its software unit.
type ReleaseSourceType string

const (
	// ReleaseSourceChart applies a Helm chart (helm upgrade --install).
	ReleaseSourceChart ReleaseSourceType = "Chart"
	// ReleaseSourceManifest applies raw YAML manifest(s) via kubectl apply.
	ReleaseSourceManifest ReleaseSourceType = "Manifest"
)

// ChartSource identifies a Helm chart to release.
type ChartSource struct {
	// RepoURL is the Helm repository URL (e.g. https://charts.example.com).
	// Omit for charts served from the default/cluster-local registry.
	RepoURL string `json:"repoURL,omitempty"`
	// Name is the chart name (e.g. "nginx").
	Name string `json:"name"`
	// Version pins the chart version; empty means latest.
	Version string `json:"version,omitempty"`
}

// ManifestSource carries inline YAML manifest(s) applied verbatim.
type ManifestSource struct {
	// Content is one or more YAML documents applied via `kubectl apply -f -`.
	Content string `json:"content"`
}

// ReleaseSpec defines a Release task: apply a software unit (chart or
// manifest) into the target cluster, with values injected from the hub's
// parameter management. An optional Canary sub-spec turns the one-shot apply
// into a progressive rollout driven by the existing Rollout machinery.
type ReleaseSpec struct {
	// Source selects Chart vs Manifest application.
	Source ReleaseSourceType `json:"source"`
	Chart  *ChartSource      `json:"chart,omitempty"`
	// Manifest is required when Source == Manifest.
	Manifest *ManifestSource `json:"manifest,omitempty"`
	// Values are injected (from the hub's parameter management) as Helm
	// --set pairs (chart) or object fields (manifest).
	Values map[string]string `json:"values,omitempty"`
	// TargetNamespace overrides the run namespace for this release.
	TargetNamespace string `json:"targetNamespace,omitempty"`
	// Canary, when set, drives a progressive rollout via the Rollout
	// controller instead of a single apply.
	Canary *RolloutSpec `json:"canary,omitempty"`
}

// TrafficRoutingType selects how canary traffic is split.
type TrafficRoutingType string

const (
	// TrafficRoutingDeploymentWeight splits traffic by adjusting the ratio of
	// stable vs. canary Pod replicas behind a single Service selector.
	TrafficRoutingDeploymentWeight TrafficRoutingType = "DeploymentWeight"
	// TrafficRoutingIngressCanary uses an Ingress controller's native
	// canary-weight annotation for precise percentage splits.
	TrafficRoutingIngressCanary TrafficRoutingType = "IngressCanary"
)

// CanaryStep is one step of a progressive rollout: shift weight, then
// optionally pause before the controller evaluates health and proceeds.
type CanaryStep struct {
	// SetWeight is the percentage of traffic (0-100) routed to the canary
	// version.
	SetWeight *int32 `json:"setWeight,omitempty"`
	// Pause holds the rollout at the current weight. Omit both SetWeight and
	// Pause is invalid; a step must do at least one of the two.
	Pause *PauseStep `json:"pause,omitempty"`
}

// PauseStep defines how long to pause at a given step.
type PauseStep struct {
	// Duration to pause for; if omitted, pause is indefinite until resumed
	// externally.
	Duration *metav1.Duration `json:"duration,omitempty"`
}

// HealthCheckType selects how the rollout controller judges canary health at
// each step before advancing to the next one.
type HealthCheckType string

const (
	HealthCheckPodReady        HealthCheckType = "PodReady"
	HealthCheckHTTPProbe       HealthCheckType = "HTTPProbe"
	HealthCheckPrometheusQuery HealthCheckType = "PrometheusQuery"
)

// HealthCheckSpec defines how canary health is evaluated at each step.
type HealthCheckSpec struct {
	// Type selects the health-check mechanism.
	Type HealthCheckType `json:"type"`
	// IntervalSeconds between health evaluations.
	IntervalSeconds int64 `json:"intervalSeconds,omitempty"`
	// FailureThreshold is the number of consecutive failed checks before the
	// controller triggers a rollback (if AutoRollback is true).
	FailureThreshold int32 `json:"failureThreshold,omitempty"`
	// HTTPPath is used when Type == HTTPProbe, evaluated against the canary
	// Pods directly.
	HTTPPath string `json:"httpPath,omitempty"`
	// PrometheusQueryExpr / PrometheusThreshold are used when Type ==
	// PrometheusQuery.
	PrometheusQueryExpr string `json:"prometheusQueryExpr,omitempty"`
	PrometheusThreshold string `json:"prometheusThreshold,omitempty"`
}

// TrafficRoutingSpec defines how traffic is actually split between stable and
// canary versions of the workload.
type TrafficRoutingSpec struct {
	// Type selects the traffic-routing mechanism.
	Type TrafficRoutingType `json:"type"`
	// IngressRef is required when Type == IngressCanary.
	IngressRef string `json:"ingressRef,omitempty"`
}

// RolloutSpec defines the desired state of a Rollout. Created by the
// PipelineRun/TaskRun controller for Deploy-type tasks, or directly by an
// operator for a standalone progressive deployment.
type RolloutSpec struct {
	// WorkloadRef is the name of the Deployment being progressively updated.
	WorkloadRef string `json:"workloadRef"`
	// Replicas is the desired total replica count for the workload. When set
	// it overrides the runner's default; when nil the runner reads the live
	// stable Deployment's spec.replicas.
	Replicas    *int32 `json:"replicas,omitempty"`
	StableImage string `json:"stableImage"`
	CanaryImage string `json:"canaryImage"`
	// Steps defines the full progressive delivery plan, evaluated in order.
	Steps          []CanaryStep       `json:"steps"`
	HealthCheck    HealthCheckSpec    `json:"healthCheck"`
	TrafficRouting TrafficRoutingSpec `json:"trafficRouting"`
	// AutoRollback, when true, automatically reverts to 0% canary weight once
	// HealthCheck.FailureThreshold consecutive checks fail.
	AutoRollback bool `json:"autoRollback,omitempty"`
}
