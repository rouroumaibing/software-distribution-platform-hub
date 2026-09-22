package handler

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// 文档把端点名写死了（README §5.2、hub/API-REFERENCE.md、STORY-BACKLOG C-10：
// `/platform-roles`、`/platform-role-bindings`），所以这里逐条钉住路径与方法 ——
// 拼错一个字符，console 与文档就会和实现静默分叉，而编译完全不会报错。
//
// 传 nil service 是安全的：RegisterRoutes 只把方法绑到路由上，不调用服务。
func TestPlatformRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api/v1")

	NewPlatformRoleHandler(nil).RegisterRoutes(api)
	NewPlatformRoleBindingHandler(nil).RegisterRoutes(api)

	got := map[string]bool{}
	for _, ri := range r.Routes() {
		got[ri.Method+" "+ri.Path] = true
	}

	want := []string{
		"GET /api/v1/platform-roles",
		"POST /api/v1/platform-roles",
		"GET /api/v1/platform-roles/:id",
		"PUT /api/v1/platform-roles/:id",
		"DELETE /api/v1/platform-roles/:id",
		"GET /api/v1/platform-role-bindings",
		"POST /api/v1/platform-role-bindings",
		"DELETE /api/v1/platform-role-bindings/:id",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("缺少路由 %s（文档已把该端点写入 API-REFERENCE / STORY-BACKLOG C-10）", w)
		}
	}

	// 反向断言：绑定资源**不提供** Update —— 改授另一个角色是「删了再建」，
	// 这样 (org, subject, role) 的某一时刻只有一行，语义不会二义。
	if got["PUT /api/v1/platform-role-bindings/:id"] {
		t.Errorf("平台级绑定不应暴露 PUT（改授 = 删后重建）")
	}
}

// B-11 自定义角色：组件级角色的**读**与**写**必须挂在不同的中间件组上。
//
// 读（List / Get）留在裸 api：console 的 PermissionsTab 用它填角色选择器，
// 任何有组件级权限的用户都要能看到 —— 挂上平台级 user:manage 会直接打断该页。
//
// 写（Create / Update / Delete）必须走注册 `RegisterAdminRoutes` 的那个组
// （main.go 里即 platformGroup）。角色是权限的来源，**能改角色 = 能给自己加权限**，
// 只靠"已登录"兜不住。这条断言防的是：以后有人图省事把三个写方法挪回
// RegisterRoutes，于是它们静默落到裸 api 上，鉴权开启后任何登录用户都能提权。
func TestComponentRoleReadAndWriteRoutesAreSeparated(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewComponentRoleHandler(nil)

	// 两个独立引擎，才能分辨某个动作到底挂在哪个组上（Engine 才有 Routes()）。
	readEngine := gin.New()
	h.RegisterRoutes(readEngine.Group("/api/v1"))

	writeEngine := gin.New()
	h.RegisterAdminRoutes(writeEngine.Group("/api/v1"))

	paths := func(e *gin.Engine) map[string]bool {
		out := map[string]bool{}
		for _, ri := range e.Routes() {
			out[ri.Method+" "+ri.Path] = true
		}
		return out
	}
	read := paths(readEngine)
	write := paths(writeEngine)

	for _, w := range []string{
		"GET /api/v1/component-roles",
		"GET /api/v1/component-roles/:id",
	} {
		if !read[w] {
			t.Errorf("缺少读路由 %s", w)
		}
	}
	for _, w := range []string{
		"POST /api/v1/component-roles",
		"PUT /api/v1/component-roles/:id",
		"DELETE /api/v1/component-roles/:id",
	} {
		if !write[w] {
			t.Errorf("缺少写路由 %s", w)
		}
		// 关键：写端点绝不能出现在读组里（那意味着它在裸 api 上可用）。
		if read[w] {
			t.Errorf("写路由 %s 被注册到读组 —— 鉴权开启后任何登录用户都能改角色（提权）", w)
		}
	}
}
