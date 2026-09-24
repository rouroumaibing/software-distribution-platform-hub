// Package packageversion surfaces the platform's deployed component version
// matrix (§9.10) via GET /package-versions.
//
// The three versions (console / hub / runner) are rendered by the hub chart
// from build/hub/versions.yaml into the package-versions ConfigMap and injected
// into the hub pod as PACKAGE_VERSION_*. The console reads this endpoint to
// show what is deployed; the runner install/upgrade flow (§9.9) reads it to
// pick a target version. Dev mode (no CM mounted) reports "dev".
package packageversion

import (
	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/config"
)

// Handler reads the version matrix from process config (sourced from the
// package-versions ConfigMap). No DB access — the matrix is a deploy-time
// constant, not mutable runtime state.
type Handler struct{ cfg *config.Config }

func NewHandler(cfg *config.Config) *Handler { return &Handler{cfg: cfg} }

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/package-versions", h.Get)
}

// Versions is the GET /package-versions payload.
type Versions struct {
	Console string `json:"console"`
	Hub     string `json:"hub"`
	Runner  string `json:"runner"`
}

// Get returns the deployed component versions.
func (h *Handler) Get(c *gin.Context) {
	common.OK(c, Versions{
		Console: h.cfg.PackageVersionConsole,
		Hub:     h.cfg.PackageVersionHub,
		Runner:  h.cfg.PackageVersionRunner,
	})
}
