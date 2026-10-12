package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/repository"
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
	// G-6：按 storage key 签发 GET URL（不要求登记过制品行），runner 的
	// consume init 容器用它在流水线任务间搬运产物。
	rg.POST("/artifacts/storage-url", h.StorageURL)
	rg.DELETE("/artifacts/:id", h.Delete)
	// §6.2 跨组件检索（服务树第二视图数据源）。
	rg.GET("/artifacts", h.Search)
	// S-2 裁定的双通道「推」端点：外部扫描器回写安全字段。
	rg.POST("/artifacts/:id/scan-result", h.ScanResult)
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
	common.OKNoEscape(c, gin.H{"url": url, "expiresInSeconds": h.expirySeconds()})
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
	common.OKNoEscape(c, gin.H{"url": url, "expiresInSeconds": h.expirySeconds()})
}

func (h *ArtifactHandler) expirySeconds() int {
	return int(h.svc.URLExpiry().Seconds())
}

// StorageURL mints a signed GET URL for a raw storage key (G-6 consume).
// Unlike Download (by artifact id) this does not require the key to be
// registered — intra-pipeline hand-offs are ephemeral by design.
func (h *ArtifactHandler) StorageURL(c *gin.Context) {
	var req UploadURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	url, err := h.svc.KeyDownloadURL(req.Key)
	if err != nil {
		if errors.Is(err, storage.ErrNotConfigured) {
			common.Fail(c, http.StatusServiceUnavailable, err)
			return
		}
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	common.OKNoEscape(c, gin.H{"url": url, "expiresInSeconds": h.expirySeconds()})
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

// Search godoc
//
//	@Summary	跨组件制品检索（§6.2）
//	@Produce	json
//	@Param		version		query	string	false	"版本子串"
//	@Param		name		query	string	false	"storage key 子串"
//	@Param		componentId	query	string	false	"限定组件"
//	@Param		type		query	string	false	"image/binary/archive/generic"
//	@Param		since		query	string	false	"RFC3339 下界"
//	@Param		until		query	string	false	"RFC3339 上界"
//	@Param		scanStatus	query	string	false	"unscanned/scanning/scanned"
//	@Param		signed		query	string	false	"true/false"
//	@Success	200			{object}	common.Envelope
//	@Router		/artifacts [get]
func (h *ArtifactHandler) Search(c *gin.Context) {
	f := repository.ArtifactSearchFilter{}
	f.Version = c.Query("version")
	f.Name = c.Query("name")
	f.ArtifactType = c.Query("type")
	f.ScanStatus = c.Query("scanStatus")
	if v := c.Query("componentId"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			common.Fail(c, http.StatusBadRequest, err)
			return
		}
		f.ComponentID = id
	}
	parseTime := func(key string) (*time.Time, error) {
		v := c.Query(key)
		if v == "" {
			return nil, nil
		}
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, err
		}
		return &t, nil
	}
	var err error
	if f.Since, err = parseTime("since"); err != nil {
		common.Fail(c, http.StatusBadRequest, errors.New("since 须为 RFC3339"))
		return
	}
	if f.Until, err = parseTime("until"); err != nil {
		common.Fail(c, http.StatusBadRequest, errors.New("until 须为 RFC3339"))
		return
	}
	if v := c.Query("signed"); v != "" {
		b := v == "true"
		f.Signed = &b
	}
	p := common.ParsePagination(c)
	items, total, err := h.svc.Search(f, p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}

// ScanResult godoc
//
//	@Summary	回写制品扫描结果（S-2 双通道推端点）
//	@Accept		json
//	@Produce	json
//	@Param		body	body	service.ScanResultInput	true	"扫描器聚合结果，全量覆盖"
//	@Success	200		{object}	common.Envelope
//	@Router		/artifacts/{id}/scan-result [post]
func (h *ArtifactHandler) ScanResult(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var in service.ScanResultInput
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.ApplyScanResult(id, in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	common.OK(c, gin.H{"updated": true})
}
