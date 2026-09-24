package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/models"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
)

// ArtifactGC is the **retention garbage collector** for artifacts: it enforces
// `expires_at` (backlog B-16 收口 —— 此前该列没有任何读取点，等于一句空话）。
//
// 它与 ArtifactReconciler 是**两件事**，别混：
//   - Reconciler 比对"DB 行集合 vs 对象集合"，输入**有合法歧义**（归档任务刚上传完、
//     DB 行还没落库的这一刻，"有对象没行"完全正常）⇒ 只报告、不删。
//   - GC 的输入**没有歧义**：`expires_at < now()` 是运维自己写下的保留期声明，一条
//     过期行就是一条"按声明该删"的行 ⇒ 它可以真的删。
//
// 这条区分是本批唯一的争议取舍（双向钢人见 plan §15.3）：不是因为"删对象危险"就一律
// 只报告 —— 危险的是**推断**，不是**执行**。运维显式声明过的意图，执行它才是对的。
//
// 仍然是**默认关闭、按批处理**：一次部署不该悄悄打开一个会删数据的作业，
// 而一批也不该无界（见 DefaultArtifactGCBatch）。
type ArtifactGC struct {
	repo  ExpiredArtifactSource
	store ObjectDeleter
	batch int
}

// ExpiredArtifactSource is the DB side: the expired rows plus the two mutations
// the pass needs. Narrow on purpose, so the pass is testable without Postgres
// (与 reconciler 的 StorageKeySource 同一手法)。
type ExpiredArtifactSource interface {
	// FindExpired returns expired rows, oldest first, **including** rows already
	// marked pending deletion (those are the retries).
	FindExpired(now time.Time, limit int) ([]models.Artifact, error)
	// MarkCleanupPending records an outstanding object removal.
	MarkCleanupPending(id uuid.UUID) error
	// Delete drops the metadata row (the artifact has no soft-delete).
	Delete(id uuid.UUID) error
}

// ObjectDeleter is the store side; only the delete capability (a strict subset
// of storage.Client — GC never presigns).
type ObjectDeleter interface {
	Delete(key string) error
}

// DefaultArtifactGCBatch bounds how many rows one pass touches. The bound exists
// because each row costs one blocking store call: an unbounded backlog of expired
// artifacts must not turn a single pass into thousands of sequential round-trips
// (the next tick continues where this one stopped — the job is incremental).
const DefaultArtifactGCBatch = 100

// NewArtifactGC builds the collector. A nil store is valid (object storage
// unconfigured): metadata GC still runs, and no object calls are made.
//
// 注意 Go 的 typed-nil 陷阱：调用方若把一个 `storage.Client(nil)` 直接转成
// ObjectDeleter，接口**非 nil** 而底层为 nil，下面 `g.store != nil` 会误判成"已配置"
// 并在 Delete 上 panic。所以 main.go 只在 `artifactStore != nil` 时才赋值。
func NewArtifactGC(repo ExpiredArtifactSource, store ObjectDeleter, batch int) *ArtifactGC {
	if batch <= 0 {
		batch = DefaultArtifactGCBatch
	}
	return &ArtifactGC{repo: repo, store: store, batch: batch}
}

// GCReport is the outcome of one pass.
type GCReport struct {
	CalledAt time.Time `json:"calledAt"`
	Batch    int       `json:"batch"`
	// Scanned is the number of expired rows this pass looked at.
	Scanned int `json:"scanned"`
	// Deleted is rows whose object was removed (or absent) and whose metadata
	// row was then dropped.
	Deleted int `json:"deleted"`
	// ObjectsFailed is rows retained because the object delete failed; they are
	// marked pending deletion and retried next pass.
	ObjectsFailed int `json:"objectsFailed"`
	// RowsFailed is rows whose object was gone but whose metadata delete failed.
	// Next pass retries them (the object delete is idempotent on a missing key).
	RowsFailed int `json:"rowsFailed"`
	// PendingSample names up to a handful of keys that failed, so the log line
	// is actionable without dumping an unbounded backlog.
	PendingSample []string `json:"pendingSample,omitempty"`
}

