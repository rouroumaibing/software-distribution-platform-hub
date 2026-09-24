// Package metrics gives the Hub a Prometheus scraping surface (B-09).
//
// The Runner already registers prometheus/client_golang collectors and serves
// /metrics through controller-runtime's built-in metrics server. The Hub had no
// metrics endpoint at all, so this package closes that gap. It is a tiny,
// dependency-free Prometheus text (0.0.4) exporter so we don't have to add a
// fresh module requirement to the Hub just for request counters — the same
// scrape job can pick up both Hub and Runner.
//
// The exposition format follows
// https://prometheus.io/docs/instrumenting/exposition_formats/ so any Prometheus
// server (or the OTel collector's Prometheus receiver) can ingest it unchanged.
package metrics

import (
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

var startTime = time.Now()

// build info, overridden from main via SetBuildInfo.
var (
	buildVersion   = "unknown"
	buildCommit    = "unknown"
	buildGoVersion = runtime.Version()
)

// SetBuildInfo records the build identity surfaced by sdp_hub_build_info.
// Any empty field keeps its default. main passes the ldflags-injected values
// (version/commit) when available, falling back to "dev"/"unknown".
func SetBuildInfo(version, commit, goVersion string) {
	if version != "" {
		buildVersion = version
	}
	if commit != "" {
		buildCommit = commit
	}
	if goVersion != "" {
		buildGoVersion = goVersion
	}
}

type counter struct {
	label string
	value atomic.Uint64
}

var (
	mu       sync.Mutex
	counters = map[string]*counter{}

	reqDurationSum   atomic.Uint64 // nanoseconds
	reqDurationCount atomic.Uint64
	inFlight         atomic.Int64
)

func getCounter(label string) *counter {
	mu.Lock()
	defer mu.Unlock()
	c, ok := counters[label]
	if !ok {
		c = &counter{label: label}
		counters[label] = c
	}
	return c
}

// StatusClass buckets an HTTP status code into a coarse class label.
func StatusClass(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500:
		return "5xx"
	default:
		return "other"
	}
}

// RecordRequest increments the per-(method, status class) request counter and
// the latency totals.
func RecordRequest(method, statusClass string, duration time.Duration) {
	getCounter(`method="` + method + `",status_class="` + statusClass + `"`).value.Add(1)
	reqDurationSum.Add(uint64(duration.Nanoseconds()))
	reqDurationCount.Add(1)
}

// IncInFlight / DecInFlight track concurrent requests as a gauge.
func IncInFlight() { inFlight.Add(1) }
func DecInFlight() { inFlight.Add(-1) }

// GinMiddleware records per-request metrics for the Hub's Gin engine.
func GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		IncInFlight()
		start := time.Now()
		c.Next()
		DecInFlight()
		RecordRequest(c.Request.Method, StatusClass(c.Writer.Status()), time.Since(start))
	}
}

// GinHandler serves the Prometheus text exposition at /metrics.
func GinHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		c.String(http.StatusOK, Render())
	}
}

// Render produces the Prometheus text exposition. It is safe for concurrent
// use; counters are read atomically and the label map is guarded by mu.
func Render() string {
	mu.Lock()
	labels := make([]string, 0, len(counters))
	for l := range counters {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	mu.Unlock()

	var b strings.Builder
	b.WriteString("# HELP sdp_hub_http_requests_total Total HTTP requests by method and status class.\n")
	b.WriteString("# TYPE sdp_hub_http_requests_total counter\n")
	for _, l := range labels {
		b.WriteString("sdp_hub_http_requests_total{" + l + "} " + strconv.FormatUint(counters[l].value.Load(), 10) + "\n")
	}

	b.WriteString("# HELP sdp_hub_http_request_duration_seconds_total Total request latency in seconds.\n")
	b.WriteString("# TYPE sdp_hub_http_request_duration_seconds_total counter\n")
	b.WriteString("sdp_hub_http_request_duration_seconds_total " +
		strconv.FormatFloat(float64(reqDurationSum.Load())/1e9, 'f', 6, 64) + "\n")
	b.WriteString("# HELP sdp_hub_http_request_duration_seconds_count Number of requests timed.\n")
	b.WriteString("# TYPE sdp_hub_http_request_duration_seconds_count counter\n")
	b.WriteString("sdp_hub_http_request_duration_seconds_count " +
		strconv.FormatUint(reqDurationCount.Load(), 10) + "\n")

	b.WriteString("# HELP sdp_hub_in_flight_requests Current in-flight HTTP requests.\n")
	b.WriteString("# TYPE sdp_hub_in_flight_requests gauge\n")
	b.WriteString("sdp_hub_in_flight_requests " + strconv.FormatInt(inFlight.Load(), 10) + "\n")

	b.WriteString("# HELP sdp_hub_uptime_seconds Process uptime in seconds.\n")
	b.WriteString("# TYPE sdp_hub_uptime_seconds gauge\n")
	b.WriteString("sdp_hub_uptime_seconds " + strconv.FormatInt(int64(time.Since(startTime).Seconds()), 10) + "\n")

	b.WriteString("# HELP sdp_hub_build_info Build information (version, commit, goversion).\n")
	b.WriteString("# TYPE sdp_hub_build_info gauge\n")
	b.WriteString(`sdp_hub_build_info{version="` + buildVersion +
		`",commit="` + buildCommit +
		`",goversion="` + buildGoVersion + `"} 1` + "\n")

	return b.String()
}
