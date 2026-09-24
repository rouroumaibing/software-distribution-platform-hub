package db

import (
	"time"

	gormlogger "gorm.io/gorm/logger"

	artifactmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/models"
	catalogmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	componentmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
	credentialmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/credentials/models"
	environmentmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/models"
	environmentgroupmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/environmentgroup/models"
	orgmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/org/models"
	permmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	pipelinemodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	runmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
	targetmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Config holds the Postgres connection parameters plus connection-pool tuning.
// It mirrors the structured init used by the legacy go-devops-gin service
// (host/user/password/... and MaxIdle/MaxOpen/ConnMaxLifetime) while keeping
// DSN as the primary input for simple local/dev setups.
type Config struct {
	// DSN is the full Postgres connection string. Takes precedence when set.
	DSN string
	// LogLevel is the GORM log verbosity: silent / error / warn / info.
	LogLevel string

	// Pool tuning. Zero values fall back to sane defaults in Open.
	MaxIdleConns    int
	MaxOpenConns    int
	ConnMaxLifetime time.Duration
}

// Open connects to Postgres, applies connection-pool tuning, runs AutoMigrate
// over every hub table, and returns the *gorm.DB. The caller owns the
// connection and should defer db.Close.
func Open(cfg Config) (*gorm.DB, error) {
	level := gormlogger.Warn
	switch cfg.LogLevel {
	case "silent":
		level = gormlogger.Silent
	case "error":
		level = gormlogger.Error
	case "info":
		level = gormlogger.Info
	}

	gdb, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{
		Logger: gormlogger.Default.LogMode(level),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, err
	}

	idle := cfg.MaxIdleConns
	if idle <= 0 {
		idle = 10
	}
	open := cfg.MaxOpenConns
	if open <= 0 {
		open = 100
	}
	life := cfg.ConnMaxLifetime
	if life <= 0 {
		life = time.Hour
	}
	sqlDB.SetMaxIdleConns(idle)
	sqlDB.SetMaxOpenConns(open)
	sqlDB.SetConnMaxLifetime(life)

	if err := gdb.AutoMigrate(
		&orgmodels.Org{}, &orgmodels.ServiceTree{},
		&catalogmodels.Service{},
		&componentmodels.Component{}, &componentmodels.ComponentConfig{}, &componentmodels.ComponentConfigHistory{},
		&targetmodels.Target{},
		// §9.9 / §9.5 接入编排：hub 侧 agent 操作台账（exec / install / upgrade）
		// 及其流式输出分片（SSE 重放源，migration 0018）。
		&targetmodels.AgentOp{}, &targetmodels.AgentOpLog{},
		&environmentmodels.Environment{},
		&credentialmodels.Credential{},
		&environmentgroupmodels.EnvironmentGroup{},
		&pipelinemodels.Pipeline{}, &pipelinemodels.PipelineStage{}, &pipelinemodels.PipelineTaskTemplate{}, &pipelinemodels.PipelineVersion{},
		&artifactmodels.Artifact{},
		&runmodels.PipelineRun{}, &runmodels.TaskRun{}, &runmodels.TaskRunLog{}, &runmodels.RolloutRun{}, &runmodels.Approval{}, &runmodels.DispatchJob{}, &runmodels.PipelineApproval{},
		// D3：不再有 `users` 表 —— hub 对身份无状态（ACCOUNT-PERMISSION-MODEL
		// §2.2），主体一律取 token `sub`。Role 仍保留（V1 `roles` 表本身不在
		// D3 的确认范围内，见 plans/UNIMPLEMENTED-MODULES-PLAN.md §14）。
		&permmodels.Role{}, &permmodels.ComponentRoleBinding{},
		&permmodels.PlatformRole{}, &permmodels.PlatformRoleBinding{}, &permmodels.ComponentRole{},
		// ACCOUNT-PERMISSION-MODEL §3 / §5.1③ / §6 / §7.2 — the four tables
		// that complete the permission model (2026-09-22 第八批).
		&permmodels.ResourceOwnership{}, &permmodels.RoleAPIMapping{},
		&permmodels.AuditLog{}, &permmodels.PermissionRequest{},
	); err != nil {
		return nil, err
	}

	logger.Infof("db: connected (pool idle=%d open=%d life=%s), auto-migrate complete", idle, open, life)
	return gdb, nil
}

// Close releases the underlying SQL connection pool.
func Close(gdb *gorm.DB) error {
	if gdb == nil {
		return nil
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
