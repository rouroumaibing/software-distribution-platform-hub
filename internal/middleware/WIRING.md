# 中间件接线示例

`Authenticator` / `UserContext` / `RequirePermission` 三个中间件在
`cmd/hub/main.go`(或未来拆出的 `internal/router`)里大致这样组合:

```go
func setupRouter(deps *Dependencies) *gin.Engine {
	r := gin.Default()
	api := r.Group("/api/v1")

	// 1. 每个请求先验证 Bearer token(签名/过期/issuer/azp),失败直接 401
	api.Use(deps.Authenticator.Middleware())

	// 2. 验证通过后,把 token 里的身份维度落进 context:
	//    subject(`sub`,§5.3)/ 显示名(仅供审计)/ groups / orgs。
	//    不落库 —— hub 不存用户表(§2.2 / D3),所以这里没有 UserService。
	api.Use(middleware.UserContext())

	// 3. 不需要按组件校验权限的路由(比如 Org/Target 管理)直接挂
	deps.OrgHandler.RegisterRoutes(api)
	deps.TargetHandler.RegisterRoutes(api)

	// 4. 组件类路由（pipeline/run 的 handler）：**组件类管理路由本身裸挂**在
	//    api 上（`pipelineHandler.RegisterRoutes(api)` 等），不带组件级守卫；
	//    只有组件作用域的运行类路由在**开鉴权时**挂两层守卫（§4 鉴权链是
	//    两段固定顺序，短路语义一致）：
	//    a 段 RequireResourceOwnership —— 资源归属（吃 token 的 `/org:<slug>`，查 resource_ownership）
	//    b 段 RequirePermission —— RBAC 决策（吃 hub 绑定表，按 token `sub`/组查绑定）
	//    Requirement 里的 Resource 说明**路径里那个 id 是什么资源**:
	//      ResourcePipeline / ResourceRun
	//                                  —— 参数是流水线 / 运行**自己的** id，必须先由
	//                                    permLocator 反查它所属的 component，再拿那个
	//                                    component 查归属/绑定。
	//    漏掉反查的后果不是"变松"而是"全拒"：拿 pipeline id 去查 component 绑定
	//    永远查不到，一旦开了鉴权这些路由**恒 403**。
	//    规范与对账见 ACCOUNT-PERMISSION-MODEL.md §10 第 14 行。
	permLocator := permissionsvc.NewComponentLocator(deps.PipelineRepo, deps.PipelineRunRepo)
	wrap := func(res middleware.Resource, param, perm string, h gin.HandlerFunc) []gin.HandlerFunc {
		if deps.Auth == nil {
			return []gin.HandlerFunc{h} // dev：鉴权关闭，裸挂
		}
		req := middleware.Requirement{Resource: res, Param: param, Permission: perm}
		return []gin.HandlerFunc{
			middleware.RequireResourceOwnership(deps.ResourceOwnershipSvc, deps.OrgRepo, permLocator, req),
			middleware.RequirePermission(deps.BindingService, permLocator, req),
			h,
		}
	}

	scoped := api.Group("/")
	// 注：实际接线没有 ResourceComponent 的用法 —— 组件 id 类路由
	// （如 GET /components/:id/pipelines，挂在 pipelineHandler 等裸注册里）
	// 不经过这里的守卫。
	//
	// :id 是 **pipeline id** —— 先反查所属 component，再依次查归属/绑定；
	// 生产环境部署另外在 service 层做一次强制审批校验
	scoped.POST(
		"/pipelines/:id/runs",
		wrap(middleware.ResourcePipeline, "id", permmodels.ActionPipelineTrigger, deps.PipelineRunHandler.Trigger)...,
	)

	// :id 是 **run id** —— 两跳：run → pipeline → component
	scoped.POST(
		"/runs/:id/redispatch",
		wrap(middleware.ResourceRun, "id", permmodels.ActionPipelineTrigger, deps.PipelineRunHandler.Redispatch)...,
	)

	return r
}
```

## 被拒时回什么码(便于排查,不要混用)

| 码 | 含义 |
| --- | --- |
| `401` | 上下文里没有已认证身份(校验失败,或中间件顺序错了) |
| `400` | 路径参数不是合法 UUID |
| `404` | 路径里的 pipeline / run **不存在**(反查不到) |
| `403` | 身份有效,但**确实没有**该组件上的这个权限 |
| `500` | 反查或绑定查询本身失败(库连不上等)——**故障不是拒绝**,不要报成 403 |

## Keycloak 侧需要提前配置好的东西

1. **建一个 Realm**,比如 `sdp`
2. **给 console 建一个 Public Client**(SPA 用 Authorization Code + PKCE,不需要 client secret),比如 client ID 填 `sdp-console`,Valid Redirect URIs 填 console 部署后的回调地址
3. **hub 这边不需要单独建 Client**——它只是被动验证 token,`AuthConfig.ClientID` 填的是上面 `sdp-console` 这个 client ID,因为 hub 要检查 token 的 `azp`(token 是为哪个 client 签发的)是不是这个值
4. 如果以后要做服务间调用(比如某个后台任务需要以系统身份调 Hub API),再单独建一个 **Confidential Client** 走 Client Credentials 模式,这个不在本次范围内
