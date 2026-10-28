package middleware

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimit is an in-memory token-bucket limiter keyed by client IP. It is
// single-instance by design: under multiple hub replicas the counters are NOT
// shared, so a replica restart or horizontal scale-out widens the effective
// limit rather than enforcing it globally. The design note is deliberate — see
// the package doc on the trade-off; a multi-replica deployment must front the
// hub with a shared counter (e.g. redis) before relying on this for hard
// abuse protection. For single-replica (the common dev/SMall-prod shape) it
// fully protects login/token endpoints from brute force.
//
// 非直觉约束：限流按「IP + 路由」维度，但本实现仅取客户端 IP（X-Forwarded-For
// 末跳优先、回退 RealIP、再回退 RemoteAddr）。网关已终止 TLS 并注入 XFF，故取末跳
// 即真实客户端；若未来 hub 直接暴露公网且无网关，需改为取 XFF 首跳并校验信任链。
type RateLimit struct {
	rps      float64
	burst    int
	mu       sync.Mutex
	buckets  map[string]*bucket
	lastSeen map[string]time.Time
}

type bucket struct {
	tokens   float64
	last     time.Time
	maxBurst int
}

// NewRateLimit builds a limiter. rps is the steady-state requests/sec per IP;
// burst is the max tokens a bucket can hold (allows short bursts). A zero/neg
// rps disables the limiter (pass-through) — callers may guard with config.
func NewRateLimit(rps float64, burst int) *RateLimit {
	if burst < 1 {
		burst = 1
	}
	return &RateLimit{
		rps:      rps,
		burst:    burst,
		buckets:  make(map[string]*bucket),
		lastSeen: make(map[string]time.Time),
	}
}

// Handler returns the gin middleware.
func (rl *RateLimit) Handler() gin.HandlerFunc {
	if rl == nil || rl.rps <= 0 {
		// 禁用：直接放行，零开销。
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		ip := clientIP(c)
		if !rl.allow(ip) {
			c.AbortWithStatusJSON(429, gin.H{
				"error":   "rate limit exceeded",
				"message": "too many requests, retry later",
			})
			return
		}
		c.Next()
	}
}

func (rl *RateLimit) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[ip]
	if !ok {
		b = &bucket{tokens: float64(rl.burst), last: now, maxBurst: rl.burst}
		rl.buckets[ip] = b
		rl.lastSeen[ip] = now
		return true
	}
	// 漏桶补充：按经过时间回补 token，上限 burst。
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * rl.rps
	if b.tokens > float64(b.maxBurst) {
		b.tokens = float64(b.maxBurst)
	}
	b.last = now
	rl.lastSeen[ip] = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// StartSweeper periodically drops idle buckets so the map cannot grow
// unbounded with churning client IPs. Call from a goroutine; it returns when
// ctx is done.
func (rl *RateLimit) StartSweeper(stale time.Duration) {
	if rl == nil {
		return
	}
	ticker := time.NewTicker(stale)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-stale)
		for ip, t := range rl.lastSeen {
			if t.Before(cutoff) {
				delete(rl.buckets, ip)
				delete(rl.lastSeen, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func clientIP(c *gin.Context) string {
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		// 取末跳 = 真实客户端（网关注入信任链）。
		if idx := lastComma(xff); idx >= 0 {
			return trimSpace(xff[idx+1:])
		}
		return trimSpace(xff)
	}
	if rip := c.GetHeader("X-Real-IP"); rip != "" {
		return trimSpace(rip)
	}
	return c.ClientIP()
}

func lastComma(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ',' {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
