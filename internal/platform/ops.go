package platform

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// OpsHandler 是 §6.3 平台运维视图的只读聚合端点：
//
//	GET /ops/runners —— 接入目标聚合（在线/版本/心跳）
//	GET /ops/health  —— 平台健康（DB ping + 进程信息）
//	GET /ops/quota   —— 用量（近 30 天按目标维度的运行计数）
//
// 口径说明（2026-10-12 落码裁定，2026-10-11 放开 org 维度）：run 行已反规范化
// org_id（migrations 0019），配额同时给出目标维度（items）与 org 维度
// （orgItems）；org 维度只含归属解析成功的行，未归属行仍计入目标维度。
type OpsHandler struct {
	db      *gorm.DB
	version string
	started time.Time
}

func NewOpsHandler(db *gorm.DB, version string) *OpsHandler {
	return &OpsHandler{db: db, version: version, started: time.Now()}
}

// runnerAgg mirrors the target aggregate row.
type runnerAgg struct {
	Name            string     `json:"name"`
	Vendor          string     `json:"vendor"`
	Region          string     `json:"region"`
	Status          string     `json:"status"`
	AgentVersion    string     `json:"agentVersion,omitempty"`
	LastHeartbeatAt *time.Time `json:"lastHeartbeatAt,omitempty"`
	Online          bool       `json:"online"`
}

// GetRunners godoc
//
//	@Summary	Runner 聚合视图（§6.3）
//	@Produce	json
//	@Success	200	{object}	common.Envelope
//	@Router		/ops/runners [get]
func (h *OpsHandler) GetRunners(c *gin.Context) {
	rows := []runnerAgg{}
	err := h.db.Table("targets").
		Select("name, vendor, region, status, agent_version, last_heartbeat_at").
		Order("name asc").Find(&rows).Error
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	online := 0
	for i := range rows {
		// 在线判定与 target 域一致口径：心跳 90s 内。
		rows[i].Online = rows[i].LastHeartbeatAt != nil &&
			time.Since(*rows[i].LastHeartbeatAt) < 90*time.Second
		if rows[i].Online {
			online++
		}
	}
	common.OK(c, gin.H{"items": rows, "total": len(rows), "online": online})
}

// GetHealth godoc
//
//	@Summary	平台健康（§6.3）
//	@Produce	json
//	@Success	200	{object}	common.Envelope
//	@Router		/ops/health [get]
func (h *OpsHandler) GetHealth(c *gin.Context) {
	dbOk, dbErr := true, error(nil)
	sql, err := h.db.DB()
	if err != nil {
		dbOk, dbErr = false, err
	} else if perr := sql.Ping(); perr != nil {
		dbOk, dbErr = false, perr
	}
	status := "healthy"
	if !dbOk {
		status = "degraded"
	}
	common.OK(c, gin.H{
		"status":    status,
		"version":   h.version,
		"startedAt": h.started,
		"now":       time.Now(),
		"checks":    gin.H{"database": gin.H{"ok": dbOk, "error": errString(dbErr)}},
	})
}

// quotaRow is one per-target usage aggregate.
type quotaRow struct {
	TargetID   string `json:"targetId"`
	TargetName string `json:"targetName"`
	Runs30d    int64  `json:"runs30d"`
	RunsTotal  int64  `json:"runsTotal"`
}

// orgQuotaRow is one per-org usage aggregate. Enabled by the run-row
// denormalization (migrations 0019: pipeline_runs.org_id) — the v1
// target-only ruling's "反规范化后再放开" precondition. Runs with no org
// attribution (legacy rows / unresolved triggers) are excluded here but
// still counted in the target dimension.
type orgQuotaRow struct {
	OrgID     string `json:"orgId"`
	Runs30d   int64  `json:"runs30d"`
	RunsTotal int64  `json:"runsTotal"`
}

// GetQuota godoc
//
//	@Summary	用量统计（§6.3，v1 按目标维度）
//	@Produce	json
//	@Success	200	{object}	common.Envelope
//	@Router		/ops/quota [get]
func (h *OpsHandler) GetQuota(c *gin.Context) {
	rows := []quotaRow{}
	err := h.db.Raw(`
		SELECT t.id AS target_id, t.name AS target_name,
		       COUNT(r.id) FILTER (WHERE r.created_at >= now() - interval '30 days') AS runs30d,
		       COUNT(r.id) AS runs_total
		FROM targets t LEFT JOIN pipeline_runs r ON r.target_id = t.id
		GROUP BY t.id, t.name ORDER BY runs30d DESC, t.name ASC`).Scan(&rows).Error
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	orgRows := []orgQuotaRow{}
	if err := h.db.Raw(`
		SELECT r.org_id::text AS org_id,
		       COUNT(r.id) FILTER (WHERE r.created_at >= now() - interval '30 days') AS runs30d,
		       COUNT(r.id) AS runs_total
		FROM pipeline_runs r
		WHERE r.org_id IS NOT NULL
		GROUP BY r.org_id ORDER BY runs30d DESC, org_id ASC`).Scan(&orgRows).Error; err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{
		"items":     rows,
		"orgItems":  orgRows,
		"window":    "30d",
		"dimension": "target+org",
	})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
