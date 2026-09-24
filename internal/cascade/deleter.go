// Package cascade implements the 「域内级联删除」 prescribed by
// docs/hub/DELETE-CONTRACT.md §6.4 (and the §1.1 修正): removing a node in the
// service tree removes its **in-domain descendants in the same transaction**,
// instead of refusing because children exist.
//
// 为什么需要它: 本仓的删除长期只有"节点自身"这一层。父节点软删后，子表
// (environments / configs / stages / task_templates / artifacts / bindings)
// 没有 deleted_at 被置位 —— 这正是 §6.4 ② 点名的第一类残留「孤儿可见」。
// §1.1 原来的"有下级即拒绝"被 §6.4 取代：**只有"活跃运行"是硬规则**，其余一律级联。
//
// 严格按 §6.4 结论清单落表:
//
//	#2 Service 有下级 Component  → 级联**软删** components
//	#3 Component 有下级 Pipeline → 级联**软删** pipelines（含 stages / task_templates）
//	#4 Component 有下级 Environment → 级联**硬删** environments（平台侧；集群侧不回收，已拍板）
//	#5 Component 有下级 Config   → 级联删 configs；**保留 config_history**
//	#10 Artifact 对象存储        → 级联 + 清理标记（交给 Artifact GC 回收对象）
//	   bindings                   → 「随父级联」硬删（FK 本就是 ON DELETE CASCADE，
//	                                软删不触发才需要显式删；见下 DELETE 顺序注释）
//
// **本包不做**的两件事，以及为什么:
//   - **不检查活跃运行**：那是 §6.4 #6 的唯一硬规则，已经落在各 service 的 Delete 里
//     （`ActiveRunCounter`）。放在这里会把"功能安全判定"和"级联执行"耦成一个不可
//     单测的巨块；调用方必须先判定再调用本包。
//   - **不做 Org → Service 级联**：§6.4 表格 #1 明确「不检 / 软删停用 + 平台管理员权限 /
//     物理清空走独立运维流程」。它与 §6.4 末尾四句建议里的"org → service → …"读起来
//     冲突（该文档内部的措辞不一致），按**更具体、且已被逐项拍板**的表格执行：
//     Org 删除 = 仅停用自身。理由见下方 org 的说明。
package cascade

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	artifactmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/models"
	catalogmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/models"
	componentmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
	envmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/models"
	permmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	pipelinemodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

// Deleter performs whole-subtree deletions. Every public method runs in **one**
// transaction: §1.3 记的「校验与删除不在同一事务」缺口就靠这一点关掉（校验在调用方、
// 删除在这里，两者之间不再有可插入窗口 —— 调用方判定"无活跃运行"与本次删除是一个
// 原子动作的前提是删除本身不留下"半删"状态）。
//
// 依赖 *gorm.DB 而不是各模块 repository：跨 6 个模块的清理若逐 repo 调用，就不再是
// 一个事务，§1.3 的缺口会原样回来。这是本包唯一的"直连 DB"，故刻意独立成包、
// 与业务 service 分离，便于单独审阅。
type Deleter struct {
	DB *gorm.DB
}

func NewDeleter(db *gorm.DB) *Deleter { return &Deleter{DB: db} }

// DeleteComponentSubtree 删除一个组件及其全部域内子资源（§6.4 #3/#4/#5/#10）。
func (d *Deleter) DeleteComponentSubtree(componentID uuid.UUID) error {
	return d.DB.Transaction(func(tx *gorm.DB) error {
		return deleteComponents(tx, tx.Table("components").Select("id").Where("id = ?", componentID))
	})
}

// DeleteServiceSubtree 删除一个服务、其全部组件及组件的子资源（§6.4 #2）。
func (d *Deleter) DeleteServiceSubtree(serviceID uuid.UUID) error {
	return d.DB.Transaction(func(tx *gorm.DB) error {
		if err := deleteComponents(tx, tx.Table("components").Select("id").Where("service_id = ?", serviceID)); err != nil {
			return err
		}
		// 服务自身最后软删（父后于子）。
		return tx.Where("id = ?", serviceID).Delete(&catalogmodels.Service{}).Error
	})
}

// 说明: 本包**刻意没有** `DeleteOrgSubtree`。`OrgService.Delete` 已经只做软删停用
// （其注释："组织删除是高危操作,只做软删除,保留恢复窗口,不允许立刻级联物理清除下属
// 全部资源"），与 §6.4 表格 #1 一致。这里不再提供一个同名方法 —— 那会让人以为
// "换个入口就能级联删 org"，而 §6.4 明确要求不要那样做。

