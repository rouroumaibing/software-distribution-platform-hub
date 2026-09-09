package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/service"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/storage"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// ArtifactHandler is hand-written (no common.RegisterCRUD) because there's
// no Update route — artifacts are immutable, and download uses a signed
// URL rather than serving bytes directly through the hub.
type ArtifactHandler struct{ svc *service.ArtifactService }

func NewArtifactHandler(svc *service.ArtifactService) *ArtifactHandler {
	return &ArtifactHandler{svc: svc}
}

func (h *ArtifactHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/components/:id/artifacts", h.ListByComponent)
	rg.GET("/artifacts/:id", h.Get)
	rg.GET("/artifacts/:id/download", h.Download)
	// POST (not GET) so it doesn't collide with /artifacts/:id in gin's tree.
	rg.POST("/artifacts/upload-url", h.UploadURL)
	rg.DELETE("/artifacts/:id", h.Delete)
}

func (h *ArtifactHandler) ListByComponent(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	p := common.ParsePagination(c)
	items, total, err := h.svc.ListByComponent(componentID, p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}

func (h *ArtifactHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	item, err := h.svc.Get(id)
	if err != nil {
		common.Fail(c, http.StatusNotFound, err)
		return
	}
	common.OK(c, item)
}

func (h *ArtifactHandler) Download(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	url, err := h.svc.DownloadURL(id)
	if err != nil {
		if errors.Is(err, storage.ErrNotConfigured) {
			common.Fail(c, http.StatusServiceUnavailable, err)
			return
		}
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{"url": url, "expiresInSeconds": h.expirySeconds()})
}

// UploadURLRequest asks the hub to mint a signed PUT URL for an archive task
// to push a build deliverable straight into the object store.
type UploadURLRequest struct {
	// Key is the storage key, e.g. "components/<id>/<version>/<name>".
	Key string `json:"key" binding:"required"`
}

func (h *ArtifactHandler) UploadURL(c *gin.Context) {
	var req UploadURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	url, err := h.svc.UploadURL(req.Key)
	if err != nil {
		if errors.Is(err, storage.ErrNotConfigured) {
			common.Fail(c, http.StatusServiceUnavailable, err)
			return
		}
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	common.OK(c, gin.H{"url": url, "expiresInSeconds": h.expirySeconds()})
}

func (h *ArtifactHandler) expirySeconds() int {
	return int(h.svc.URLExpiry().Seconds())
}

func (h *ArtifactHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Delete(id); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Status(http.StatusNoContent)
}
