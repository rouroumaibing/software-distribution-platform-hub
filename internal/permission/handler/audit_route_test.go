package handler

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// 审计读端点的**挂载纪律**（2026-10-11 组件级审计下沉后更新）：/audit-logs
// 挂在 main.go 经 RequireAuditAccess 的分组上 —— 平台管理员（user:manage）
// 全量可查；组件管理员（component:manage）须携带 ?componentId= 且 handler
// 强制钳到该组件。这条测试钉住两件事：
//
//  1. 路由存在且方法是 GET /api/v1/audit-logs —— 拼错一个字符，console
//     与 API-REFERENCE 就会和实现静默分叉，编译不报错；
//  2. handler 对 nil repo 安全（RegisterRoutes 只绑路由不调服务），
//     与 platform_routes_test.go 同一口径。
//
// 「双通道鉴权」的中间件语义由 middleware 包的 RequireAuditAccess 测试钉住；
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
