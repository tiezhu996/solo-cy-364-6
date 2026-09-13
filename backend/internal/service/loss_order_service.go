package service

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ld/storeinventory/internal/constants"
	"github.com/ld/storeinventory/internal/model"
	"github.com/ld/storeinventory/internal/repository"
	"github.com/ld/storeinventory/internal/util"
)

// LossOrderService 库存报损单业务逻辑。
type LossOrderService interface {
	Create(storeID, skuID uint, quantity int, reason string, applicantID uint) (*model.LossOrder, error)
	List(page, pageSize int, storeID uint, status constants.LossOrderStatus) ([]model.LossOrder, int64, error)
	GetByID(id uint) (*model.LossOrder, error)
	Approve(id, reviewerID uint) (*model.LossOrder, error)
	Reject(id, reviewerID uint, rejectReason string) (*model.LossOrder, error)
}

type lossOrderService struct {
	lossRepo repository.LossOrderRepository
	invSvc   StoreInventoryService
	recSvc   StockRecordService
	db       *gorm.DB
	logger   *slog.Logger
}

// NewLossOrderService 构造报损单服务。
func NewLossOrderService(lossRepo repository.LossOrderRepository, invSvc StoreInventoryService, recSvc StockRecordService, db *gorm.DB, logger *slog.Logger) LossOrderService {
	return &lossOrderService{lossRepo: lossRepo, invSvc: invSvc, recSvc: recSvc, db: db, logger: logger}
}

func (s *lossOrderService) Create(storeID, skuID uint, quantity int, reason string, applicantID uint) (*model.LossOrder, error) {
	if quantity <= 0 {
		return nil, fmt.Errorf("create loss quantity[%d]: %w", quantity, util.ErrValidation)
	}
	if strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("create loss store[%d] sku[%d] reason empty: %w", storeID, skuID, util.ErrValidation)
	}
	order := &model.LossOrder{
		StoreID:     storeID,
		SKUID:       skuID,
		Quantity:    quantity,
		Reason:      reason,
		Status:      constants.LossPending,
		ApplicantID: &applicantID,
	}
	if err := s.lossRepo.Create(order); err != nil {
		s.logger.Error(constants.LogLossOrderCreateFailed, "store", storeID, "sku", skuID, "qty", quantity, "error", err)
		return nil, fmt.Errorf("create loss order store[%d] sku[%d]: %w", storeID, skuID, err)
	}
	s.logger.Info(constants.LogLossOrderCreateSuccess, "order_id", order.ID, "store", storeID, "sku", skuID, "qty", quantity, "applicant", applicantID)
	return order, nil
}

func (s *lossOrderService) List(page, pageSize int, storeID uint, status constants.LossOrderStatus) ([]model.LossOrder, int64, error) {
	orders, total, err := s.lossRepo.List(page, pageSize, storeID, status)
	if err != nil {
		return nil, 0, fmt.Errorf("list loss orders: %w", err)
	}
	s.logger.Info(constants.LogLossOrderListQueried, "store", storeID, "status", status, "total", total)
	return orders, total, nil
}

func (s *lossOrderService) GetByID(id uint) (*model.LossOrder, error) {
	order, err := s.lossRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("get loss order[id=%d]: %w", id, err)
	}
	s.logger.Info(constants.LogLossOrderDetailQueried, "order_id", id)
	return order, nil
}

// Approve 审批通过：校验库存、扣减库存、生成损耗出库记录、流转为已通过。
// 库存不足时审批失败，事务回滚，单据保持待审核。
func (s *lossOrderService) Approve(id, reviewerID uint) (*model.LossOrder, error) {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 行级锁 + CAS 状态条件，保证每张单据只能处理一次，并发审批安全。
		order, err := s.lossRepo.FindByIDTx(tx, id, true)
		if err != nil {
			return fmt.Errorf("approve loss order[id=%d]: %w", id, err)
		}
		if !constants.CanLossTransition(order.Status, constants.LossApproved) {
			s.logger.Warn(constants.LogLossOrderStatusInvalid, "order_id", id, "status", order.Status, "action", "approve")
			return fmt.Errorf("approve loss order[id=%d] status[%s]: %w", id, order.Status, util.ErrConflict)
		}
		// 库存不足：审批失败，回滚，单据保持待审核。
		if err := s.invSvc.CheckSufficientTx(tx, order.StoreID, order.SKUID, order.Quantity); err != nil {
			s.logger.Warn(constants.LogLossOrderApproveFailed, "order_id", id, "store", order.StoreID, "sku", order.SKUID, "qty", order.Quantity, "error", err)
			return fmt.Errorf("approve loss order[id=%d] by reviewer[%d]: %w", id, reviewerID, err)
		}
		// 损耗出库记录：RecordLoss 方向为 -1，CreateTx 内部原子扣减库存，无需再次调整。
		relatedID := id
		if _, err := s.recSvc.CreateTx(tx, order.StoreID, order.SKUID, constants.RecordLoss, order.Quantity, &relatedID); err != nil {
			return fmt.Errorf("approve loss order[id=%d] create loss record: %w", id, err)
		}
		now := time.Now()
		updates := map[string]interface{}{
			"reviewer_id": reviewerID,
			"reviewed_at": now,
		}
		if err := s.lossRepo.TransitionStatusTx(tx, id, constants.LossPending, constants.LossApproved, updates); err != nil {
			return fmt.Errorf("approve loss order[id=%d] status[%s]: %w", id, order.Status, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	order, err := s.lossRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("approve loss order[id=%d] reload: %w", id, err)
	}
	s.logger.Info(constants.LogLossOrderApproveSuccess, "order_id", id, "reviewer", reviewerID, "store", order.StoreID, "sku", order.SKUID, "qty", order.Quantity)
	return order, nil
}

// Reject 审批驳回：必须填写驳回原因，库存保持不变，单据流转为已驳回（终态）。
func (s *lossOrderService) Reject(id, reviewerID uint, rejectReason string) (*model.LossOrder, error) {
	if strings.TrimSpace(rejectReason) == "" {
		return nil, fmt.Errorf("reject loss order[id=%d] reason empty: %w", id, util.ErrValidation)
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		order, err := s.lossRepo.FindByIDTx(tx, id, true)
		if err != nil {
			return fmt.Errorf("reject loss order[id=%d]: %w", id, err)
		}
		if !constants.CanLossTransition(order.Status, constants.LossRejected) {
			s.logger.Warn(constants.LogLossOrderStatusInvalid, "order_id", id, "status", order.Status, "action", "reject")
			return fmt.Errorf("reject loss order[id=%d] status[%s]: %w", id, order.Status, util.ErrConflict)
		}
		now := time.Now()
		updates := map[string]interface{}{
			"reviewer_id":   reviewerID,
			"reject_reason": rejectReason,
			"reviewed_at":   now,
		}
		if err := s.lossRepo.TransitionStatusTx(tx, id, constants.LossPending, constants.LossRejected, updates); err != nil {
			return fmt.Errorf("reject loss order[id=%d] status[%s]: %w", id, order.Status, err)
		}
		return nil
	})
	if err != nil {
		s.logger.Warn(constants.LogLossOrderRejectFailed, "order_id", id, "reviewer", reviewerID, "error", err)
		return nil, err
	}
	order, err := s.lossRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("reject loss order[id=%d] reload: %w", id, err)
	}
	s.logger.Info(constants.LogLossOrderRejectSuccess, "order_id", id, "reviewer", reviewerID)
	return order, nil
}
