package service

import (
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/search/models"
)

// 覆盖 CONSOLE-UI-DESIGN.md §5.3 / 附 A N-8 的 `GET /search` 服务层。
// 这一层是纯逻辑（解析 + 分发 + 夹取），用假 store 就能把每条约定钉住，
// 不必起 Postgres —— 与 StageService / ComponentService 的窄接口手法一致。

type fakeSearchStore struct {
	services   []models.Hit
	components []models.Hit
	pipelines  []models.Hit

	calls  []string
	limits []int
	gotQ   string
	failOn string
}

func (f *fakeSearchStore) record(t string, q string, limit int) {
	f.calls = append(f.calls, t)
	f.limits = append(f.limits, limit)
	f.gotQ = q
}

func (f *fakeSearchStore) SearchServices(q string, limit int) ([]models.Hit, error) {
	f.record(models.TypeService, q, limit)
	if f.failOn == models.TypeService {
		return nil, errors.New("boom")
	}
	return f.services, nil
}

func (f *fakeSearchStore) SearchComponents(q string, limit int) ([]models.Hit, error) {
	f.record(models.TypeComponent, q, limit)
	if f.failOn == models.TypeComponent {
		return nil, errors.New("boom")
	}
	return f.components, nil
}

func (f *fakeSearchStore) SearchPipelines(q string, limit int) ([]models.Hit, error) {
	f.record(models.TypePipeline, q, limit)
	if f.failOn == models.TypePipeline {
		return nil, errors.New("boom")
	}
	return f.pipelines, nil
}

func apiErrCode(t *testing.T, err error) string {
	t.Helper()
	ae, ok := err.(*common.APIError)
	if !ok {
		t.Fatalf("want *common.APIError, got %T (%v)", err, err)
	}
	if ae.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", ae.Code, ae.ErrorCode)
	}
	return ae.ErrorCode
}

// asErr 把 (value, error) 双返回值收成单 error，方便直接喂给 apiErrCode。
// 泛型：ParseTypes 回 []string、ParseLimit 回 int、Search 回 []models.Hit，
// 三种返回类型共用同一个收口函数。
func asErr[T any](_ T, err error) error { return err }

// --------------------------------------------------------------------------
// ParseTypes
// --------------------------------------------------------------------------

func TestParseTypes_DefaultsToAllThree(t *testing.T) {
	for _, raw := range []string{"", "   ", ",", " , , "} {
		types, err := ParseTypes(raw)
		if err != nil {
			t.Fatalf("ParseTypes(%q) should default silently, got %v", raw, err)
		}
		if len(types) != 3 {
			t.Fatalf("ParseTypes(%q) = %v, want all three types", raw, types)
		}
	}
}

func TestParseTypes_DedupesButKeepsCallerOrder(t *testing.T) {
	types, err := ParseTypes(" pipeline , component ,pipeline,")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 顺序 = UI 分组顺序，所以"去重"不能顺手排序。
	want := []string{models.TypePipeline, models.TypeComponent}
	if len(types) != len(want) || types[0] != want[0] || types[1] != want[1] {
		t.Fatalf("got %v, want %v", types, want)
	}
}

func TestParseTypes_RejectsUnknownInsteadOfIgnoring(t *testing.T) {
	// 拼错的类型必须 400：静默忽略会让调用方以为筛选生效了。
	code := apiErrCode(t, asErr(ParseTypes("component,compnent")))
	if code != ErrInvalidTypeCode {
		t.Fatalf("want %s, got %s", ErrInvalidTypeCode, code)
	}
}

func TestParseTypes_IsCaseInsensitive(t *testing.T) {
	types, err := ParseTypes("COMPONENT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(types) != 1 || types[0] != models.TypeComponent {
		t.Fatalf("got %v, want [component]", types)
	}
}

func TestParseTypes_ErrorMessageDoesNotLeakIntoSharedSingleton(t *testing.T) {
	// common.ErrBadRequest 之类的包级单例被 WithMessage 就地改写会互相污染；
	// 本模块用值拷贝构造错误，两次不同入参的消息必须各自独立。
	first := asErr(ParseTypes("nope"))
	_, _ = ParseTypes("alsonope")
	if first.Error() != "invalid search type: nope (allowed: service,component,pipeline)" {
		t.Fatalf("first error message was mutated: %q", first.Error())
	}
}

// --------------------------------------------------------------------------
// ParseLimit
// --------------------------------------------------------------------------

func TestParseLimit_DefaultsAndClampsLowSideSilently(t *testing.T) {
	for _, raw := range []string{"", "0", "-5"} {
		n, err := ParseLimit(raw)
		if err != nil {
			t.Fatalf("ParseLimit(%q) should not error, got %v", raw, err)
		}
		if n != DefaultLimit {
			t.Fatalf("ParseLimit(%q) = %d, want DefaultLimit %d", raw, n, DefaultLimit)
		}
	}
}

