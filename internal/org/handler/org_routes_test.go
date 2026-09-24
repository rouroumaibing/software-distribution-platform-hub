package handler

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// 组织删除是高危平台级动作（org/service Delete 的 TODO「平台级管理员权限校验」）。
// 这条断言防的是：以后有人图省事把 DELETE /orgs/:id 挪回 RegisterRoutes，于是它
// 静默落到裸 api 上，鉴权开启后**任何登录用户都能删掉一个组织**。
//
// 传 nil service 是安全的：Register*Routes 只把方法绑到路由上，不调用服务。
func TestOrgDeleteRouteIsSeparatedFromReads(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewOrgHandler(nil)

	// 两个独立引擎，才能分辨某个动作到底挂在哪个组上（Engine 才有 Routes()）。
	readEngine := gin.New()
	h.RegisterRoutes(readEngine.Group("/api/v1"))

	adminEngine := gin.New()
	h.RegisterAdminRoutes(adminEngine.Group("/api/v1"))

	paths := func(e *gin.Engine) map[string]bool {
		out := map[string]bool{}
		for _, ri := range e.Routes() {
			out[ri.Method+" "+ri.Path] = true
		}
		return out
	}
	read := paths(readEngine)
	admin := paths(adminEngine)

	for _, w := range []string{
		"GET /api/v1/orgs",
		"POST /api/v1/orgs",
		"GET /api/v1/orgs/:id",
		"PUT /api/v1/orgs/:id",
		"GET /api/v1/orgs/:id/service-tree",
	} {
		if !read[w] {
			t.Errorf("缺少读/建/改路由 %s", w)
		}
	}

	const del = "DELETE /api/v1/orgs/:id"
	if !admin[del] {
		t.Errorf("缺少平台级删除路由 %s（应挂在 RegisterAdminRoutes 的组上）", del)
	}
	if read[del] {
		t.Errorf("删除路由 %s 被注册到裸 api 读组 —— 鉴权开启后任何登录用户都能删组织", del)
	}
}
