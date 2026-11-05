package main

// BFF session layer wiring (AUTH-ARCHITECTURE.md). Kept in a dedicated file so
// cmd/hub/main.go stays a single call site — the session/auth/handler/middleware
// imports live here, not in main.go's import block.

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/auth"
	authhandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/auth/handler"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/keycloak"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/session"
)

// setupBFFAuth wires the BFF session layer:
//   - AutoMigrate the sessions table
//   - session.Repository
//   - AuthHandler with the five pre-auth endpoints on /api/v1/auth/*
//   - session-cookie verification path on the Authenticator (highest priority,
//     AUTH-ARCHITECTURE §8; missing/expired cookie falls through to bearer)
//
// No-op (returns nil) when auth is disabled (dev, no issuer) or the SA client
// is absent — the admin REST calls (GetUserByName / ResetPassword /
// ClearRequiredActions / LogoutAll) need the sdp-backend secret. The grant
// calls themselves take an explicit loginClientID, so the same SA-tuned Client
// serves both roles.
func setupBFFAuth(r *gin.Engine, gdb *gorm.DB, auth *middleware.Authenticator, kcAdmin *keycloak.Client, issuer, loginClientID, redirectURI string, loginMode auth.LoginModeService) *authhandler.AuthHandler {
	if auth == nil || kcAdmin == nil {
		return nil
	}
	if err := gdb.AutoMigrate(&session.Session{}); err != nil {
		applog.Fatalf("hub: session AutoMigrate: %v", err)
	}
	sessionRepo := session.NewRepository(gdb)
	secureCookie := strings.HasPrefix(issuer, "https://")
	authHandler := authhandler.NewAuthHandler(kcAdmin, sessionRepo, issuer, loginClientID, redirectURI, secureCookie, loginMode)

	// Session-cookie → claims. The Authenticator's Middleware calls this first;
	// on error it falls through to the bearer/PAT paths.
	auth.SetSessionVerifier(func(req *http.Request) (middleware.SessionIdentity, error) {
		sid := session.ReadCookie(req)
		if sid == "" {
			return middleware.SessionIdentity{}, errors.New("no session cookie")
		}
		s, serr := sessionRepo.Get(req.Context(), sid)
		if serr != nil {
			return middleware.SessionIdentity{}, serr
		}
		return middleware.SessionIdentity{
			Subject:  s.Subject,
			Username: s.Username,
			Groups:   s.GroupsList(),
		}, nil
	})

	// Pre-auth group: NO auth middleware (these endpoints are called before a
	// session exists). Mounted under /api/v1/auth to stay in the v1 namespace.
	authHandler.RegisterRoutes(r.Group("/api/v1/auth"))
	return authHandler
}
