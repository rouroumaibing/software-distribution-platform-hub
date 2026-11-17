package coordination

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// AdminHandler exposes the authenticated drain endpoint (§4.6). It mounts on
// the platform group (user:manage) next to the other high-privilege platform
// routes; SIGTERM reuses the same Drain routine without going through HTTP.
type AdminHandler struct {
	coord *Coordinator
}

// NewAdminHandler builds the drain handler on a coordinator (nil means
// coordination is disabled — the endpoint answers 501).
func NewAdminHandler(coord *Coordinator) *AdminHandler { return &AdminHandler{coord: coord} }

// RegisterRoutes mounts POST /admin/coordination/drain on the given group
// (the platform group contributes the /api/v1 prefix and the user:manage
// guard).
func (h *AdminHandler) RegisterRoutes(rg gin.IRouter) {
	rg.POST("/admin/coordination/drain", h.drain)
}

func (h *AdminHandler) drain(c *gin.Context) {
	if h.coord == nil {
		common.Fail(c, http.StatusNotImplemented, errCoordinationDisabled)
		return
	}
	if err := h.coord.Drain(c.Request.Context()); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{"status": "draining"})
}

var errCoordinationDisabled = errors.New("coordination is disabled on this instance")
