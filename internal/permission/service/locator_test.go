package service

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	pipelinemodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	runmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// stubPipelineLookup / stubRunLookup stand in for the repositories so the
// resolution rules under test never touch Postgres.
type stubPipelineLookup struct {
	row    *pipelinemodels.Pipeline
	err    error
	gotIDs []uuid.UUID
}

func (s *stubPipelineLookup) GetByID(id uuid.UUID) (*pipelinemodels.Pipeline, error) {
	s.gotIDs = append(s.gotIDs, id)
	if s.err != nil {
		return nil, s.err
	}
	return s.row, nil
}

type stubRunLookup struct {
	row *runmodels.PipelineRun
	err error
}

func (s *stubRunLookup) GetByID(id uuid.UUID) (*runmodels.PipelineRun, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.row, nil
}

func TestComponentOfPipeline_ResolvesOwningComponent(t *testing.T) {
	pipelineID, componentID := uuid.New(), uuid.New()
	lookup := &stubPipelineLookup{row: &pipelinemodels.Pipeline{ComponentID: componentID}}
	loc := NewComponentLocator(lookup, &stubRunLookup{})

	got, err := loc.ComponentOfPipeline(pipelineID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != componentID {
		t.Errorf("component = %s, want %s", got, componentID)
	}
	if len(lookup.gotIDs) != 1 || lookup.gotIDs[0] != pipelineID {
		t.Errorf("lookup called with %v, want [%s]", lookup.gotIDs, pipelineID)
	}
}

func TestComponentOfPipeline_MissingRowIsResourceNotFound(t *testing.T) {
	loc := NewComponentLocator(&stubPipelineLookup{err: gorm.ErrRecordNotFound}, &stubRunLookup{})

	_, err := loc.ComponentOfPipeline(uuid.New())
	if !errors.Is(err, common.ErrResourceNotFound) {
		t.Fatalf("err = %v, want common.ErrResourceNotFound", err)
	}
}

func TestComponentOfPipeline_StoreFailureIsNotReportedAsMissing(t *testing.T) {
	// A dead connection must not masquerade as "this pipeline does not
	// exist": the middleware turns the sentinel into a 404, so leaking it
	// here would hide an outage behind a plausible-looking answer.
	boom := errors.New("connection refused")
	loc := NewComponentLocator(&stubPipelineLookup{err: boom}, &stubRunLookup{})

	_, err := loc.ComponentOfPipeline(uuid.New())
	if errors.Is(err, common.ErrResourceNotFound) {
		t.Fatal("store failure was mapped to ErrResourceNotFound")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the original store error", err)
	}
}

func TestComponentOfRun_ResolvesThroughPipeline(t *testing.T) {
	runID, pipelineID, componentID := uuid.New(), uuid.New(), uuid.New()
	runs := &stubRunLookup{row: &runmodels.PipelineRun{PipelineID: pipelineID}}
	pipelines := &stubPipelineLookup{row: &pipelinemodels.Pipeline{ComponentID: componentID}}
	loc := NewComponentLocator(pipelines, runs)

	got, err := loc.ComponentOfRun(runID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != componentID {
		t.Errorf("component = %s, want %s", got, componentID)
	}
	// Two hops: run -> pipeline -> component.
	if len(pipelines.gotIDs) != 1 || pipelines.gotIDs[0] != pipelineID {
		t.Errorf("pipeline lookup called with %v, want [%s]", pipelines.gotIDs, pipelineID)
	}
}

func TestComponentOfRun_UnknownRunIsResourceNotFound(t *testing.T) {
	loc := NewComponentLocator(&stubPipelineLookup{}, &stubRunLookup{err: gorm.ErrRecordNotFound})

	_, err := loc.ComponentOfRun(uuid.New())
	if !errors.Is(err, common.ErrResourceNotFound) {
		t.Fatalf("err = %v, want common.ErrResourceNotFound", err)
	}
}

func TestComponentOfRun_UnknownPipelineIsResourceNotFound(t *testing.T) {
	runs := &stubRunLookup{row: &runmodels.PipelineRun{PipelineID: uuid.New()}}
	loc := NewComponentLocator(&stubPipelineLookup{err: gorm.ErrRecordNotFound}, runs)

	_, err := loc.ComponentOfRun(uuid.New())
	if !errors.Is(err, common.ErrResourceNotFound) {
		t.Fatalf("err = %v, want common.ErrResourceNotFound", err)
	}
}
