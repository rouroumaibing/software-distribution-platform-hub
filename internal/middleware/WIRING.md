# 中间件接线示例

`Authenticator` / `UserContext` / `RequirePermission` 三个中间件在
`cmd/hub/main.go`(或未来拆出的 `internal/router`)里大致这样组合:

```go
func setupRouter(deps *Dependencies) *gin.Engine {
	r := gin.Default()
	api := r.Group("/api/v1")

	// 1. 每个请求先验证 Bearer token(签名/过期/issuer/azp),失败直接 401
	api.Use(deps.Authenticator.Middleware())

	// 2. 验证通过后,解析出对应的本地 User(首次登录自动创建)
	api.Use(middleware.UserContext(deps.UserService))

	// 3. 不需要按组件校验权限的路由(比如 Org/Cluster 管理)直接挂
	deps.OrgHandler.RegisterRoutes(api)
	deps.ClusterHandler.RegisterRoutes(api)

	// 4. 需要按组件校验权限的路由,单独在路由组上加 RequirePermission
	componentScoped := api.Group("/")
	{
		// 查看流水线只需要 view 权限
		componentScoped.GET(
			"/components/:componentId/pipelines",
			middleware.RequirePermission(deps.BindingService, "componentId", models.PermissionView),
			deps.PipelineHandler.ListByComponent,
		)

		// 触发发布需要 edit 权限,生产环境部署在 service 层再做一次强制审批校验
		componentScoped.POST(
			"/pipelines/:pipelineId/runs",
			middleware.RequirePermission(deps.BindingService, "componentId", models.PermissionEdit),
			deps.PipelineRunHandler.Trigger,
		)

		// 删除组件需要 delete 权限
		componentScoped.DELETE(
			"/components/:id",
			middleware.RequirePermission(deps.BindingService, "id", models.PermissionDelete),
			deps.ComponentHandler.Delete,
		)
	}

	return r
}
```

## Keycloak 侧需要提前配置好的东西

1. **建一个 Realm**,比如 `sdp`
2. **给 console 建一个 Public Client**(SPA 用 Authorization Code + PKCE,不需要 client secret),比如 client ID 填 `sdp-console`,Valid Redirect URIs 填 console 部署后的回调地址
3. **hub 这边不需要单独建 Client**——它只是被动验证 token,`AuthConfig.ClientID` 填的是上面 `sdp-console` 这个 client ID,因为 hub 要检查 token 的 `azp`(token 是为哪个 client 签发的)是不是这个值
4. 如果以后要做服务间调用(比如某个后台任务需要以系统身份调 Hub API),再单独建一个 **Confidential Client** 走 Client Credentials 模式,这个不在本次范围内
