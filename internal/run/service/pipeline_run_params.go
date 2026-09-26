package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	uuid "github.com/google/uuid"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	runmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"

	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
)

// ArtifactRegistry is the G-2 surface the run service uses to register
// artifacts a task declared in its Produces list once the task reaches
// Succeeded. Implemented by the artifact service (wired in cmd/hub/main.go
// via SetArtifactRegistrar); nil disables registration (tests, and hubs
// without the artifact subsystem).
type ArtifactRegistry interface {
	RegisterProduced(componentID, runID, taskRunID uuid.UUID, taskName, version string, storageKeys []string)
}

// SetArtifactRegistrar wires the G-2 completion hook (see ArtifactRegistry).
func (s *PipelineRunService) SetArtifactRegistrar(r ArtifactRegistry) { s.artifactRegistry = r }

// SetJobServiceAccount wires the G-1 SA name injected into every dispatched
// PipelineRunSpec (config SDP_JOB_SERVICE_ACCOUNT). Empty disables injection.
func (s *PipelineRunService) SetJobServiceAccount(name string) { s.jobServiceAccount = name }

// paramSubst is the hub-side half of the G-5 parameter-injection fix: task
// templates are stored as static definitions, so `${KEY}` placeholders in
// command/args/scriptArgs/produces/consumes and inside ReleaseConfig
// (manifest content, chart values, chart version) are expanded here, at
// trigger time, with the run's params. Only the braced form is substituted —
// bare `$KEY` is left alone so shell variables in user scripts are never
// rewritten. The runner-side half (params → container env) covers plain
// `$KEY` shell references.
var paramSubst = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// substituteParams returns s with every ${KEY} replaced by the matching
// param value. Unknown keys are left verbatim (visible in logs instead of
// silently vanishing into empty strings).
func substituteParams(s string, params map[string]string) string {
	if len(params) == 0 || !strings.Contains(s, "${") {
		return s
	}
	return paramSubst.ReplaceAllStringFunc(s, func(m string) string {
		key := m[2 : len(m)-1]
		if v, ok := params[key]; ok {
			return v
		}
		return m
	})
}

func substituteParamsSlice(ss []string, params map[string]string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = substituteParams(s, params)
	}
	return out
}

func paramsMap(params []runnerapi.Param) map[string]string {
	if len(params) == 0 {
		return nil
	}
	m := make(map[string]string, len(params))
	for _, p := range params {
		if p.Name != "" {
			m[p.Name] = p.Value
		}
	}
	return m
}

// substituteReleaseSpec expands ${KEY} in the parts of a ReleaseSpec that
// carry user content: manifest YAML, chart values, chart version, and the
// target namespace override.
func substituteReleaseSpec(rs *runnerapi.ReleaseSpec, params map[string]string) {
	if rs == nil || len(params) == 0 {
		return
	}
	if rs.Manifest != nil {
		rs.Manifest.Content = substituteParams(rs.Manifest.Content, params)
	}
	if rs.Chart != nil {
		rs.Chart.Version = substituteParams(rs.Chart.Version, params)
	}
	for k, v := range rs.Values {
		rs.Values[k] = substituteParams(v, params)
	}
	rs.TargetNamespace = substituteParams(rs.TargetNamespace, params)
}

// registerProducedArtifacts is called from ApplyStatus when a task reaches
// Succeeded: it resolves the task template's Produces keys and hands them to
// the artifact registry (G-2 收口). Best-effort — a registration failure must
// never fail the status update itself.
func (s *PipelineRunService) registerProducedArtifacts(run *runmodels.PipelineRun, ts runnerapi.TaskRunStatusSummary) {
	if s.artifactRegistry == nil || ts.Phase != runnerapi.TaskRunSucceeded {
		return
	}
	tr, err := s.taskRepo.GetByRunAndTaskName(run.ID, ts.Name)
	if err != nil || tr == nil {
		applog.Warnf("run: G-2 artifact registration: task run %q not found for run %s: %v", ts.Name, run.ID, err)
		return
	}
	keys, err := s.producesForTask(run.PipelineID, ts.Name)
	if err != nil {
		applog.Warnf("run: G-2 artifact registration: cannot resolve produces for task %q: %v", ts.Name, err)
		return
	}
	if len(keys) == 0 {
		return
	}
	// 模板里的 Produces 是静态定义，与触发时 buildSpec 相同地做 ${KEY} 替换，
	// 否则登记的 key 与实际上传的 key（已替换）对不上（2026-09-26 实测踩中）。
	var params []runnerapi.Param
	if len(run.Params) > 0 {
		_ = json.Unmarshal(run.Params, &params)
	}
	keys = substituteParamsSlice(keys, paramsMap(params))
	p, err := s.pipelineRepo.GetByID(run.PipelineID)
	if err != nil {
		applog.Warnf("run: G-2 artifact registration: pipeline %s not found: %v", run.PipelineID, err)
		return
	}
	version := runVersion(run)
	s.artifactRegistry.RegisterProduced(p.ComponentID, run.ID, tr.ID, ts.Name, version, keys)
	applog.Infof("run: G-2 registered %d artifact(s) for run %s task %q (version=%s)", len(keys), run.ID, ts.Name, version)
}

// producesForTask looks up the task template by name across the pipeline's
// stages and returns its (already param-substituted at trigger time)
// Produces keys.
func (s *PipelineRunService) producesForTask(pipelineID uuid.UUID, taskName string) ([]string, error) {
	stages, err := s.stageRepo.ListByPipelineID(pipelineID)
	if err != nil {
		return nil, err
	}
	for _, stage := range stages {
		templates, err := s.taskTemplateRepo.ListByStageID(stage.ID)
		if err != nil {
			return nil, err
		}
		for _, tpl := range templates {
			if tpl.Name == taskName {
				var keys []string
				if len(tpl.Produces) > 0 && string(tpl.Produces) != "null" {
					if err := json.Unmarshal(tpl.Produces, &keys); err != nil {
						return nil, fmt.Errorf("task %q produces: %w", taskName, err)
					}
				}
				return keys, nil
			}
		}
	}
	return nil, nil
}

// runVersion derives a display version for artifacts from the run's params:
// a VERSION param wins; otherwise the conventional middle segment of the
// storage key ("components/<cid>/<version>/<name>") is used. Fallback is a
// run-scoped pseudo version.
func runVersion(run *runmodels.PipelineRun) string {
	var params []runnerapi.Param
	if len(run.Params) > 0 {
		_ = json.Unmarshal(run.Params, &params)
	}
	for _, p := range params {
		if p.Name == "VERSION" && p.Value != "" {
			return p.Value
		}
	}
	return "run-" + run.ID.String()[:8]
}
