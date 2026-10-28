package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// CORS applies a cross-origin policy to the API group. The platform's console
// and hub are served behind the same gateway (same-origin), so by default — with
// an empty allowlist — this middleware adds NO CORS headers and lets the browser
// enforce same-origin. Only when explicit origins are configured (a real
// cross-origin deployment) does it emit Access-Control-* headers.
//
// 非直觉约束：跨域只在「真有非同源前端」时才开。盲目回显 `Origin` 或 `*`
// 会击穿同源防护；故 allowlist 为空即不处理（不回显、不开通），避免配错阻断
// 前端、也避免误开通任意域。预检请求按标准处理（OPTIONS 短路）。
func CORS(allowedOrigins []string) gin.HandlerFunc {
	normalized := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		o = strings.TrimSpace(o)
		if o != "" {
			normalized[o] = struct{}{}
		}
	}
	if len(normalized) == 0 {
		// 无白名单：不搞 CORS，原样放行（同源场景）。
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if _, ok := normalized[origin]; ok {
				h := c.Writer.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Vary", "Origin")
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With")
				h.Set("Access-Control-Max-Age", "600")
			}
		}
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
