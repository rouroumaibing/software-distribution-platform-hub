package handler

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/keycloak"
)

// KeycloakDirectoryHandler is the management-plane proxy in front of the
// Keycloak Admin REST API (D3 re-estimation, ui-05 §7.14 M1 / ui-09 N-34):
// the console gets a user/group directory for subject pickers and account
// administration without ever holding admin credentials itself. Mounted on
// the platform group (user:manage). The org carrier groups (/org:<slug>) are
// orgsvc-owned and read-only through this proxy — enforced here, not just in
// the UI.
//
// The interface mirrors the client's capability set so tests can stub it.
type KeycloakDirectory interface {
	ListUsers(ctx context.Context, search string, first, max int) ([]keycloak.User, error)
	CreateUser(ctx context.Context, in keycloak.NewUser) (string, error)
	UpdateUser(ctx context.Context, id string, in keycloak.UserPatch) error
	ResetPassword(ctx context.Context, id, password string, temporary bool) error
	ListGroups(ctx context.Context, search string) ([]keycloak.Group, error)
	CreateGroup(ctx context.Context, name string) (string, error)
	GetGroup(ctx context.Context, id string) (keycloak.Group, error)
	ListGroupMembers(ctx context.Context, groupID string, first, max int) ([]keycloak.User, error)
	AddGroupMember(ctx context.Context, groupID, userID string) error
	RemoveGroupMember(ctx context.Context, groupID, userID string) error
}

type KeycloakDirectoryHandler struct {
	kc KeycloakDirectory
}

func NewKeycloakDirectoryHandler(kc KeycloakDirectory) *KeycloakDirectoryHandler {
	return &KeycloakDirectoryHandler{kc: kc}
}

func (h *KeycloakDirectoryHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/keycloak/users", h.ListUsers)
	rg.POST("/keycloak/users", h.CreateUser)
	rg.PUT("/keycloak/users/:id", h.UpdateUser)
	rg.PUT("/keycloak/users/:id/password", h.ResetUserPassword)
	rg.GET("/keycloak/groups", h.ListGroups)
	rg.POST("/keycloak/groups", h.CreateGroup)
	rg.GET("/keycloak/groups/:id", h.GetGroup)
	rg.GET("/keycloak/groups/:id/members", h.ListGroupMembers)
	rg.PUT("/keycloak/groups/:id/members/:userId", h.AddGroupMember)
	rg.DELETE("/keycloak/groups/:id/members/:userId", h.RemoveGroupMember)
}

// failDirectory maps the client's sentinels onto the permission error family;
// anything else falls through to the shared mapper (500 for unknown errors).
func failDirectory(c *gin.Context, err error) {
	switch {
	case errors.Is(err, keycloak.ErrNotFound):
		common.Fail(c, http.StatusNotFound,
			common.DomainError(common.KindPermission, http.StatusNotFound, 30, "Directory Entity Not Found"))
	case errors.Is(err, keycloak.ErrConflict):
		common.Fail(c, http.StatusConflict,
			common.DomainError(common.KindPermission, http.StatusConflict, 31, "Directory Entity Already Exists"))
	default:
		failPermission(c, err)
	}
}

type createUserReq struct {
	Username        string `json:"username"`
	Email           string `json:"email"`
	InitialPassword string `json:"initialPassword"`
}

type createUserResp struct {
	ID                string   `json:"id"`
	Username          string   `json:"username"`
	Email             string   `json:"email,omitempty"`
	TemporaryPassword string   `json:"temporaryPassword,omitempty"`
	RequiredActions   []string `json:"requiredActions,omitempty"`
}

func (h *KeycloakDirectoryHandler) ListUsers(c *gin.Context) {
	first, _ := strconv.Atoi(c.DefaultQuery("first", "0"))
	max, _ := strconv.Atoi(c.DefaultQuery("max", "100"))
	users, err := h.kc.ListUsers(c.Request.Context(), c.Query("search"), first, max)
	if err != nil {
		failDirectory(c, err)
		return
	}
	common.OK(c, users)
}

