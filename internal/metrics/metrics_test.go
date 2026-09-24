package metrics

import (
	"strings"
	"testing"
	"time"
)

func TestRenderContainsExpectedMetrics(t *testing.T) {
	RecordRequest("GET", "2xx", 5*time.Millisecond)
	RecordRequest("POST", "4xx", 10*time.Millisecond)
	RecordRequest("GET", "2xx", 15*time.Millisecond)

	out := Render()

	want := []string{
		"# TYPE sdp_hub_http_requests_total counter",
		`sdp_hub_http_requests_total{method="GET",status_class="2xx"} 2`,
		`sdp_hub_http_requests_total{method="POST",status_class="4xx"} 1`,
		"# TYPE sdp_hub_http_request_duration_seconds_total counter",
		"# TYPE sdp_hub_in_flight_requests gauge",
		"# TYPE sdp_hub_uptime_seconds gauge",
		"# TYPE sdp_hub_build_info gauge",
		"sdp_hub_build_info{",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("Render() missing %q\n--- output ---\n%s", w, out)
		}
	}

	// duration total must equal the sum we recorded (30ms = 0.030000s).
	if !strings.Contains(out, "sdp_hub_http_request_duration_seconds_total 0.030000") {
		t.Errorf("duration total not summed correctly:\n%s", out)
	}
}

func TestStatusClass(t *testing.T) {
	cases := map[int]string{
		200: "2xx", 204: "2xx",
		301: "3xx", 302: "3xx",
		400: "4xx", 404: "4xx", 429: "4xx",
		500: "5xx", 503: "5xx",
		0: "other",
	}
	for code, want := range cases {
		if got := StatusClass(code); got != want {
			t.Errorf("StatusClass(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestSetBuildInfo(t *testing.T) {
	SetBuildInfo("v1.2.3", "abc123", "go1.27")
	out := Render()
	if !strings.Contains(out, `version="v1.2.3"`) || !strings.Contains(out, `commit="abc123"`) {
		t.Errorf("build info not reflected in Render():\n%s", out)
	}
}
