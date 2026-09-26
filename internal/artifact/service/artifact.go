package service

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/repository"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/storage"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
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

// RegisterProduced implements the run service's ArtifactRegistry interface
// (G-2 收口，2026-09-26)：当一个声明了 Produces 的任务 Succeeded 时，由
// ApplyStatus 调用，把每个 storage key 登记成制品行 —— 此前 Register 全仓
// 无调用方，上传成功后前端制品 Tab 恒空。Best-effort：单条失败记日志继续，
// 绝不让登记失败影响运行状态回写。重复 key 幂等跳过（同任务重跑场景）。
func (s *ArtifactService) RegisterProduced(componentID, runID, taskRunID uuid.UUID, taskName, version string, storageKeys []string) {
	for _, key := range storageKeys {
		if key == "" {
			continue
		}
		// G-14 idempotency: the completion hook fires on every Succeeded
		// status update (re-reports, reconcile replays), so skip keys that
		// are already registered for this component instead of piling up
		// duplicate rows. First-write-wins: later runs re-producing the same
		// key keep the original provenance (runID/taskRunID).
		exists, err := s.repo.ExistsByComponentAndKey(componentID, key)
		if err != nil {
			applog.Warnf("artifact: G-2 register produced key=%q run=%s task=%q existence probe failed: %v", key, runID, taskName, err)
		} else if exists {
			continue
		}
		artifactType := classifyArtifact(key)
		a := &models.Artifact{
			ComponentID:   componentID,
			PipelineRunID: &runID,
			TaskRunID:     &taskRunID,
			Version:       version,
			ArtifactType:  artifactType,
			StorageKey:    key,
		}
		if err := s.repo.Create(a); err != nil {
			applog.Warnf("artifact: G-2 register produced key=%q run=%s task=%q failed: %v", key, runID, taskName, err)
		}
	}
}

// classifyArtifact infers the artifact type from the storage key extension.
func classifyArtifact(key string) string {
	switch {
	case strings.HasSuffix(key, ".tgz"), strings.HasSuffix(key, ".tar.gz"), strings.HasSuffix(key, ".tar"):
		return "archive"
	case strings.HasSuffix(key, ".whl"), strings.HasSuffix(key, ".jar"), strings.HasSuffix(key, ".bin"):
		return "binary"
	case strings.HasSuffix(key, ".img"), strings.HasSuffix(key, ".image"):
		return "image"
	default:
		return "generic"
	}
}

// KeyDownloadURL mints a short-lived signed GET URL for an arbitrary storage
// key — no artifact row required. Used by the runner's consume init
// container (G-6) to fetch upstream task outputs, which live in the object
// store but are (deliberately) not necessarily registered as browsable
// artifacts.
func (s *ArtifactService) KeyDownloadURL(key string) (string, error) {
	if s.store == nil {
		return "", storage.ErrNotConfigured
	}
	return s.store.PresignDownload(key, s.urlExpiry)
}

func (s *ArtifactService) Get(id uuid.UUID) (*models.Artifact, error) { return s.repo.GetByID(id) }

func (s *ArtifactService) ListByComponent(componentID uuid.UUID, p common.Pagination) ([]models.Artifact, int64, error) {
	return s.repo.FindByComponentID(componentID, p)
}

// Delete removes the DB row, then best-effort deletes the underlying object —
// but no longer swallows the object error (backlog B-16 源头治理 /
// DELETE-CONTRACT §6.6-4 决策 4 第 1 步).
//
// 顺序与语义:
//   - 元数据行是权威记录，先删它；删成功即本次调用成功。
//   - 对象删除是**事后清理**。失败不会被吞掉，而是记结构化日志（artifact id /
//     component / storage key / 原因）—— 这正是"孤儿对象"的来源，必须可观测。
//   - 不把对象删除失败升级成 API 错误：元数据已经删了，返回失败会让调用方以为
//     行还在并去重试，而重试只会拿到 404 —— 那是在对调用方撒谎。
//   - 清理失败目前没有自动重试/标记列（`cleanup_state`），由对账任务兜底：那属
//     backlog B-16 的"对账（仅报告）"项，按 §6.6-4 排期在本轮之后。
func (s *ArtifactService) Delete(id uuid.UUID) error {
	a, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	if s.store == nil {
		// 对象存储未配置：没有对象可删，元数据删除已是全部工作。
		return nil
	}
	if err := s.store.Delete(a.StorageKey); err != nil {
		applog.Warnf("artifact: object cleanup failed (orphan object possible) id=%s component=%s key=%s err=%v",
			a.ID, a.ComponentID, a.StorageKey, err)
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
