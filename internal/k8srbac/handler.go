package k8srbac

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// ClusterPermissionHandler exposes the "集群权限" management API (ACCESS-CONTROL §7).
// Mounted on the platform group (/api/v1), so routes resolve to
// /api/v1/cluster-permissions/*.
type ClusterPermissionHandler struct {
	svc *Service
}

func NewClusterPermissionHandler(svc *Service) *ClusterPermissionHandler {
	return &ClusterPermissionHandler{svc: svc}
}

func (h *ClusterPermissionHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/cluster-permissions", h.Create)
	rg.GET("/cluster-permissions", h.List)
	rg.GET("/cluster-permissions/:id", h.Get)
	rg.POST("/cluster-permissions/:id/revoke", h.Revoke)
	rg.DELETE("/cluster-permissions/:id", h.Delete)
}

type clusterBindingResp struct {
	ID            string   `json:"id"`
	EnvironmentID string   `json:"environmentId"`
	SubjectKind   string   `json:"subjectKind"`
	SubjectID     string   `json:"subjectId"`
	SubjectName   string   `json:"subjectName"`
	Role          string   `json:"role"`
	ClusterScoped bool     `json:"clusterScoped"`
	Namespace     string   `json:"namespace"`
	Resources     []string `json:"resources"`
	Operations    []string `json:"operations"`
	CertHash      string   `json:"certHash"`
	Status        string   `json:"status"`
}

func toResp(b *ClusterBinding) clusterBindingResp {
	var res, ops []string
	_ = parseJSON(b.Resources, &res)
	_ = parseJSON(b.Operations, &ops)
	return clusterBindingResp{
		ID:            b.ID.String(),
		EnvironmentID: b.EnvironmentID.String(),
		SubjectKind:   b.SubjectKind,
		SubjectID:     b.SubjectID,
		SubjectName:   b.SubjectName,
		Role:          b.Role,
		ClusterScoped: b.ClusterScoped,
		Namespace:     b.Namespace,
		Resources:     res,
		Operations:    ops,
		CertHash:      b.CertHash,
		Status:        b.Status,
	}
}

type createResp struct {
	Binding    clusterBindingResp `json:"binding"`
	Kubeconfig string             `json:"kubeconfig,omitempty"`
}

func (h *ClusterPermissionHandler) Create(c *gin.Context) {
	var in CreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	b, kubeconfig, err := h.svc.Create(c.Request.Context(), &in)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.Created(c, createResp{Binding: toResp(b), Kubeconfig: kubeconfig})
}

func (h *ClusterPermissionHandler) List(c *gin.Context) {
	envID, err := uuid.Parse(c.Query("environmentId"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, errors.New("invalid environmentId"))
		return
	}
	bs, err := h.svc.ListByEnvironment(c.Request.Context(), envID)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	out := make([]clusterBindingResp, 0, len(bs))
	for i := range bs {
		out = append(out, toResp(&bs[i]))
	}
	common.OK(c, out)
}

func (h *ClusterPermissionHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, errors.New("invalid id"))
		return
	}
	b, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		common.Fail(c, http.StatusNotFound, err)
		return
	}
	common.OK(c, toResp(b))
}

// Revoke rotates a User subject's certificate (§9) or removes a Group/Org binding.
// For a User it returns a freshly-issued kubeconfig; for Group/Org it returns the
// binding (now revoked) with no kubeconfig.
func (h *ClusterPermissionHandler) Revoke(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, errors.New("invalid id"))
		return
	}
	b, kubeconfig, err := h.svc.Revoke(c.Request.Context(), id)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, createResp{Binding: toResp(b), Kubeconfig: kubeconfig})
}

func (h *ClusterPermissionHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, errors.New("invalid id"))
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Status(http.StatusNoContent)
}
