package db

import (
	"time"

	gormlogger "gorm.io/gorm/logger"

	artifactmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/models"
	catalogmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/models"
	clustermodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/cluster/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	componentmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
	environmentmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/models"
	orgmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/org/models"
	permmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	pipelinemodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	runmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"

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
		&clustermodels.Cluster{},
		&environmentmodels.Environment{},
		&pipelinemodels.Pipeline{}, &pipelinemodels.PipelineStage{}, &pipelinemodels.PipelineTaskTemplate{}, &pipelinemodels.PipelineVersion{},
		&artifactmodels.Artifact{},
		&runmodels.PipelineRun{}, &runmodels.TaskRun{}, &runmodels.TaskRunLog{}, &runmodels.RolloutRun{}, &runmodels.Approval{}, &runmodels.DispatchJob{},
		&permmodels.Role{}, &permmodels.ComponentRoleBinding{}, &permmodels.User{},
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
