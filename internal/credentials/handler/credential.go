package handler

import (
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/credentials/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/credentials/service"
)

type CredentialHandler struct{ svc *service.CredentialService }

func NewCredentialHandler(svc *service.CredentialService) *CredentialHandler {
	return &CredentialHandler{svc: svc}
}

func (h *CredentialHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/credentials", h.List)
	rg.POST("/credentials", h.Create)
	rg.GET("/credentials/:id", h.Get)
	rg.PUT("/credentials/:id", h.Update)
	rg.DELETE("/credentials/:id", h.Delete)
	// Structural kubeconfig preview — authoritative validation stays server-side
	// (§7.12.3 ②); hub has no client-go, so this is a text/structural parse.
	rg.POST("/credentials/parse-kubeconfig", h.ParseKubeconfig)
}

func (h *CredentialHandler) List(c *gin.Context) {
	scope := c.Query("scope")
	scopeID := c.Query("scopeId")
	p := common.ParsePagination(c)
	items, total, err := h.svc.ListByScope(scope, scopeID, p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	dtos := make([]models.CredentialDTO, 0, len(items))
	for i := range items {
		dtos = append(dtos, items[i].ToDTO())
	}
	common.OKPaged(c, dtos, total, p)
}

func (h *CredentialHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	item, err := h.svc.Get(id)
	if err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, item.ToDTO())
}

func (h *CredentialHandler) Create(c *gin.Context) {
	var in models.CredentialInput
	if err := c.ShouldBindJSON(&in); err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	cred := &models.Credential{Name: in.Name, Type: in.Type, Scope: in.Scope, ScopeID: in.ScopeID, Value: in.Value}
	if err := h.svc.Create(cred); err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.Created(c, cred.ToDTO())
}

func (h *CredentialHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var in models.CredentialInput
	if err := c.ShouldBindJSON(&in); err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	cred := &models.Credential{Name: in.Name, Type: in.Type, Scope: in.Scope, ScopeID: in.ScopeID, Value: in.Value}
	if err := h.svc.Update(id, cred); err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, cred.ToDTO())
}

func (h *CredentialHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Delete(id); err != nil {
		common.AbortWithError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// KubeParseResult is the structural echo returned by parse-kubeconfig.
type KubeParseResult struct {
	Server           string   `json:"server"`
	CAPresent        bool     `json:"caPresent"`
	InsecureSkipTLS  bool     `json:"insecureSkipTLS"`
	AuthMethod       string   `json:"authMethod"` // token | client-cert | basic | none
	CurrentContext   string   `json:"currentContext"`
	DefaultNamespace string   `json:"defaultNamespace"`
	Errors           []string `json:"errors"`
}

var (
	reServer      = regexp.MustCompile(`(?m)^\s*server:\s*["']?([^\s"']+)["']?`)
	reCAData      = regexp.MustCompile(`certificate-authority-data:`)
	reInsecure    = regexp.MustCompile(`insecure-skip-tls-verify:\s*true`)
	reToken       = regexp.MustCompile(`(?m)^\s*token:`)
	reClientCert  = regexp.MustCompile(`client-certificate`)
	reUserPass    = regexp.MustCompile(`(?m)^\s*(username|password):`)
	reExec        = regexp.MustCompile(`(?m)^\s*exec:`)
	reCurrentCtx  = regexp.MustCompile(`(?m)^\s*current-context:\s*["']?([^\s"']+)["']?`)
	reNamespace   = regexp.MustCompile(`(?m)^\s*namespace:\s*["']?([^\s"']+)["']?`)
	reHasClusters = regexp.MustCompile(`^\s*clusters:`) // allow leading whitespace
)

func parseKubeconfig(raw string) KubeParseResult {
	res := KubeParseResult{Errors: []string{}}
	if !reHasClusters.MatchString(raw) {
		res.Errors = append(res.Errors, "不是合法 kubeconfig：缺少 clusters 段")
	}
	if m := reServer.FindStringSubmatch(raw); m != nil {
		res.Server = m[1]
	}
	res.CAPresent = reCAData.MatchString(raw)
	res.InsecureSkipTLS = reInsecure.MatchString(raw)
	if m := reCurrentCtx.FindStringSubmatch(raw); m != nil {
		res.CurrentContext = m[1]
	}
	if m := reNamespace.FindStringSubmatch(raw); m != nil {
		res.DefaultNamespace = m[1]
	}
	switch {
	case reToken.MatchString(raw):
		res.AuthMethod = "token"
	case reClientCert.MatchString(raw):
		res.AuthMethod = "client-cert"
	case reUserPass.MatchString(raw):
		res.AuthMethod = "basic"
	default:
		res.AuthMethod = "none"
	}
	if res.Server == "" {
		res.Errors = append(res.Errors, "无法定位 kube-apiserver 地址（clusters[].cluster.server 缺失）")
	}
	if reExec.MatchString(raw) {
		res.Errors = append(res.Errors, "平台不支持 exec 凭据（需本地执行二进制，等于在 hub 侧允许任意代码执行）；请改用静态 Token 或客户端证书")
	}
	if res.AuthMethod == "none" {
		res.Errors = append(res.Errors, "未找到可用认证材料（token / client-certificate-data / username+password 至少其一）")
	}
	return res
}

func (h *CredentialHandler) ParseKubeconfig(c *gin.Context) {
	var body struct {
		Raw string `json:"raw"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	common.OK(c, parseKubeconfig(body.Raw))
}
