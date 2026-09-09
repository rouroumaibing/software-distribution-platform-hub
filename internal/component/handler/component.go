package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/service"
)

type ComponentHandler struct{ svc *service.ComponentService }

func NewComponentHandler(svc *service.ComponentService) *ComponentHandler {
	return &ComponentHandler{svc: svc}
}

func (h *ComponentHandler) RegisterRoutes(rg *gin.RouterGroup) {
	// POST/GET/:id/PUT/:id/DELETE/:id 全部由 common.RegisterCRUD 生成,
	// 因为 ComponentService 已经实现了 common.CRUDService[models.Component]。
	common.RegisterCRUD[models.Component](rg, "/components", h.svc)

	// 父级作用域的 List 因为每个模块的 scoping 方式不一样,单独写。
	// 参数名必须是 :id —— gin 要求同一路径位置的通配符同名,
	// 与 RegisterCRUD 生成的 GET /services/:id 共存。
	rg.GET("/services/:id/components", h.ListByService)
}

func (h *ComponentHandler) ListByService(c *gin.Context) {
	serviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	p := common.ParsePagination(c)
	items, total, err := h.svc.ListByService(serviceID, p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}
