package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/service"
)

// PipelineVersionHandler exposes the define-time history of a pipeline: list /
// read / compare / roll back (C-09).
//
// 路由形状刻意让 diff 挂在 `:version` 之下（`.../versions/:version/diff?against=N`）
// 而不是 `.../versions/compare?from=&to=`：后者会让 gin 在同一层同时出现静态段
// `compare` 与参数段 `:version`，属于路由树冲突的老坑。挂在参数之下既无冲突，
// 语义也更清楚 —— "以 N 为基准，看 :version 改了什么"。
type PipelineVersionHandler struct {
	svc *service.PipelineVersionService
}

func NewPipelineVersionHandler(svc *service.PipelineVersionService) *PipelineVersionHandler {
	return &PipelineVersionHandler{svc: svc}
}

func (h *PipelineVersionHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/pipelines/:id/versions", h.List)
	rg.GET("/pipelines/:id/versions/:version", h.Get)
	rg.GET("/pipelines/:id/versions/:version/diff", h.Compare)
	rg.POST("/pipelines/:id/versions/:version/rollback", h.Rollback)
}

func parseVersion(c *gin.Context) (int, bool) {
	n, err := strconv.Atoi(c.Param("version"))
	if err != nil || n <= 0 {
		common.AbortWithError(c, common.ErrBadRequest.WithError(
			common.DomainError(common.KindPipeline, http.StatusBadRequest, 2, "invalid version (want a positive integer)")))
		return 0, false
	}
	return n, true
}

// List godoc
// @Summary List a pipeline's definition versions
// @Description Returns the append-only snapshot history (newest first): version number, author, stage/task counts, and whether the body differs from the previous version. Bodies are omitted — fetch one with GET /pipelines/:id/versions/:version.
// @Tags pipelines
// @Produce json
// @Param id path string true "Pipeline ID (UUID)"
// @Param limit query int false "max versions to return (default 50, max 200)"
// @Success 200 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Router /pipelines/{id}/versions [get]
func (h *PipelineVersionHandler) List(c *gin.Context) {
	pipelineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		n, perr := strconv.Atoi(raw)
		if perr != nil || n <= 0 {
			common.AbortWithError(c, common.ErrBadRequest.WithError(
				common.DomainError(common.KindPipeline, http.StatusBadRequest, 2, "invalid limit (want a positive integer)")))
			return
		}
		limit = n
	}
	items, err := h.svc.List(pipelineID, limit)
	if err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, items)
}

// Get godoc
// @Summary Get one pipeline definition version (full snapshot body)
// @Tags pipelines
// @Produce json
// @Param id path string true "Pipeline ID (UUID)"
// @Param version path int true "Version number"
// @Success 200 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 404 {object} common.Envelope
// @Router /pipelines/{id}/versions/{version} [get]
func (h *PipelineVersionHandler) Get(c *gin.Context) {
	pipelineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	version, ok := parseVersion(c)
	if !ok {
		return
	}
	item, err := h.svc.Get(pipelineID, version)
	if err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, item)
}

// Compare godoc
// @Summary Diff two versions of a pipeline definition
// @Description Returns the stage/task level differences from ?against=<n> to :version. Directional: added/removed swap if :version is the older one.
// @Tags pipelines
// @Produce json
// @Param id path string true "Pipeline ID (UUID)"
// @Param version path int true "Target version number"
// @Param against query int true "Baseline version number"
// @Success 200 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 404 {object} common.Envelope
// @Router /pipelines/{id}/versions/{version}/diff [get]
func (h *PipelineVersionHandler) Compare(c *gin.Context) {
	pipelineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	to, ok := parseVersion(c)
	if !ok {
		return
	}
	raw := c.Query("against")
	from, perr := strconv.Atoi(raw)
	if raw == "" || perr != nil || from <= 0 {
		common.AbortWithError(c, common.ErrBadRequest.WithError(
			common.DomainError(common.KindPipeline, http.StatusBadRequest, 2, "invalid against (want a positive integer version number)")))
		return
	}
	diff, err := h.svc.Compare(pipelineID, from, to)
	if err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, diff)
}

// Rollback godoc
// @Summary Roll a pipeline's structure back to a past version
// @Description Re-materializes the version's stages and task templates and records the result as a new version. Structure only — pipeline metadata (name/kind/description) is left alone. History is append-only: no version rows are rewritten or deleted.
// @Tags pipelines
// @Produce json
// @Param id path string true "Pipeline ID (UUID)"
// @Param version path int true "Version number to restore"
// @Success 200 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 404 {object} common.Envelope
// @Router /pipelines/{id}/versions/{version}/rollback [post]
func (h *PipelineVersionHandler) Rollback(c *gin.Context) {
	pipelineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	version, ok := parseVersion(c)
	if !ok {
		return
	}
	// 记录主体用 token 的 `sub`（D3 主体语义）；dev 模式下退回空串，不伪造身份。
	by, _ := middleware.CurrentSubject(c)
	res, err := h.svc.Rollback(pipelineID, version, by)
	if err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, res)
}
