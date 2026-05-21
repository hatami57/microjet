// Package postgres provides GORM-based table helpers with pagination support.
package postgres

import (
	"context"
	"errors"

	"github.com/hatami57/microjet/types"
	"gorm.io/gorm"
)

type Table[TEntity any] struct {
	entity *TEntity
	db     *gorm.DB
}

type ListRequest[T any] interface {
	CurrentPageData() (where *string, args []any, err error)
	CreateNextPageToken(items []T) (*string, error)
	PageSize() int
	OrderBy() string
	Where() (any, []any)
	WhereMap() map[string]any
}

func NewTable[TEntity any](db *gorm.DB) *Table[TEntity] {
	var entity TEntity
	return &Table[TEntity]{entity: &entity, db: db}
}

// DB returns the underlying gorm.DB for advanced use cases.
func (t *Table[TEntity]) DB() *gorm.DB { return t.db }

func (t *Table[TEntity]) CloneWithTx(tx *gorm.DB) *Table[TEntity] {
	return &Table[TEntity]{entity: t.entity, db: tx}
}

func (t *Table[TEntity]) Tx(ctx context.Context, op func(*Table[TEntity]) error) error {
	return t.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return op(&Table[TEntity]{entity: t.entity, db: tx})
	})
}

func (t *Table[TEntity]) Create(ctx context.Context, item any) error {
	return t.db.WithContext(ctx).Create(item).Error
}

func (t *Table[TEntity]) CreateMany(ctx context.Context, items any, batchSize int) error {
	return t.db.WithContext(ctx).CreateInBatches(items, batchSize).Error
}

func (t *Table[TEntity]) Update(ctx context.Context, item any) error {
	return t.db.WithContext(ctx).Save(item).Error
}

func (t *Table[TEntity]) UpdateMap(ctx context.Context, where map[string]any, values map[string]any) error {
	return t.db.WithContext(ctx).Where(where).Updates(values).Error
}

func (t *Table[TEntity]) Remove(ctx context.Context, conditions ...any) error {
	return t.db.WithContext(ctx).Delete(t.entity, conditions...).Error
}

func (t *Table[TEntity]) Count(ctx context.Context, where any, args ...any) (int64, error) {
	var count int64
	if err := t.db.WithContext(ctx).Where(where, args...).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (t *Table[TEntity]) Find(ctx context.Context, conditions ...any) (*TEntity, error) {
	var item TEntity
	if err := t.db.WithContext(ctx).First(&item, conditions...).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (t *Table[TEntity]) FindPreload(ctx context.Context, preloadFields []string, conditions ...any) (*TEntity, error) {
	var item TEntity
	q := t.db.WithContext(ctx)
	for _, preload := range preloadFields {
		q = q.Preload(preload)
	}
	if err := q.First(&item, conditions...).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (t *Table[TEntity]) List(ctx context.Context, req ListRequest[TEntity]) (*types.PagedResult[TEntity], error) {
	pageSize := req.PageSize()
	query := t.db.WithContext(ctx)

	if where, args := req.Where(); where != nil {
		query = query.Where(where, args...)
	} else if whereMap := req.WhereMap(); whereMap != nil {
		query = query.Where(whereMap)
	}

	if where, args, err := req.CurrentPageData(); err != nil {
		return nil, err
	} else if where != nil {
		query = query.Where(*where, args...)
	}

	var items []TEntity
	if err := query.Order(req.OrderBy()).Limit(pageSize + 1).Find(&items).Error; err != nil {
		return nil, err
	}

	var nextPageToken *string
	if len(items) > pageSize {
		items = items[:pageSize]
		var err error
		nextPageToken, err = req.CreateNextPageToken(items)
		if err != nil {
			return nil, err
		}
	}

	return &types.PagedResult[TEntity]{
		Items:         items,
		PageSize:      int32(pageSize),
		TotalCount:    -1,
		NextPageToken: nextPageToken,
	}, nil
}

func (t *Table[TEntity]) ListAll(ctx context.Context, req ListRequest[TEntity]) ([]*TEntity, error) {
	query := t.db.WithContext(ctx)

	if where, args := req.Where(); where != nil {
		query = query.Where(where, args...)
	} else if whereMap := req.WhereMap(); whereMap != nil {
		query = query.Where(whereMap)
	}

	var items []*TEntity
	if err := query.Order(req.OrderBy()).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}
