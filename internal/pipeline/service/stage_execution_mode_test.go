package service

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

// 覆盖 pipeline_stages.execution_mode 的写入语义。
//
// 背景：该字段的**契约与 DDL 早已拍板**（hub/API-REFERENCE.md `stages[].executionMode`、
// hub/DATA-MODEL.md §6.4-①），但 hub 结构体此前没有它，于是 console 编排页的阶段
// 「并行/串行」开关无处落库。本次补上字段后，最容易出错的两点就是本文件断言的东西：
//
//  1. 缺省语义不能被改坏 —— 老客户端不传 executionMode 时，行为必须与加字段前一致
//     （落到 DDL 的同名默认值 parallel），而不是存进一个空字符串模式；
//  2. Update 必须区分"没传"与"传了" —— Update 同时承担改名与重排，若空串也照写，
//     一次「重排 sequence」就会把阶段的串行模式静默重置回默认值。
//
// 全部走内存 fake，不需要 Postgres（与 stage_guard_test.go 同一套路）。

func assertAPIStatus(t *testing.T, err error, wantHTTP int, wantCode, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: want %d error, got nil", what, wantHTTP)
	}
	ae, ok := err.(*common.APIError)
	if !ok {
		t.Fatalf("%s: want *common.APIError, got %T (%v)", what, err, err)
	}
	if ae.Code != wantHTTP {
		t.Fatalf("%s: want HTTP %d, got %d (%s)", what, wantHTTP, ae.Code, ae.ErrorCode)
	}
	if wantCode != "" && ae.ErrorCode != wantCode {
		t.Fatalf("%s: want %s, got %s", what, wantCode, ae.ErrorCode)
	}
}

// ---- Create：缺省落 parallel，非法值拒收 -------------------------------------

func TestStageCreate_DefaultsExecutionModeToParallel(t *testing.T) {
	live := uuid.New()
	svc, store := newGuardFixture(live)

	// 不传 executionMode（老客户端 / 空表单）——必须落到与 DDL default 一致的 parallel。
	if err := svc.Create(&models.PipelineStage{PipelineID: live, Name: "构建", Sequence: 1}); err != nil {
		t.Fatalf("Create: unexpected error %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("want 1 created stage, got %d", len(store.created))
	}
	if got := store.created[0].ExecutionMode; got != models.ExecutionModeParallel {
		t.Fatalf("want default %q, got %q", models.ExecutionModeParallel, got)
	}
}

func TestStageCreate_NormalizesExecutionModeCase(t *testing.T) {
	live := uuid.New()
	svc, store := newGuardFixture(live)

	// 权威文档之间曾经不一致：API-REFERENCE 写小写，DATA-MODEL §6.4-① 的草案 ALTER
	// 写 'Parallel'。客户端照后者传大写不该被判 400 —— 归一化后落库的必须是规范小写，
	// 否则 DB 里会同时存在 'parallel' 与 'Parallel' 两种拼写、CHECK 约束也会挂。
	if err := svc.Create(&models.PipelineStage{
		PipelineID: live, Name: "测试", Sequence: 2, ExecutionMode: "  Serial ",
	}); err != nil {
		t.Fatalf("Create with '  Serial ': unexpected error %v", err)
	}
	if got := store.created[0].ExecutionMode; got != models.ExecutionModeSerial {
		t.Fatalf("want normalized %q, got %q", models.ExecutionModeSerial, got)
	}
}

func TestStageCreate_RejectsUnknownExecutionMode(t *testing.T) {
	live := uuid.New()
	svc, store := newGuardFixture(live)

	err := svc.Create(&models.PipelineStage{
		PipelineID: live, Name: "构建", Sequence: 1, ExecutionMode: "concurrent",
	})
	// 静默纠正成 parallel 会让调用方以为自己的设置生效了 —— 必须显式 400。
	assertAPIStatus(t, err, http.StatusBadRequest, "ERR.08400001", "Create with bogus mode")
	if len(store.created) != 0 {
		t.Fatalf("stage was written despite invalid mode: %+v", store.created)
	}
}

// ---- Update：空串 = 不改（关键的不对称） -------------------------------------

func TestStageUpdate_FlipsExecutionMode(t *testing.T) {
	live := uuid.New()
	svc, store := newGuardFixture(live)
	id := uuid.New()
	row := models.PipelineStage{PipelineID: live, Name: "构建", Sequence: 1}
	row.ID = id // ID 是内嵌 common.Base 的提升字段，不能出现在复合字面量里
	row.ExecutionMode = models.ExecutionModeParallel
	store.rows[id] = row

	out, err := svc.Update(id, &models.PipelineStage{Name: "构建", Sequence: 1, ExecutionMode: "serial"})
	if err != nil {
		t.Fatalf("Update: unexpected error %v", err)
	}
	if out.ExecutionMode != models.ExecutionModeSerial {
		t.Fatalf("want serial, got %q", out.ExecutionMode)
	}
	if got := store.rows[id].ExecutionMode; got != models.ExecutionModeSerial {
		t.Fatalf("store: want serial, got %q", got)
	}
}

func TestStageUpdate_EmptyExecutionModeLeavesItUntouched(t *testing.T) {
	live := uuid.New()
	svc, store := newGuardFixture(live)
	id := uuid.New()
	row := models.PipelineStage{PipelineID: live, Name: "构建", Sequence: 1}
	row.ID = id
	row.ExecutionMode = models.ExecutionModeSerial
	store.rows[id] = row

	// 一次纯粹的「重排 sequence」：body 里没有 executionMode。串行模式必须原样保留。
	if _, err := svc.Update(id, &models.PipelineStage{Name: "构建", Sequence: 3}); err != nil {
		t.Fatalf("Update (reorder only): unexpected error %v", err)
	}
	got := store.rows[id]
	if got.ExecutionMode != models.ExecutionModeSerial {
		t.Fatalf("reorder wiped executionMode: want serial, got %q", got.ExecutionMode)
	}
	if got.Sequence != 3 {
		t.Fatalf("reorder did not apply: want sequence 3, got %d", got.Sequence)
	}
}

func TestStageUpdate_RejectsUnknownExecutionModeWithoutWriting(t *testing.T) {
	live := uuid.New()
	svc, store := newGuardFixture(live)
	id := uuid.New()
	row := models.PipelineStage{PipelineID: live, Name: "构建", Sequence: 1}
	row.ID = id
	row.ExecutionMode = models.ExecutionModeSerial
	store.rows[id] = row

	_, err := svc.Update(id, &models.PipelineStage{Name: "改名了", Sequence: 1, ExecutionMode: "parallel-ish"})
	assertAPIStatus(t, err, http.StatusBadRequest, "ERR.08400001", "Update with bogus mode")

	// 关键：既没改模式，也**没顺手把改名写进去** —— 校验先于持久化，非法请求不产生半成品。
	got := store.rows[id]
	if got.ExecutionMode != models.ExecutionModeSerial {
		t.Fatalf("invalid update corrupted mode: got %q", got.ExecutionMode)
	}
	if got.Name != "构建" {
		t.Fatalf("invalid update partially applied the rename: got %q", got.Name)
	}
}
