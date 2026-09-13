package repository

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ld/storeinventory/internal/constants"
	"github.com/ld/storeinventory/internal/model"
	"github.com/ld/storeinventory/internal/util"
)

// LossOrderRepository 报损单仓储。
type LossOrderRepository interface {
	Create(order *model.LossOrder) error
	FindByID(id uint) (*model.LossOrder, error)
	FindByIDTx(tx *gorm.DB, id uint, forUpdate bool) (*model.LossOrder, error)
	List(page, pageSize int, storeID uint, status constants.LossOrderStatus) ([]model.LossOrder, int64, error)
	// TransitionStatusTx 仅当单据仍处于 from 状态时更新为 to，保证每张单据只能处理一次（CAS 乐观锁）。
	TransitionStatusTx(tx *gorm.DB, id uint, from, to constants.LossOrderStatus, updates map[string]interface{}) error
}

type lossOrderRepository struct {
	db *gorm.DB
}

// NewLossOrderRepository 构造报损单仓储。
func NewLossOrderRepository(db *gorm.DB) LossOrderRepository {
	return &lossOrderRepository{db: db}
}

func (r *lossOrderRepository) Create(order *model.LossOrder) error {
	if err := r.db.Create(order).Error; err != nil {
		return fmt.Errorf("create loss order: %w", err)
	}
	return nil
}

func (r *lossOrderRepository) FindByID(id uint) (*model.LossOrder, error) {
	return r.FindByIDTx(nil, id, false)
}

func (r *lossOrderRepository) FindByIDTx(tx *gorm.DB, id uint, forUpdate bool) (*model.LossOrder, error) {
	var o model.LossOrder
	q := dbOrTx(r.db, tx).
		Preload("Store").Preload("SKU").Preload("Applicant").Preload("Reviewer")
	if forUpdate {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.First(&o, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("find loss order by id: %w", util.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("find loss order by id: %w", err)
	}
	return &o, nil
}

func (r *lossOrderRepository) List(page, pageSize int, storeID uint, status constants.LossOrderStatus) ([]model.LossOrder, int64, error) {
	var orders []model.LossOrder
	var total int64
	q := r.db.Model(&model.LossOrder{})
	if storeID > 0 {
		q = q.Where("store_id = ?", storeID)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count loss orders: %w", err)
	}
	if err := q.Preload("Store").Preload("SKU").Preload("Applicant").Preload("Reviewer").
		Offset((page - 1) * pageSize).Limit(pageSize).Order("id desc").Find(&orders).Error; err != nil {
		return nil, 0, fmt.Errorf("list loss orders: %w", err)
	}
	return orders, total, nil
}

func (r *lossOrderRepository) TransitionStatusTx(tx *gorm.DB, id uint, from, to constants.LossOrderStatus, updates map[string]interface{}) error {
	set := map[string]interface{}{"status": to}
	for k, v := range updates {
		set[k] = v
	}
	res := dbOrTx(r.db, tx).Model(&model.LossOrder{}).
		Where("id = ? AND status = ?", id, from).
		Updates(set)
	if res.Error != nil {
		return fmt.Errorf("transition loss order status: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("transition loss order status: %w", util.ErrConflict)
	}
	return nil
}
