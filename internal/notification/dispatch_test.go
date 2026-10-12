package notification

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/notification/models"
)

type fakeSubStore struct {
	mu   sync.Mutex
	rows map[string]models.Subscription
}

func (f *fakeSubStore) Get(subject string) (models.Subscription, error) {
	if s, ok := f.rows[subject]; ok {
		return s, nil
	}
	return models.Subscription{Subject: subject}, nil
}

func (f *fakeSubStore) Upsert(sub *models.Subscription) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rows == nil {
		f.rows = map[string]models.Subscription{}
	}
	f.rows[sub.Subject] = *sub
	return nil
}

type recordingMailer struct {
	mu   sync.Mutex
	sent []string // "to|subject"
}

func (r *recordingMailer) Send(to, subject, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, to+"|"+subject)
	return nil
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待条件超时")
}

func TestSubscription_Wants(t *testing.T) {
	cases := []struct {
		events string
		evt    string
		want   bool
	}{
		{"", "approval_required", true}, // 空 = 基线
		{"", "run_finished", false},     // 空 ≠ 通配
		{"run_finished,run_failed", "run_failed", true},
		{" run_finished , run_failed ", "run_finished", true}, // 带空格容错
		{"run_finished", "approval_required", false},
	}
	for _, c := range cases {
		s := models.Subscription{Events: c.events, Enabled: true}
		if got := s.Wants(c.evt); got != c.want {
			t.Errorf("Wants(%q) with events=%q = %v, want %v", c.evt, c.events, got, c.want)
		}
	}
}

func TestDispatch_EmailHappyPath(t *testing.T) {
	svc := New(nil, nil)
	store := &fakeSubStore{}
	_ = store.Upsert(&models.Subscription{Subject: "u1", Email: "a@b.c", Events: "run_finished", Enabled: true})
	svc.SetSubscriptionStore(store)
	mailer := &recordingMailer{}
	svc.SetMailer(mailer)

	svc.Dispatch("run_finished", "u1", "[SDP] done", "body")
	waitFor(t, func() bool { return len(mailer.sent) == 1 })
	if !strings.HasPrefix(mailer.sent[0], "a@b.c|") {
		t.Errorf("收件人/主题不符: %s", mailer.sent[0])
	}
}

func TestDispatch_Filters(t *testing.T) {
	svc := New(nil, nil)
	store := &fakeSubStore{}
	_ = store.Upsert(&models.Subscription{Subject: "u1", Email: "a@b.c", Events: "run_finished", Enabled: true})
	_ = store.Upsert(&models.Subscription{Subject: "u2", Email: "x@y.z", Events: "run_finished", Enabled: false}) // 静默
	_ = store.Upsert(&models.Subscription{Subject: "u3", Events: "run_finished", Enabled: true})                  // 无邮箱=站内 only
	svc.SetSubscriptionStore(store)
	mailer := &recordingMailer{}
	svc.SetMailer(mailer)

	svc.Dispatch("run_failed", "u1", "t", "b") // 事件不匹配
	svc.Dispatch("run_finished", "u2", "t", "b")
	svc.Dispatch("run_finished", "u3", "t", "b")
	svc.Dispatch("run_finished", "nobody", "t", "b") // 无订阅行

	time.Sleep(50 * time.Millisecond) // 给误发留时间窗
	if len(mailer.sent) != 0 {
		t.Errorf("过滤后不应发信，实际发送: %v", mailer.sent)
	}
}

func TestDispatch_UnconfiguredChannel(t *testing.T) {
	// 渠道未配置（SetMailer 未调）→ Dispatch 必须是受控 no-op，不得 panic。
	svc := New(nil, nil)
	svc.SetSubscriptionStore(&fakeSubStore{})
	svc.Dispatch("run_finished", "u1", "t", "b")
}
