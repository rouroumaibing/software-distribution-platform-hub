package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/service"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
)

var errOrgRequired = errors.New("orgId/groupId 必填且必须是合法 uuid")

// VariableGroupHandler serves the shared variable-group API (ui-12 §5.3,
// N-26): org-scope group CRUD + component bindings. Group entries are
// plaintext variables only — secrets stay in credentials.
type VariableGroupHandler struct {
	svc *service.VariableGroupService
}

func NewVariableGroupHandler(svc *service.VariableGroupService) *VariableGroupHandler {
	return &VariableGroupHandler{svc: svc}
}

func (h *VariableGroupHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/orgs/:orgId/variable-groups", h.ListByOrg)
	rg.POST("/variable-groups", h.Create)
	rg.PUT("/variable-groups/:id", h.Update)
	rg.DELETE("/variable-groups/:id", h.Delete)
	rg.GET("/components/:id/variable-group-bindings", h.ListBindings)
	rg.POST("/components/:id/variable-group-bindings", h.Bind)
	rg.DELETE("/variable-group-bindings/:id", h.Unbind)
}

func (h *VariableGroupHandler) ListByOrg(c *gin.Context) {
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	rows, err := h.svc.ListByOrg(orgID)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, rows)
}

func (h *VariableGroupHandler) Create(c *gin.Context) {
	var in models.VariableGroup
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if in.OrgID == uuid.Nil {
		common.Fail(c, http.StatusBadRequest, errOrgRequired)
		return
	}
	by, _ := middleware.CurrentSubject(c)
	if err := h.svc.Create(&in, by); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, in)
}

func (h *VariableGroupHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var in models.VariableGroup
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	in.ID = id
	if err := h.svc.Update(&in); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, in)
}

func (h *VariableGroupHandler) Delete(c *gin.Context) {
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

func (h *VariableGroupHandler) ListBindings(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	rows, err := h.svc.ListBindings(componentID)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, rows)
}

// BindRequest is the body of the bind endpoint.
type BindRequest struct {
	GroupID  uuid.UUID `json:"groupId"`
	Priority int       `json:"priority"`
}

func (h *VariableGroupHandler) Bind(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var in BindRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if in.GroupID == uuid.Nil {
		common.Fail(c, http.StatusBadRequest, errOrgRequired)
		return
	}
	by, _ := middleware.CurrentSubject(c)
	if err := h.svc.Bind(componentID, in.GroupID, in.Priority, by); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{"bound": true})
}

func (h *VariableGroupHandler) Unbind(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Unbind(id); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Status(http.StatusNoContent)
}
