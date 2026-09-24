package packageversion

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/config"
)

func TestGet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		PackageVersionConsole: "v1.2.3",
		PackageVersionHub:     "v1.2.4",
		PackageVersionRunner:  "v1.2.5",
	}
	h := NewHandler(cfg)

	r := gin.New()
	h.RegisterRoutes(r.Group("/api/v1"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/package-versions", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data Versions `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "v1.2.3", body.Data.Console)
	assert.Equal(t, "v1.2.4", body.Data.Hub)
	assert.Equal(t, "v1.2.5", body.Data.Runner)
}

func TestGetDevDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// "dev" is Load()'s fallback when the CM is not mounted (env unset).
	t.Setenv("PACKAGE_VERSION_CONSOLE", "")
	t.Setenv("PACKAGE_VERSION_HUB", "")
	t.Setenv("PACKAGE_VERSION_RUNNER", "")
	cfg := config.Load()
	h := NewHandler(cfg)

	r := gin.New()
	h.RegisterRoutes(r.Group("/api/v1"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/package-versions", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data Versions `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "dev", body.Data.Console)
	assert.Equal(t, "dev", body.Data.Hub)
	assert.Equal(t, "dev", body.Data.Runner)
}
