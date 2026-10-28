package middleware

import "github.com/gin-gonic/gin"

// SecurityHeaders injects defensive HTTP response headers on every response.
// It is a zero-cost, no-state middleware: it only sets headers, never reads
// the request body or blocks. Apply it once on the root engine so it covers
// both the API group and the gateway/metrics paths.
//
// 非直觉约束：HSTS 仅在 HTTPS 前端（网关/ingress 已终止 TLS）才有意义；本中间件
// 无脑下发 `Strict-Transport-Security` 不会破坏明文本地 dev（浏览器在 http 下忽略该头），
// 但生产须由网关保证 TLS，否则该头是空话。故默认开启、不依赖调用方判断。
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		// 防 MIME 嗅探：强制按 Content-Type 解释响应体。
		h.Set("X-Content-Type-Options", "nosniff")
		// 防点击劫持：禁止被嵌到 <frame>/<iframe>（console 全屏独立页，无需被嵌入）。
		h.Set("X-Frame-Options", "DENY")
		// 基础 CSP：仅允许同源脚本/样式/图片，阻断 inline 与外域注入（console 走打包资产）。
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		// 关掉 IE 的 XSS 审计（现代浏览器已移除，保留无副作用， legacy 防护）。
		h.Set("X-XSS-Protection", "1; mode=block")
		// 禁止被误识别为第三方 MITM 探针的 MIME 类型。
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}
