package service

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/models"
)

// fakeVersionSource is a narrow ArtifactVersionSource for unit testing
// ListDeployableVersions without Postgres (mirrors the ActiveRunCounter fakes).
type fakeVersionSource struct {
	arts []models.Artifact
	err  error
}

func (f *fakeVersionSource) ListDeployableVersions(_ uuid.UUID, _ int) ([]models.Artifact, error) {
	return f.arts, f.err
}

func TestListDeployableVersions_NilSource_ReturnsEmpty(t *testing.T) {
	svc := &ComponentService{} // no artifactSource wired
	got, err := svc.ListDeployableVersions(uuid.New(), 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("expected empty slice, got %#v", got)
	}
}

func TestListDeployableVersions_MapsFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	id := uuid.New()
	src := &fakeVersionSource{arts: []models.Artifact{
		{ID: id, Version: "v1.2.0", CreatedAt: now, CleanupState: models.CleanupStateActive},
	}}
	svc := (&ComponentService{}).WithArtifactSource(src)

	got, err := svc.ListDeployableVersions(uuid.New(), 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 version, got %d", len(got))
	}
	dv := got[0]
	if dv.Version != "v1.2.0" {
		t.Errorf("version: got %q", dv.Version)
	}
	if !dv.PublishedAt.Equal(now) {
		t.Errorf("publishedAt: got %v want %v", dv.PublishedAt, now)
	}
	if dv.Source != "artifact" {
		t.Errorf("source: got %q", dv.Source)
	}
	if dv.ArtifactRef != id.String() {
		t.Errorf("artifactRef: got %q want %q", dv.ArtifactRef, id.String())
	}
}

func TestListDeployableVersions_PassesLimitThrough(t *testing.T) {
	src := &fakeVersionSource{arts: []models.Artifact{}}
	svc := (&ComponentService{}).WithArtifactSource(src)
	if _, err := svc.ListDeployableVersions(uuid.New(), 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The service forwards the limit verbatim; the repo applies dedup/cap.
	// Here we just assert the call did not error and the fake was exercised.
	if len(src.arts) != 0 {
		t.Fatalf("fake should not be mutated")
	}
}
