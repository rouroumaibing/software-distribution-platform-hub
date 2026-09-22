package common

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Repository is a generic CRUD implementation shared by every module, so
// module-level repository files only add domain-specific query methods
// (e.g. FindByComponentID) instead of re-implementing basic CRUD.
//
// T must be a GORM model with a uuid.UUID primary key (Base or
// BaseNoSoftDelete embedded).
type Repository[T any] struct {
	DB *gorm.DB
}

func NewRepository[T any](db *gorm.DB) *Repository[T] { return &Repository[T]{DB: db} }

func (r *Repository[T]) Create(entity *T) error { return r.DB.Create(entity).Error }

func (r *Repository[T]) GetByID(id uuid.UUID) (*T, error) {
	var entity T
	if err := r.DB.First(&entity, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &entity, nil
}

// List applies optional scopes (e.g. filtering by a parent's foreign key)
// before counting and paginating.
func (r *Repository[T]) List(p Pagination, scopes ...func(*gorm.DB) *gorm.DB) ([]T, int64, error) {
	var entities []T
	var total int64

	query := r.DB.Model(new(T))
	for _, scope := range scopes {
		query = scope(query)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Offset(p.Offset()).Limit(p.Limit()).Find(&entities).Error; err != nil {
		return nil, 0, err
	}
	return entities, total, nil
}

func (r *Repository[T]) Update(entity *T) error { return r.DB.Save(entity).Error }

func (r *Repository[T]) Delete(id uuid.UUID) error {
	var entity T
	return r.DB.Delete(&entity, "id = ?", id).Error
}

// ListWhere returns every entity matching a raw WHERE clause (e.g.
// "status = ?", status). Use for simple single-clause lookups that don't need
// the pagination/offset machinery of List.
func (r *Repository[T]) ListWhere(where string, args ...any) ([]T, error) {
	var entities []T
	if err := r.DB.Where(where, args...).Find(&entities).Error; err != nil {
		return nil, err
	}
	return entities, nil
}
