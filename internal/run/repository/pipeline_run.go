package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

type PipelineRunRepository struct {
	*common.Repository[models.PipelineRun]
}

func NewPipelineRunRepository(db *gorm.DB) *PipelineRunRepository {
	return &PipelineRunRepository{common.NewRepository[models.PipelineRun](db)}
}

func (r *PipelineRunRepository) FindByPipelineID(pipelineID uuid.UUID, p common.Pagination) ([]models.PipelineRun, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB {
		return db.Where("pipeline_id = ?", pipelineID).Order("created_at desc")
	})
}

// PipelineIDOf returns the pipeline a run belongs to, for building deep-links
// (通知中心把待审批项指向运行详情页).
func (r *PipelineRunRepository) PipelineIDOf(ctx context.Context, runID uuid.UUID) (uuid.UUID, error) {
	var pid uuid.UUID
	err := r.DB.WithContext(ctx).Table("pipeline_runs").Select("pipeline_id").Where("id = ?", runID).Scan(&pid).Error
	return pid, err
}

// FindAll lists runs across all pipelines (Run Center). All filters are
// optional; the zero value on any dimension means "no filter".
//   - phase:        exact match on the run phase (e.g. "Failed", "Running")
//   - componentID:  only runs whose pipeline belongs to this component
//   - createdAfter: only runs created at/after this time (STATUS §2 #15 运行时间窗过滤)
//
// componentID 存在的理由与 CountActiveByComponent 同一手法：流水线列表的
// 「最近运行」一列需要"该组件下所有流水线的运行，按时间倒序取头几条"，一次查询
// 就能在客户端按 pipeline_id 分组取最新，避免对每条流水线各发一次请求（N+1）。
// JOIN 而非 EXISTS：两张表都是软删，JOIN 与既有聚合查询保持同一写法。
func (r *PipelineRunRepository) FindAll(p common.Pagination, phase string, componentID uuid.UUID, createdAfter *time.Time) ([]models.PipelineRun, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB {
		if componentID != uuid.Nil {
			db = db.Joins("JOIN pipelines p ON p.id = pipeline_runs.pipeline_id").
				Where("p.component_id = ? AND p.deleted_at IS NULL", componentID)
		}
		if phase != "" {
			db = db.Where("pipeline_runs.phase = ?", phase)
		}
		if createdAfter != nil {
			db = db.Where("pipeline_runs.created_at >= ?", *createdAfter)
		}
		return db.Order("pipeline_runs.created_at desc")
	})
}

// GetByCRNameTarget finds a run by its in-cluster CR name + owning target,
// used when a Runner reports status back over the gateway.
func (r *PipelineRunRepository) GetByCRNameTarget(crName string, targetID uuid.UUID) (*models.PipelineRun, error) {
	var run models.PipelineRun
	if err := r.DB.First(&run, "cr_name = ? AND target_id = ?", crName, targetID).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

// CountByPipeline returns how many PipelineRun rows reference a pipeline, used
// by the pipeline service to block deletion while run history still exists.
func (r *PipelineRunRepository) CountByPipeline(pipelineID uuid.UUID) (int64, error) {
	var n int64
	if err := r.DB.Model(&models.PipelineRun{}).Where("pipeline_id = ?", pipelineID).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// CountActiveByPipeline counts runs in one of the given phases for a pipeline.
// empty phases → 0 (caller must pass a non-empty set, e.g. active phases).
func (r *PipelineRunRepository) CountActiveByPipeline(pipelineID uuid.UUID, phases []string) (int64, error) {
	if len(phases) == 0 {
		return 0, nil
	}
	var n int64
	if err := r.DB.Model(&models.PipelineRun{}).
		Where("pipeline_id = ? AND phase IN ?", pipelineID, phases).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// CountActiveByComponent counts active runs under any pipeline of a component.
func (r *PipelineRunRepository) CountActiveByComponent(componentID uuid.UUID, phases []string) (int64, error) {
	if len(phases) == 0 {
		return 0, nil
	}
	var n int64
	if err := r.DB.Model(&models.PipelineRun{}).
		Joins("JOIN pipelines p ON p.id = pipeline_runs.pipeline_id").
		Where("p.component_id = ? AND pipeline_runs.phase IN ?", componentID, phases).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// CountActiveByService counts active runs under any component of a service
// (joins components → pipelines → runs).
func (r *PipelineRunRepository) CountActiveByService(serviceID uuid.UUID, phases []string) (int64, error) {
	return countActiveByService(r.DB, serviceID, phases)
}

// CountActiveByComponentTx 是 CountActiveByComponent 的事务视图：在调用方传入的
// tx 上计数，用于把"无活跃运行"判定收进级联删除同一事务（关闭 DELETE-CONTRACT
// §1.3 的并发插入绕过窗口）。
func (r *PipelineRunRepository) CountActiveByComponentTx(tx *gorm.DB, componentID uuid.UUID, phases []string) (int64, error) {
	return countActiveByComponent(tx, componentID, phases)
}

// CountActiveByServiceTx 是 CountActiveByService 的事务视图，用途同上。
func (r *PipelineRunRepository) CountActiveByServiceTx(tx *gorm.DB, serviceID uuid.UUID, phases []string) (int64, error) {
	return countActiveByService(tx, serviceID, phases)
}

func countActiveByComponent(db *gorm.DB, componentID uuid.UUID, phases []string) (int64, error) {
	if len(phases) == 0 {
		return 0, nil
	}
	var n int64
	if err := db.Model(&models.PipelineRun{}).
		Joins("JOIN pipelines p ON p.id = pipeline_runs.pipeline_id").
		Where("p.component_id = ? AND pipeline_runs.phase IN ?", componentID, phases).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func countActiveByService(db *gorm.DB, serviceID uuid.UUID, phases []string) (int64, error) {
	if len(phases) == 0 {
		return 0, nil
	}
	var n int64
	if err := db.Model(&models.PipelineRun{}).
		Joins("JOIN pipelines p ON p.id = pipeline_runs.pipeline_id").
		Joins("JOIN components c ON c.id = p.component_id").
		Where("c.service_id = ? AND pipeline_runs.phase IN ?", serviceID, phases).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}
