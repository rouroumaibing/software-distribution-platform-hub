package service

import (
	"context"
	"sort"
	"time"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/storage"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
)

// ArtifactReconciler is the **report-only** orphan reconciliation for
// Artifact objects (backlog B-16 对账 / DELETE-CONTRACT §6.6-4).
//
// 为什么只报告、不自动删：对象删除不可逆，而对账的输入（DB 行集合 vs 对象集合）
// 存在**合法的**边界情形 —— 归档任务刚上传完对象、DB 行还没落库，这一刻"有对象
// 没行"完全正常。自动删会把这类在途制品直接抹掉。所以本作业只产出差异清单，
// 清理由人按清单执行。这条边界是用户拍板的（§6.5 决策 4：「少删」优于「多删」）。
type ArtifactReconciler struct {
	db     StorageKeySource
	store  storage.Enumerator
	prefix string
	// maxObjects bounds the object listing. 0 用 DefaultMaxObjects。
	maxObjects int
	// maxSample bounds how many keys the report carries per side (counts stay
	// exact). 0 用 DefaultMaxSample。
	maxSample int
}

// StorageKeySource is the DB side: every storage key the metadata believes in.
type StorageKeySource interface {
	ListStorageKeys() ([]string, error)
}

// Defaults for the listing bound and the per-side sample size. The bound exists
// so a multi-million-object bucket cannot exhaust the hub's memory during a
// background job; the sample exists so the log line stays readable while the
// **counts** remain exact.
const (
	DefaultMaxObjects = 100_000
	DefaultMaxSample  = 100
)

func NewArtifactReconciler(db StorageKeySource, store storage.Enumerator, prefix string) *ArtifactReconciler {
	return &ArtifactReconciler{db: db, store: store, prefix: prefix,
		maxObjects: DefaultMaxObjects, maxSample: DefaultMaxSample}
}

// ReconciliationReport is the outcome of one pass.
type ReconciliationReport struct {
	CheckedAt time.Time `json:"checkedAt"`
	Prefix    string    `json:"prefix,omitempty"`
	// DBKeys / ObjectKeys are the two sides' sizes.
	DBKeys     int `json:"dbKeys"`
	ObjectKeys int `json:"objectKeys"`
	// MissingObjects: DB row exists, object absent — a下载必然 404 的制品。
	MissingObjects int `json:"missingObjects"`
	// OrphanObjects: object exists, no DB row — 不可达的残留对象。
	OrphanObjects int      `json:"orphanObjects"`
	MissingSample []string `json:"missingSample,omitempty"`
	OrphanSample  []string `json:"orphanSample,omitempty"`
	// Truncated is true when the object listing hit the bound, so the report
	// covers only a prefix of the store. 报告里必须显式说明，否则读者会以为
	// 这是一次完整对账。
	Truncated bool `json:"truncated"`
}

// Clean reports whether the pass found nothing to look at.
func (r ReconciliationReport) Clean() bool {
	return r.MissingObjects == 0 && r.OrphanObjects == 0
}

// DiffStorageKeys is the **pure** core: given the two sorted/unsorted key
// lists it returns the two sides' differences, each in ascending order.
// No DB, no store, no clock — so the comparison rules are unit-testable on
// their own (与 pipeline 的 DiffSnapshots 同一手法).
//
// 空字符串 key 被忽略：它不可能是合法存储键（ValidateKey 已拒绝），出现即代表
// 数据损坏，把它当键参与比对只会产生一条噪音行。
func DiffStorageKeys(dbKeys, objectKeys []string) (missing, orphan []string) {
	inDB := make(map[string]struct{}, len(dbKeys))
	for _, k := range dbKeys {
		if k == "" {
			continue
		}
		inDB[k] = struct{}{}
	}
	inStore := make(map[string]struct{}, len(objectKeys))
	for _, k := range objectKeys {
		if k == "" {
			continue
		}
		inStore[k] = struct{}{}
	}

	missing = make([]string, 0)
	for k := range inDB {
		if _, ok := inStore[k]; !ok {
			missing = append(missing, k)
		}
	}
	orphan = make([]string, 0)
	for k := range inStore {
		if _, ok := inDB[k]; !ok {
			orphan = append(orphan, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(orphan)
	return missing, orphan
}

// RunOnce performs one reconciliation pass.
func (r *ArtifactReconciler) RunOnce(now time.Time) (*ReconciliationReport, error) {
	limit := r.maxObjects
	if limit <= 0 {
		limit = DefaultMaxObjects
	}
	objects, truncated, err := r.store.ListObjects(r.prefix, limit)
	if err != nil {
		return nil, err
	}
	dbKeys, err := r.db.ListStorageKeys()
	if err != nil {
		return nil, err
	}

	objectKeys := make([]string, 0, len(objects))
	for _, o := range objects {
		objectKeys = append(objectKeys, o.Key)
	}
	missing, orphan := DiffStorageKeys(dbKeys, objectKeys)

	sample := r.maxSample
	if sample <= 0 {
		sample = DefaultMaxSample
	}
	rep := &ReconciliationReport{
		CheckedAt:      now,
		Prefix:         r.prefix,
		DBKeys:         len(dbKeys),
		ObjectKeys:     len(objectKeys),
		MissingObjects: len(missing),
		OrphanObjects:  len(orphan),
		MissingSample:  capList(missing, sample),
		OrphanSample:   capList(orphan, sample),
		Truncated:      truncated,
	}
	return rep, nil
}

func capList(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

// Run periodically reconciles until ctx is cancelled. A zero/negative interval
// **disables** the loop (returns at once) — 默认关闭，因为列出整个桶的成本随
// 存储规模线性增长，不该由一次部署悄悄打开；运维显式设置
// ARTIFACT_RECONCILE_INTERVAL（秒，如 86400）后才启用。
//
// 首轮不立即执行：与 BindingReaper.Run 不同 —— 那里跑的是几条 SQL，这里可能扫
// 整个对象存储，放在启动路径上会拖慢 hub 就绪。
func (r *ArtifactReconciler) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reportOnce(time.Now())
		}
	}
}

// reportOnce runs a pass and logs the result. A failure is logged and swallowed:
// the job is observational, it must never take the hub down or retry-storm.
func (r *ArtifactReconciler) reportOnce(now time.Time) {
	rep, err := r.RunOnce(now)
	if err != nil {
		applog.Warnf("artifact-reconcile: pass failed: %v", err)
		return
	}
	if rep.Clean() {
		applog.Infof("artifact-reconcile: clean dbKeys=%d objectKeys=%d truncated=%v",
			rep.DBKeys, rep.ObjectKeys, rep.Truncated)
		return
	}
	// 只报告，不删除（§6.5 决策 4）。清单交给运维按需处理。
	applog.Warnf("artifact-reconcile: orphans found missing=%d orphan=%d dbKeys=%d objectKeys=%d truncated=%v missingSample=%v orphanSample=%v",
		rep.MissingObjects, rep.OrphanObjects, rep.DBKeys, rep.ObjectKeys, rep.Truncated,
		rep.MissingSample, rep.OrphanSample)
}
