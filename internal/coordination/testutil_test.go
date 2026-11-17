package coordination

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// errTestConsume marks a ref the fake consumer must fail.
var errTestConsume = errors.New("test consume failure")

// portFromTestServer extracts the listen port of an httptest server so the
// report client can be pointed at it.
func portFromTestServer(srv *httptest.Server) int {
	idx := strings.LastIndex(srv.URL, ":")
	port, err := strconv.Atoi(srv.URL[idx+1:])
	if err != nil {
		return 80
	}
	return port
}

// decodeReportResponse parses a 200 report body.
func decodeReportResponse(t *testing.T, body string) ReportResponse {
	t.Helper()
	var resp ReportResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode report response %q: %v", body, err)
	}
	return resp
}