// deleteComponents 对 scope 选出的**每个** component 执行同一套子资源清理，最后软删
// component 自身。scope 必须是只 `Select("id")` 的子查询（用 `Table()` 而非 `Model()`
// 构造 —— `Table()` 不会带上 GORM 的软删过滤，这样"父节点先前已被软删、子行还活着"
// 的孤儿也能被本次清理收走；用 `Model()` 会漏掉它们，等于把孤儿留到下一轮)。
func deleteComponents(tx *gorm.DB, scope *gorm.DB) error {
	// 每个元素 = (表, 一句删除)。顺序 = **深层先、父层后**。
	// 软删理论上与顺序无关（不触发 FK），但硬删层之间有关系，统一"子先父后"最不容易错。
	steps := []func() error{
		// ── 软删层（§6.4 #3）────────────────────────────────────────────
		// task_templates 只能经 stage 定位（表里没有 component_id）
		func() error {
			return tx.Where("stage_id IN (?)",
				tx.Table("pipeline_stages").Select("id").Where("pipeline_id IN (?)",
					tx.Table("pipelines").Select("id").Where("component_id IN (?)", scope))).
				Delete(&pipelinemodels.PipelineTaskTemplate{}).Error
		},
		func() error {
			return tx.Where("pipeline_id IN (?)",
				tx.Table("pipelines").Select("id").Where("component_id IN (?)", scope)).
				Delete(&pipelinemodels.PipelineStage{}).Error
		},
		func() error {
			return tx.Where("component_id IN (?)", scope).
				Delete(&pipelinemodels.Pipeline{}).Error
		},

		// ── 硬删层（§6.4 #4/#5 + bindings）──────────────────────────────
		// environments: 平台侧硬删；集群侧已部署资源**不回收**（§6.5 决策 1）。
		func() error {
			return tx.Where("component_id IN (?)", scope).
				Delete(&envmodels.Environment{}).Error
		},
		// configs: 硬删；`component_config_history` **不动** —— 它是审计事实，
		// 且其 component_id 外键指向 components，而 components 是**软删**，
		// 故外键不会级联、历史自然存活（§6.4 #5「保留 config_history」）。
		func() error {
			return tx.Where("component_id IN (?)", scope).
				Delete(&componentmodels.ComponentConfig{}).Error
		},
		// bindings: 「随父级联」硬删，不走"给硬删表补 deleted_at"那条路。
		// 依据: `component_role_bindings.component_id` 的外键**本就是 ON DELETE CASCADE**
		// （见迁移 0001/0002），即 schema 作者已表达"绑定不应比组件活得久"。软删父节点
		// 不会触发外键，所以这里显式补上同一语义。代价：组件被软删后其授权不可恢复
		// （审计由 `audit_log` 保留），这一点在 README/DELETE-CONTRACT 里记明。
		func() error {
			return tx.Where("component_id IN (?)", scope).
				Delete(&permmodels.ComponentRoleBinding{}).Error
		},

		// ── 清理标记（§6.4 #10）─────────────────────────────────────────
		// ⚠️ 必须**同时**把 expires_at 置为 now()，不能只改 cleanup_state。
		//    原因（真实代码逻辑）: Artifact GC 的取数谓词是
		//      `expires_at IS NOT NULL AND expires_at < now()`
		//    而**列表**谓词是 `cleanup_state <> 'pending_deletion'`。若只标 pending，
		//    这些行会立刻从列表消失、却因没有 expires_at 永远不被 GC 选中 ——
		//    变成"看不见也删不掉"的死路，比不标记更糟。置 now() 让本轮标记真正可回收。
		//    注意 GC 默认关闭（ARTIFACT_GC_INTERVAL=0），所以这只是**安全标记**：
		//    对象是否真的被删仍由运维通过开关决定。
		func() error {
			return tx.Table("artifacts").
				Where("component_id IN (?) AND cleanup_state <> ?", scope, artifactmodels.CleanupStatePendingDeletion).
				Updates(map[string]any{
					"cleanup_state": artifactmodels.CleanupStatePendingDeletion,
					"expires_at":    gorm.Expr("now()"),
				}).Error
		},

		// ── 父层最后 ────────────────────────────────────────────────────
		func() error {
			return tx.Where("id IN (?)", scope).Delete(&componentmodels.Component{}).Error
		},
	}

	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}
