package notification

// 服务端已读游标单测（RUNNER-REFLUX-SPEC §6 / STATUS #20 hub 半边）：
// unreadCount = CreatedAt > lastReadAt 的行动项数（服务端时钟）；无游标 =
// 全部未读的旧行为（console localStorage 兜底语义不变）。

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	runmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

type fakeApprovals struct{ items []runmodels.PipelineApproval }

func (f *fakeApprovals) ListPending(limit int) ([]runmodels.PipelineApproval, error) {
	return f.items, nil
}

type fakeRuns struct{}

func (fakeRuns) PipelineIDOf(ctx context.Context, runID uuid.UUID) (uuid.UUID, error) {
	return uuid.Nil, nil
}

type fakeCursor struct{ lastRead time.Time }

func (f *fakeCursor) LastReadAt(string) (time.Time, error) { return f.lastRead, nil }
func (f *fakeCursor) AdvanceTo(_ string, at time.Time) error {
	f.lastRead = at
	return nil
}

func mkApproval(createdAt time.Time) runmodels.PipelineApproval {
	return runmodels.PipelineApproval{
		ID:          uuid.New(),
		RunID:       uuid.New(),
		Status:      "Pending",
		CreatedAt:   createdAt,
		ComponentID: uuid.New(),
	}
}

func TestList_UnreadCountsOnlyNewerThanCursor(t *testing.T) {
	old1 := mkApproval(time.Now().Add(-2 * time.Hour))
	old2 := mkApproval(time.Now().Add(-1 * time.Hour))
	fresh := mkApproval(time.Now().Add(-5 * time.Minute))
	svc := New(&fakeApprovals{items: []runmodels.PipelineApproval{old1, old2, fresh}}, fakeRuns{})
	cursor := &fakeCursor{lastRead: time.Now().Add(-30 * time.Minute)}
	svc.SetReadStore(cursor)

	items, unread, err := svc.List(context.Background(), "sub-1", 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3 (游标只影响未读数，不下线列表)", len(items))
	}
	if unread != 1 {
		t.Fatalf("unread = %d, want 1（仅游标之后的 fresh 计入）", unread)
	}
}

func TestList_NoCursor_AllUnread(t *testing.T) {
	svc := New(&fakeApprovals{items: []runmodels.PipelineApproval{
		mkApproval(time.Now().Add(-time.Hour)),
		mkApproval(time.Now().Add(-time.Hour)),
	}}, fakeRuns{})

	_, unread, err := svc.List(context.Background(), "sub-1", 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if unread != 2 {
		t.Fatalf("unread = %d, want 2（旧库/未挂游标时保持旧行为）", unread)
	}
}

func TestMarkRead_DelegatesToCursor(t *testing.T) {
	svc := New(&fakeApprovals{}, fakeRuns{})
	cursor := &fakeCursor{}
	svc.SetReadStore(cursor)

	at := time.Now().Add(-time.Minute)
	if err := svc.MarkRead("sub-1", at); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if !cursor.lastRead.Equal(at) {
		t.Fatalf("cursor = %v, want %v（service 原样转发 readAt）", cursor.lastRead, at)
	}
}
