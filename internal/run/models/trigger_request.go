package models

import (
	"github.com/google/uuid"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"
)

// TriggerRequest is the JSON body of POST /pipelines/:pipelineId/runs. It
// carries the trigger-time inputs that aren't derivable from the pipeline
// definition itself: which cluster(s) to run on, the git source/ref to check
// out, pipeline params, and who triggered it.
type TriggerRequest struct {
	// ClusterID overrides cluster selection. If nil/empty, the hub picks the
	// first online cluster. Ignored when TargetClusters is set.
	ClusterID *uuid.UUID `json:"clusterId,omitempty"`

	// TargetClusters fans the trigger out to multiple environments: one
	// independent PipelineRun is created per listed cluster (each keeps the
	// 1:1 run↔cluster invariant). Targets only need to EXIST, not be online —
	// the durable dispatch queue delivers each run once its Runner reconnects.
	// When set, ClusterID is ignored.
	TargetClusters []uuid.UUID `json:"targetClusters,omitempty"`

	// TargetNamespace is the K8s namespace the runner creates Jobs in.
	// Defaults to "sdp-run" when empty.
	TargetNamespace string `json:"targetNamespace,omitempty"`

	// RepoURL/RepoRef/RepoPath describe the source checkout for script tasks.
	// The hub resolves Ref to a concrete commit SHA at trigger time.
	RepoURL  string `json:"repoUrl,omitempty"`
	RepoRef  string `json:"repoRef,omitempty"`
	RepoPath string `json:"repoPath,omitempty"`

	// Params are substituted into task command/args/env by the runner.
	Params []runnerapi.Param `json:"params,omitempty"`

	// CommitSHA records the resolved commit for audit display.
	CommitSHA string `json:"commitSha,omitempty"`

	// TriggeredBy is the hub-side user/system identity starting the run.
	TriggeredBy string `json:"triggeredBy,omitempty"`
}
