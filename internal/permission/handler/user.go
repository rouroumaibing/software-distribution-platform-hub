package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/service"
)

type UserHandler struct{ svc *service.UserService }

func NewUserHandler(svc *service.UserService) *UserHandler { return &UserHandler{svc: svc} }

func (h *UserHandler) RegisterRoutes(rg *gin.RouterGroup) {
	common.RegisterCRUD[models.User](rg, "/users", h.svc)
	rg.GET("/users", h.List)
}

func (h *UserHandler) List(c *gin.Context) {
	p := common.ParsePagination(c)
	items, total, err := h.svc.List(p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}
