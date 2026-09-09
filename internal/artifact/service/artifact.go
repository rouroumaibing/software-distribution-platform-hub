package service

import (
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/repository"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/storage"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// ArtifactService has no Update — artifacts are immutable once registered.
// store may be nil (object storage unconfigured): metadata endpoints keep
// working, but URL generation returns storage.ErrNotConfigured.
type ArtifactService struct {
	repo      *repository.ArtifactRepository
	store     storage.Client
	urlExpiry time.Duration
}

func NewArtifactService(repo *repository.ArtifactRepository, store storage.Client, urlExpiry time.Duration) *ArtifactService {
	if urlExpiry <= 0 {
		urlExpiry = 15 * time.Minute
	}
	return &ArtifactService{repo: repo, store: store, urlExpiry: urlExpiry}
}

// Register is called by the "归档阶段" task's status-sync handler once the
// script finishes uploading to object storage (via PresignUpload URL).
func (s *ArtifactService) Register(a *models.Artifact) error { return s.repo.Create(a) }

func (s *ArtifactService) Get(id uuid.UUID) (*models.Artifact, error) { return s.repo.GetByID(id) }

func (s *ArtifactService) ListByComponent(componentID uuid.UUID, p common.Pagination) ([]models.Artifact, int64, error) {
	return s.repo.FindByComponentID(componentID, p)
}

// Delete removes the DB row and best-effort deletes the underlying object.
func (s *ArtifactService) Delete(id uuid.UUID) error {
	a, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	if s.store != nil {
		_ = s.store.Delete(a.StorageKey) // object already unreferenced; nothing to roll back
	}
	return nil
}

// DownloadURL mints a short-lived signed URL against the object store
// (S3 presigned GET, or a hub-served HMAC link with the local driver).
func (s *ArtifactService) DownloadURL(id uuid.UUID) (string, error) {
	if s.store == nil {
		return "", storage.ErrNotConfigured
	}
	a, err := s.repo.GetByID(id)
	if err != nil {
		return "", err
	}
	return s.store.PresignDownload(a.StorageKey, s.urlExpiry)
}

// UploadURL mints a short-lived signed PUT URL so archive tasks can push a
// build deliverable straight to the object store without proxying bytes
// through the hub. key conventions live with the caller (e.g.
// "components/<id>/<version>/<name>").
func (s *ArtifactService) UploadURL(key string) (string, error) {
	if s.store == nil {
		return "", storage.ErrNotConfigured
	}
	return s.store.PresignUpload(key, s.urlExpiry)
}

// URLExpiry exposes the signed-URL lifetime for API responses.
func (s *ArtifactService) URLExpiry() time.Duration { return s.urlExpiry }
