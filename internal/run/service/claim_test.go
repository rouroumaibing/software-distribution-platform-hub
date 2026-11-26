package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// ClaimForTarget 是 runner HTTP pull 直取的正确性核心：与 WS 推送共享
// MarkDispatching 占坑，一个 job 只能被一条通道领走（coordination-design §2.6）。
func TestClaimForTargetClaimsPendingJob(t *testing.T) {
	store := newFakeDispatchStore()
	svc := newTestService(&fakeDispatcher{}, store)
	runID := uuid.New()
	targetID := uuid.New()
	require.NoError(t, store.Create(&models.DispatchJob{
		PipelineRunID: runID,
		TargetID:      targetID,
		Payload:       mustMarshal(t, samplePayload()),
		State:         models.DispatchJobPending,
	}))

	claimed, err := svc.ClaimForTarget(context.Background(), targetID)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, runID, claimed[0].PipelineRunID)
	require.NotNil(t, claimed[0].Payload)

	job := store.mustGet(t, claimed[0].JobID)
	require.Equal(t, models.DispatchJobDispatched, job.State)
	require.Equal(t, 1, job.Attempts)

	// 已 dispatched 的 job 不可再被 claim（不重复投递）。
	again, err := svc.ClaimForTarget(context.Background(), targetID)
	require.NoError(t, err)
	require.Empty(t, again)
}

func TestClaimSkipsJobClaimedByPushPath(t *testing.T) {
	store := newFakeDispatchStore()
	svc := newTestService(&fakeDispatcher{}, store)
	targetID := uuid.New()
	require.NoError(t, store.Create(&models.DispatchJob{
		PipelineRunID: uuid.New(),
		TargetID:      targetID,
		Payload:       mustMarshal(t, samplePayload()),
		State:         models.DispatchJobDispatching, // push 路径已占坑
	}))

	claimed, err := svc.ClaimForTarget(context.Background(), targetID)
	require.NoError(t, err)
	require.Empty(t, claimed)
	job := store.all()[0]
	require.Equal(t, models.DispatchJobDispatching, job.State)
	require.Equal(t, 0, job.Attempts)
}

func TestClaimRetiresCorruptPayload(t *testing.T) {
	store := newFakeDispatchStore()
	svc := newTestService(&fakeDispatcher{}, store)
	targetID := uuid.New()
	require.NoError(t, store.Create(&models.DispatchJob{
		PipelineRunID: uuid.New(),
		TargetID:      targetID,
		Payload:       datatypes.JSON("{not-json"),
		State:         models.DispatchJobPending,
	}))

	claimed, err := svc.ClaimForTarget(context.Background(), targetID)
	require.NoError(t, err)
	require.Empty(t, claimed)
	job := store.all()[0]
	require.Equal(t, models.DispatchJobDead, job.State)
}

func TestSweepGateSemantics(t *testing.T) {
	svc := &PipelineRunService{}
	require.True(t, svc.gated(), "nil gate = 单实例模式，无条件执行")

	svc.SetSweepGate(func() bool { return false })
	require.False(t, svc.gated(), "standby 实例必须跳过扫描")

	svc.SetSweepGate(func() bool { return true })
	require.True(t, svc.gated(), "master 实例执行扫描")
}
