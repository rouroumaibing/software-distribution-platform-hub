// Package middleware conformance gate.
//
// These tests are the continuous gate requested by
// shared/ACCOUNT-PERMISSION-MODEL.md §10 ("对账脚本化后应作为门禁") and
// §11 step 5 ("防回退静态断言"). They protect the three immutables of the
// account/permission model without needing a running Keycloak or DB:
//
//  1. hub never reads realm/client roles (realm_access / resource_access)
//     to make authorization decisions (不动式②: hub is the sole authority);
//  2. hub is a Resource Server: AuthConfig carries no credential, and token
//     validity is checked via azp (not aud) — SkipClientIDCheck: true;
//  3. the auth path never calls Keycloak token introspection / userinfo at
//     runtime (it only verifies the signature locally via go-oidc/JWKS);
//  4. the two enforcement middleware entry points exist and are wired
//     (resource-ownership + RBAC), so a refactor cannot silently drop them.
//
// The test parses auth.go from the package directory (tests run with cwd
// == package dir) and applies both AST and substring assertions.
package middleware

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func loadAuthSrc(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("auth.go")
	if err != nil {
		t.Fatalf("conformance: read auth.go: %v", err)
	}
	return string(src)
}

// TestConformance_AuthConfigNoSecret — §10 #16/#17: hub is a Resource Server.
// It must not hold a client secret or any other credential: no token endpoint
// call, no userinfo/introspection, so nothing to protect.
func TestConformance_AuthConfigNoSecret(t *testing.T) {
	src := loadAuthSrc(t)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "auth.go", src, 0)
	if err != nil {
		t.Fatalf("conformance: parse auth.go: %v", err)
	}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "AuthConfig" {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, field := range st.Fields.List {
				for _, name := range field.Names {
					switch strings.ToLower(name.Name) {
					case "secret", "clientsecret", "password", "apikey", "apisecret", "accesstoken":
						t.Errorf("AuthConfig must not carry a credential field (§10 #16/#17 Resource Server invariant); found %q", name.Name)
					}
				}
			}
		}
	}
}

// TestConformance_SkipClientIDCheck — §10 #18: verify azp, skip aud.
func TestConformance_SkipClientIDCheck(t *testing.T) {
	src := loadAuthSrc(t)
	if !strings.Contains(src, "SkipClientIDCheck: true") {
		t.Error("auth must verify azp not aud (§10 #18): SkipClientIDCheck: true missing")
	}
	if !strings.Contains(src, "errWrongClient") {
		t.Error("azp mismatch must be rejected with errWrongClient (§10 #18)")
	}
}

// TestConformance_NoRealmRoleConsumption — §10 #1/#3: hub never consumes
// realm_access / resource_access for authorization. The keycloakClaims struct
// (what the verified token is decoded into) must NOT carry those tags. We
// inspect the AST struct tags rather than substring-matching, because the
// source comment legitimately *mentions* them while stating they are not read.
func TestConformance_NoRealmRoleConsumption(t *testing.T) {
	src := loadAuthSrc(t)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "auth.go", src, 0)
	if err != nil {
		t.Fatalf("conformance: parse auth.go: %v", err)
	}
	banned := map[string]bool{"realm_access": true, "resource_access": true}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, field := range st.Fields.List {
				if field.Tag == nil {
					continue
				}
				tag := strings.Trim(field.Tag.Value, "`")
				if strings.HasPrefix(tag, "json:") {
					// tag form: json:"name" or json:"name,..." — extract the name.
					inner := strings.TrimPrefix(tag, "json:")
					inner = strings.Trim(inner, "\"")
					name := inner
					if idx := strings.Index(name, ","); idx >= 0 {
						name = name[:idx]
					}
					if banned[name] {
						t.Errorf("verified-claims struct %q must not decode %q (§10 #1/#3); found json tag %q", ts.Name.Name, name, tag)
					}
				}
			}
		}
	}
}

// TestConformance_NoRuntimeTokenIntrospection — §10 #17/#19: the auth path
// must not call Keycloak token introspection or userinfo at runtime. Signature
// verification is local (go-oidc + JWKS).
func TestConformance_NoRuntimeTokenIntrospection(t *testing.T) {
	src := loadAuthSrc(t)
	for _, bad := range []string{"introspect", "introspection", "TokenIntrospection", "/userinfo", "UserInfoService", "userInfo"} {
		if strings.Contains(src, bad) {
			t.Errorf("auth path must not call Keycloak runtime token endpoint %q (§10 #17/#19); reference found in auth.go", bad)
		}
	}
}

// TestConformance_EnforcementPointsWired — §10 #7/#14: the two required
// middleware entry points must exist. Compile-time references fail the build
// if a refactor removes or renames them, which is exactly the regression we
// want to catch.
func TestConformance_EnforcementPointsWired(t *testing.T) {
	var _ = RequireResourceOwnership
	var _ = RequirePermission
	var _ = RequirePlatformPermission
}