// Failed reports whether the pass left anything for a human to look at.
func (r GCReport) Failed() bool { return r.ObjectsFailed > 0 || r.RowsFailed > 0 }

// MaxGCSample caps how many failing keys the report carries.
const MaxGCSample = 20

// RunOnce performs one bounded pass.
//
// 删除顺序 = **先对象、后行**，与 ArtifactService.Delete 相反，且是刻意的：
//   - Delete() 面对的是"用户点删除"的即时语义，元数据是权威记录，先删它、对象清理
//     作为事后动作 —— 失败就只剩孤儿对象，由对账兜底。
//   - GC 面对的是"回收存储"的批处理语义。先删行则一旦对象删失败就**没有行可用于
//     重试**（行没了），只能留下孤儿对象等人读报告。先删对象则相反：行还在，本轮标记
//     pending、下轮重试，直到对象真删掉才删行 —— 失败被自动修复，不需要人。
//
// 两个驱动的 Delete 对"键不存在"都是幂等的（Local 容忍 os.IsNotExist，S3
// RemoveObject 成功返回），所以重试安全。
func (g *ArtifactGC) RunOnce(now time.Time) (*GCReport, error) {
	rows, err := g.repo.FindExpired(now, g.batch)
	if err != nil {
		return nil, err
	}
	rep := &GCReport{CalledAt: now, Batch: g.batch, Scanned: len(rows)}

	for _, a := range rows {
		if g.store != nil {
			if derr := g.store.Delete(a.StorageKey); derr != nil {
				rep.ObjectsFailed++
				if len(rep.PendingSample) < MaxGCSample {
					rep.PendingSample = append(rep.PendingSample, a.StorageKey)
				}
				if merr := g.repo.MarkCleanupPending(a.ID); merr != nil {
					// 连标记都失败：本轮无法推进，但也不能吞 —— 行还在，下轮仍会被选中。
					applog.Warnf("artifact-gc: marking pending failed id=%s key=%s err=%v",
						a.ID, a.StorageKey, merr)
				}
				applog.Warnf("artifact-gc: object delete failed, row retained for retry id=%s component=%s key=%s err=%v",
					a.ID, a.ComponentID, a.StorageKey, derr)
				continue
			}
		}
		if derr := g.repo.Delete(a.ID); derr != nil {
			rep.RowsFailed++
			applog.Warnf("artifact-gc: metadata delete failed id=%s key=%s err=%v", a.ID, a.StorageKey, derr)
			continue
		}
		rep.Deleted++
	}
	return rep, nil
}

// Run periodically collects until ctx is cancelled. A zero/negative interval
// **disables** the loop (returns at once) — 默认关闭：一个会删数据的作业不该由
// 一次部署悄悄打开。运维显式设置 ARTIFACT_GC_INTERVAL（秒）后才启用。
//
// 首轮不立即执行（与 reconciler 一致）：即便单批有界，让它挤在启动路径上也没有好处。
func (g *ArtifactGC) Run(ctx context.Context, interval time.Duration) {
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
			g.collectOnce(time.Now())
		}
	}
}

// collectOnce runs a pass and logs it. Failures are logged and swallowed: this is
// a housekeeping job, it must never take the hub down nor retry-storm. Anything
// it could not finish stays in the DB and is picked up next tick.
func (g *ArtifactGC) collectOnce(now time.Time) {
	rep, err := g.RunOnce(now)
	if err != nil {
		applog.Warnf("artifact-gc: pass failed: %v", err)
		return
	}
	if !rep.Failed() && rep.Deleted == 0 {
		applog.Infof("artifact-gc: nothing expired (scanned=%d batch=%d)", rep.Scanned, rep.Batch)
		return
	}
	applog.Infof("artifact-gc: scanned=%d deleted=%d objectsFailed=%d rowsFailed=%d pendingSample=%v",
		rep.Scanned, rep.Deleted, rep.ObjectsFailed, rep.RowsFailed, rep.PendingSample)
}