func TestParseLimit_RejectsOverMaxAndGarbage(t *testing.T) {
	// 上限是为了不让无分页端点变成全表扫描；写过头必须报错而不是悄悄截断。
	if code := apiErrCode(t, asErr(ParseLimit("100000"))); code != ErrInvalidLimitCode {
		t.Fatalf("want %s, got %s", ErrInvalidLimitCode, code)
	}
	apiErrCode(t, asErr(ParseLimit("abc")))
	if n, err := ParseLimit("50"); err != nil || n != MaxLimit {
		t.Fatalf("boundary 50 must be allowed, got n=%d err=%v", n, err)
	}
}

// --------------------------------------------------------------------------
// Search
// --------------------------------------------------------------------------

func TestSearch_EmptyQueryReturnsEmptyWithoutTouchingStore(t *testing.T) {
	store := &fakeSearchStore{}
	hits, err := NewSearchService(store).Search("   ", nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hits == nil || len(hits) != 0 {
		t.Fatalf("want empty non-nil slice (JSON []), got %#v", hits)
	}
	if len(store.calls) != 0 {
		t.Fatalf("empty query must not hit the store at all, got %v", store.calls)
	}
}

func TestSearch_OnlyQueriesRequestedTypes(t *testing.T) {
	store := &fakeSearchStore{}
	_, err := NewSearchService(store).Search("web", []string{models.TypeComponent}, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.calls) != 1 || store.calls[0] != models.TypeComponent {
		t.Fatalf("want only component queried, got %v", store.calls)
	}
	if store.gotQ != "web" {
		t.Fatalf("store got q=%q, want web", store.gotQ)
	}
}

func TestSearch_AppliesLimitPerTypeNotInTotal(t *testing.T) {
	// limit 是"每类各取"：某一类资源特别多时不至于把另一类挤成 0 条。
	store := &fakeSearchStore{}
	_, err := NewSearchService(store).Search("web", nil, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.calls) != 3 {
		t.Fatalf("empty types must search all three, got %v", store.calls)
	}
	for i, l := range store.limits {
		if l != 7 {
			t.Fatalf("call %d (%s) got limit %d, want 7 on every type", i, store.calls[i], l)
		}
	}
}

func TestSearch_ClampsBadLimitInsteadOfPropagatingIt(t *testing.T) {
	// 0/负数由 handler 的 ParseLimit 回落；这里再兜一层，防止调用方绕过解析。
	store := &fakeSearchStore{}
	if _, err := NewSearchService(store).Search("web", []string{models.TypeService}, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.limits[0] != DefaultLimit {
		t.Fatalf("want DefaultLimit %d, got %d", DefaultLimit, store.limits[0])
	}
	if _, err := NewSearchService(store).Search("web", []string{models.TypeService}, 9999); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.limits[1] != MaxLimit {
		t.Fatalf("want MaxLimit %d, got %d", MaxLimit, store.limits[1])
	}
}

func TestSearch_TrimsQueryBeforeDispatch(t *testing.T) {
	store := &fakeSearchStore{}
	if _, err := NewSearchService(store).Search("  web  ", []string{models.TypeService}, 5); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.gotQ != "web" {
		t.Fatalf("store got q=%q, want trimmed \"web\"", store.gotQ)
	}
}

func TestSearch_ConcatenatesInTypeOrder(t *testing.T) {
	id := uuid.New()
	store := &fakeSearchStore{
		services:   []models.Hit{{Type: models.TypeService, ID: id, Name: "svc"}},
		components: []models.Hit{{Type: models.TypeComponent, ID: id, Name: "comp"}},
		pipelines:  []models.Hit{{Type: models.TypePipeline, ID: id, Name: "pipe"}},
	}
	hits, err := NewSearchService(store).Search("x", []string{models.TypePipeline, models.TypeService}, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hits) != 2 || hits[0].Type != models.TypePipeline || hits[1].Type != models.TypeService {
		t.Fatalf("response must follow the caller's type order, got %+v", hits)
	}
}

func TestSearch_PropagatesStoreError(t *testing.T) {
	// 半个结果集比一次可重试的错误危险得多：任一类型失败即整体失败。
	store := &fakeSearchStore{failOn: models.TypeComponent}
	if _, err := NewSearchService(store).Search("web", nil, 10); err == nil {
		t.Fatalf("a failing type must fail the whole search")
	}
}

func TestSearch_RejectsTypePassedDirectlyWithoutParsing(t *testing.T) {
	store := &fakeSearchStore{}
	apiErrCode(t, asErr(NewSearchService(store).Search("web", []string{"compnent"}, 10)))
	if len(store.calls) != 0 {
		t.Fatalf("invalid type must not reach the store, got %v", store.calls)
	}
}
