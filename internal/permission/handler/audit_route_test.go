package handler

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// 审计读端点的**挂载纪律**：/audit-logs 只允许经 RegisterRoutes 挂在
// main.go 的 platformGroup 上（开鉴权后过 RequirePlatformPermission
// user:manage）。这条测试钉住两件事：
//
//  1. 路由存在且方法是 GET /api/v1/audit-logs —— 拼错一个字符，console
//     与 API-REFERENCE 就会和实现静默分叉，编译不报错；
//  2. handler 对 nil repo 安全（RegisterRoutes 只绑路由不调服务），
//     与 platform_routes_test.go 同一口径。
//
// 「不能挂到裸 api」的约束由 main.go 的分组结构保证（见 audit.go 注释）；
// 本测试若因路由被改名/删除而失败，先同步 hub/API-REFERENCE 与 console
// api/audit.ts 再改断言。
func TestAuditLogRouteIsRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewAuditLogHandler(nil).RegisterRoutes(r.Group("/api/v1"))

	got := map[string]bool{}
	for _, ri := range r.Routes() {
		got[ri.Method+" "+ri.Path] = true
	}
	if !got["GET /api/v1/audit-logs"] {
		t.Errorf("缺少路由 GET /api/v1/audit-logs（STATUS #19，console 审计 section 依赖该端点）")
	}
}
