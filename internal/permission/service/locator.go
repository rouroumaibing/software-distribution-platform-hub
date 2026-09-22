package service

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	pipelinemodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	runmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// pipelineLookup / runLookup are the narrow slices of the pipeline and run
// repositories ComponentLocator needs — same motive as the other narrow
// stores in this package: the resolution rules stay testable without
// Postgres. *pipelinerepository.PipelineRepository and
// *runrepository.PipelineRunRepository satisfy them as-is.
type pipelineLookup interface {
	GetByID(id uuid.UUID) (*pipelinemodels.Pipeline, error)
}

type runLookup interface {
	GetByID(id uuid.UUID) (*runmodels.PipelineRun, error)
}

// ComponentLocator resolves a pipeline or run id to the component that owns
// it. The authorization middleware only knows how to check a binding on a
// component, but several routes are addressed by their own pipeline / run id;
// without this hop those routes would hand a pipeline id (or a run id!) to
// the component-binding lookup, match nothing, and deny every request the
// moment auth is switched on (ACCOUNT-PERMISSION-MODEL §10 #14).
type ComponentLocator struct {
	pipelines pipelineLookup
	runs      runLookup
}

func NewComponentLocator(pipelines pipelineLookup, runs runLookup) *ComponentLocator {
	return &ComponentLocator{pipelines: pipelines, runs: runs}
}

// ComponentOfPipeline returns the component a pipeline belongs to.
func (l *ComponentLocator) ComponentOfPipeline(pipelineID uuid.UUID) (uuid.UUID, error) {
	p, err := l.pipelines.GetByID(pipelineID)
	if err != nil {
		return uuid.Nil, resourceMissing(err)
	}
	return p.ComponentID, nil
}

// ComponentOfRun returns the component owning the pipeline the run was
// triggered from. A run row carries no component_id of its own, so this is
// deliberately two hops (run -> pipeline -> component): the pipeline is the
// only thing that knows its component, and duplicating that onto runs would
// create a second source of truth for "who owns this run".
func (l *ComponentLocator) ComponentOfRun(runID uuid.UUID) (uuid.UUID, error) {
	run, err := l.runs.GetByID(runID)
	if err != nil {
		return uuid.Nil, resourceMissing(err)
	}
	return l.ComponentOfPipeline(run.PipelineID)
}

// resourceMissing normalises a store error into common.ErrResourceNotFound
// when the row simply isn't there; anything else (connection down, pool
// exhausted, ...) stays as-is so it surfaces as a 500 instead of being
// reported to the caller as "this resource does not exist".
func resourceMissing(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return common.ErrResourceNotFound
	}
	return err
}
