package service

import (
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/models"
)

// ArtifactVersionSource is the read side ComponentService needs to enumerate
// deployable versions (O-38). A narrow interface (mirrors ActiveRunCounter) so
// ListDeployableVersions is unit-testable without Postgres;
// *artifactrepo.ArtifactRepository satisfies it as-is.
type ArtifactVersionSource interface {
	ListDeployableVersions(componentID uuid.UUID, limit int) ([]models.Artifact, error)
}

// DeployableVersion is one O-38 list item: a distinct released version of a
// component that can be the target of a canary rollback. Source is always
// "artifact" in the current design (run-produced versions reuse the same
// artifact rows), kept as a field so a future second source is additive.
type DeployableVersion struct {
	Version     string    `json:"version"`
	PublishedAt time.Time `json:"publishedAt"`
	Source      string    `json:"source"`
	ArtifactRef string    `json:"artifactRef"`
}

// ListDeployableVersions enumerates the component's deployable versions for the
// canary rollback picker (O-38). A nil artifactSource (deps not wired) yields an
// empty slice rather than an error, so the endpoint degrades gracefully.
func (s *ComponentService) ListDeployableVersions(componentID uuid.UUID, limit int) ([]DeployableVersion, error) {
	if s.artifactSource == nil {
		return []DeployableVersion{}, nil
	}
	arts, err := s.artifactSource.ListDeployableVersions(componentID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]DeployableVersion, 0, len(arts))
	for _, a := range arts {
		out = append(out, DeployableVersion{
			Version:     a.Version,
			PublishedAt: a.CreatedAt,
			Source:      "artifact",
			ArtifactRef: a.ID.String(),
		})
	}
	return out, nil
}