// CreateUser provisions the account in Keycloak and returns the temporary
// password exactly once — the password lives only in Keycloak (hub keeps no
// user table, §2.2), so the UI must surface it for one-time handoff.
func (h *KeycloakDirectoryHandler) CreateUser(c *gin.Context) {
	var in createUserReq
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(in.Username) == "" {
		common.Fail(c, http.StatusBadRequest, common.ErrBadRequest.WithMessage("username is required"))
		return
	}
	tempPassword := in.InitialPassword
	if tempPassword == "" {
		tempPassword = GenerateTemporaryPassword()
	}
	if len(tempPassword) < 12 {
		common.Fail(c, http.StatusBadRequest, common.ErrBadRequest.WithMessage("initialPassword must be at least 12 characters"))
		return
	}
	id, err := h.kc.CreateUser(c.Request.Context(), keycloak.NewUser{
		Username:        strings.TrimSpace(in.Username),
		Email:           strings.TrimSpace(in.Email),
		Enabled:         true,
		RequiredActions: []string{"UPDATE_PASSWORD"},
		InitialPassword: tempPassword,
	})
	if err != nil {
		failDirectory(c, err)
		return
	}
	common.Created(c, createUserResp{
		ID:                id,
		Username:          strings.TrimSpace(in.Username),
		Email:             strings.TrimSpace(in.Email),
		TemporaryPassword: tempPassword,
		RequiredActions:   []string{"UPDATE_PASSWORD"},
	})
}

type updateUserReq struct {
	Enabled *bool   `json:"enabled"`
	Email   *string `json:"email"`
}

