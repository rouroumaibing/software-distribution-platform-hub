package coordination

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func testGinEngine(coord *Coordinator) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewReportHandler(coord).RegisterRoutes(r)
	return r
}

func postReport(r *gin.Engine, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/coordination/report", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestReportServesMasterAssignment(t *testing.T) {
	fl, fw := newFakeLeases(), &fakeWorkers{}
	fw.setList("p1", "p2")
	c := newCoordinator(fl, fw, testOpts("a", "p1"))
	c.tick(context.Background()) // master with a computed mapping

	w := postReport(testGinEngine(c), `{"podIP":"p2","clusterID":"a"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	resp := decodeReportResponse(t, w.Body.String())
	if resp.MappingEpoch != 1 || len(resp.Shards) != 2 || resp.Shards[0] != 1 || resp.Shards[1] != 3 {
		t.Fatalf("assignment = %+v, want shards [1 3] epoch 1", resp)
	}
}

func TestReportRejectedWhenNotMaster(t *testing.T) {
	fl, fw := newFakeLeases(), &fakeWorkers{}
	fw.setList("p1", "p2")
	c := newCoordinator(fl, fw, testOpts("a", "p1"))
	c.tick(context.Background()) // master；hint = p1

	// 实例锁易主（如另一实例接管）→ 本实例降级为 worker，但 hint 已更新：
	// 上报给它的 producer 会被 409 指向新持有者。
	fl.setHolder(KeyInstance, "p9")
	c.tick(context.Background())
	if c.Role() != RoleWorker {
		t.Fatalf("precondition: role = %s, want worker", c.Role())
	}

	w := postReport(testGinEngine(c), `{"podIP":"p2","clusterID":"a"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (only master accepts reports)", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"master":"p9"`) {
		t.Fatalf("409 body lacks routing hint: %s", w.Body.String())
	}
}

func TestReportRejectedForForeignCluster(t *testing.T) {
	fl, fw := newFakeLeases(), &fakeWorkers{}
	fw.setList("p1")
	c := newCoordinator(fl, fw, testOpts("a", "p1"))
	c.tick(context.Background()) // master

	// standby 集群的实例不应上报（producer 先判集群级 leader）；防御性拒绝。
	w := postReport(testGinEngine(c), `{"podIP":"p5","clusterID":"b"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for foreign cluster", w.Code)
	}
}

func TestReportBadRequest(t *testing.T) {
	c := newCoordinator(newFakeLeases(), &fakeWorkers{}, testOpts("a", "p1"))
	w := postReport(testGinEngine(c), `{"podIP":"","clusterID":""}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestReportClientFollowsConflictHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"master":"10.0.0.9"}`))
	}))
	defer srv.Close()

	rc := newReportClient(portFromTestServer(srv))
	_, _, err := rc.Report(context.Background(), "127.0.0.1", ReportRequest{PodIP: "p1", ClusterID: "a"})
	var nm *NotMasterError
	if !errors.As(err, &nm) || nm.Master != "10.0.0.9" {
		t.Fatalf("err = %v, want NotMasterError with hint 10.0.0.9", err)
	}
}