func (h *KeycloakDirectoryHandler) UpdateUser(c *gin.Context) {
	var in updateUserReq
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if in.Enabled == nil && in.Email == nil {
		common.Fail(c, http.StatusBadRequest, common.ErrBadRequest.WithMessage("nothing to update: provide enabled or email"))
		return
	}
	if err := h.kc.UpdateUser(c.Request.Context(), c.Param("id"), keycloak.UserPatch{Enabled: in.Enabled, Email: in.Email}); err != nil {
		failDirectory(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type resetPasswordReq struct {
	Password string `json:"password"`
}

type resetPasswordResp struct {
	Temporary bool   `json:"temporary"`
	Password  string `json:"password"`
}

func (h *KeycloakDirectoryHandler) ResetUserPassword(c *gin.Context) {
	var in resetPasswordReq
	_ = c.ShouldBindJSON(&in) // body optional
	password := in.Password
	if password == "" {
		password = GenerateTemporaryPassword()
	}
	if len(password) < 12 {
		common.Fail(c, http.StatusBadRequest, common.ErrBadRequest.WithMessage("password must be at least 12 characters"))
		return
	}
	if err := h.kc.ResetPassword(c.Request.Context(), c.Param("id"), password, true); err != nil {
		failDirectory(c, err)
		return
	}
	common.OK(c, resetPasswordResp{Temporary: true, Password: password})
}

func (h *KeycloakDirectoryHandler) ListGroups(c *gin.Context) {
	groups, err := h.kc.ListGroups(c.Request.Context(), c.Query("search"))
	if err != nil {
		failDirectory(c, err)
		return
	}
	common.OK(c, groups)
}

func (h *KeycloakDirectoryHandler) CreateGroup(c *gin.Context) {
	var in struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	name := strings.TrimSpace(in.Name)
	if err := validateRBACGroupName(name); err != nil {
		common.Fail(c, http.StatusForbidden, err)
		return
	}
	id, err := h.kc.CreateGroup(c.Request.Context(), name)
	if err != nil {
		failDirectory(c, err)
		return
	}
	common.Created(c, keycloak.Group{ID: id, Name: name, Path: "/" + name})
}

// validateRBACGroupName enforces the proxy's write boundary: org carrier
// groups (/org:<slug>) are provisioned by orgsvc and must never be created or
// renamed through the directory; nested groups are out of scope for the
// console and path separators are rejected for the same reason.
func validateRBACGroupName(name string) error {
	switch {
	case name == "":
		return common.DomainError(common.KindPermission, http.StatusBadRequest, 32, "group name is required")
	case strings.Contains(name, "/"):
		return common.DomainError(common.KindPermission, http.StatusForbidden, 33, "nested groups are not supported; name must not contain /")
	case name == "org" || strings.HasPrefix(name, "org:"):
		return common.DomainError(common.KindPermission, http.StatusForbidden, 34, "org carrier groups (org:*) are managed by the platform")
	}
	return nil
}

func (h *KeycloakDirectoryHandler) GetGroup(c *gin.Context) {
	g, err := h.kc.GetGroup(c.Request.Context(), c.Param("id"))
	if err != nil {
		failDirectory(c, err)
		return
	}
	common.OK(c, g)
}

func (h *KeycloakDirectoryHandler) ListGroupMembers(c *gin.Context) {
	first, _ := strconv.Atoi(c.DefaultQuery("first", "0"))
	max, _ := strconv.Atoi(c.DefaultQuery("max", "200"))
	users, err := h.kc.ListGroupMembers(c.Request.Context(), c.Param("id"), first, max)
	if err != nil {
		failDirectory(c, err)
		return
	}
	common.OK(c, users)
}

// assertMutableGroup resolves the group and rejects org carrier groups with
// 403 before any membership mutation reaches Keycloak.
func (h *KeycloakDirectoryHandler) assertMutableGroup(c *gin.Context) bool {
	g, err := h.kc.GetGroup(c.Request.Context(), c.Param("id"))
	if err != nil {
		failDirectory(c, err)
		return false
	}
	if strings.HasPrefix(g.Path, "/org:") {
		common.Fail(c, http.StatusForbidden, common.DomainError(common.KindPermission, http.StatusForbidden, 35,
			"Org carrier groups are managed by the platform and read-only through the directory"))
		return false
	}
	return true
}

func (h *KeycloakDirectoryHandler) AddGroupMember(c *gin.Context) {
	if !h.assertMutableGroup(c) {
		return
	}
	if err := h.kc.AddGroupMember(c.Request.Context(), c.Param("id"), c.Param("userId")); err != nil {
		failDirectory(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *KeycloakDirectoryHandler) RemoveGroupMember(c *gin.Context) {
	if !h.assertMutableGroup(c) {
		return
	}
	if err := h.kc.RemoveGroupMember(c.Request.Context(), c.Param("id"), c.Param("userId")); err != nil {
		failDirectory(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// tempPasswordAlphabet excludes visually ambiguous characters (0/O, 1/l/I)
// so a one-time handoff password survives transcription.
const tempPasswordAlphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// GenerateTemporaryPassword produces a 16-character password containing at
// least one lowercase, one uppercase and one digit character. It panics if
// the system CSPRNG fails: a broken entropy source invalidates the whole
// temporary-credential flow, and a degraded fallback would be worse.
func GenerateTemporaryPassword() string {
	n := big.NewInt(int64(len(tempPasswordAlphabet)))
	draw := func() int {
		i, err := rand.Int(rand.Reader, n)
		if err != nil {
			panic("keycloak directory: crypto/rand failed: " + err.Error())
		}
		return int(i.Int64())
	}
	pw := make([]byte, 16)
	// Guaranteed classes first (digit / lowercase / uppercase), then random
	// tail, then a Fisher-Yates pass so the fixed positions do not leak.
	// Alphabet layout: [0..23] lowercase, [24..47] uppercase, [48..55] digits.
	pw[0] = tempPasswordAlphabet[48] // '2' — digit
	pw[1] = tempPasswordAlphabet[0]  // 'a' — lowercase
	pw[2] = tempPasswordAlphabet[24] // 'A' — uppercase
	for i := 3; i < len(pw); i++ {
		pw[i] = tempPasswordAlphabet[draw()]
	}
	for i := len(pw) - 1; i > 0; i-- {
		j := draw() % (i + 1)
		pw[i], pw[j] = pw[j], pw[i]
	}
	return string(pw)
}

// PasswordClassSatisfied reports whether pw mixes lowercase, uppercase and
// digit characters — the contract GenerateTemporaryPassword must uphold.
func PasswordClassSatisfied(pw string) bool {
	var hasLower, hasUpper, hasDigit bool
	for i := 0; i < len(pw); i++ {
		switch {
		case pw[i] >= 'a' && pw[i] <= 'z':
			hasLower = true
		case pw[i] >= 'A' && pw[i] <= 'Z':
			hasUpper = true
		case pw[i] >= '0' && pw[i] <= '9':
			hasDigit = true
		}
	}
	return hasLower && hasUpper && hasDigit
}
