package service

import (
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/ld/storeinventory/internal/constants"
	"github.com/ld/storeinventory/internal/model"
	"github.com/ld/storeinventory/internal/repository"
	"github.com/ld/storeinventory/internal/util"
)

var lossTestDBCounter int64

func newLossTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:loss_test_%d_%d?mode=memory&cache=shared", time.Now().UnixNano(), atomic.AddInt64(&lossTestDBCounter, 1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.Store{}, &model.SKU{}, &model.StoreInventory{},
		&model.TransferOrder{}, &model.StockRecord{}, &model.Stocktake{}, &model.LossOrder{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedLossFixture(t *testing.T, db *gorm.DB) (storeID, skuID, applicantID, reviewerID uint) {
	t.Helper()
	store := &model.Store{Code: "STT1", Name: "测试门店"}
	if err := db.Create(store).Error; err != nil {
		t.Fatalf("create store: %v", err)
	}
	sku := &model.SKU{Code: "SKUT1", Name: "测试商品", Unit: "件"}
	if err := db.Create(sku).Error; err != nil {
		t.Fatalf("create sku: %v", err)
	}
	applicant := &model.User{Username: "mgr", PasswordHash: "x", Name: "店长", Role: constants.RoleStoreManager, StoreID: &store.ID}
	reviewer := &model.User{Username: "adm", PasswordHash: "x", Name: "管理员", Role: constants.RoleAdmin}
	if err := db.Create(applicant).Error; err != nil {
		t.Fatalf("create applicant: %v", err)
	}
	if err := db.Create(reviewer).Error; err != nil {
		t.Fatalf("create reviewer: %v", err)
	}
	inv := &model.StoreInventory{StoreID: store.ID, SKUID: sku.ID, Quantity: 10, SafetyStock: 2}
	if err := db.Create(inv).Error; err != nil {
		t.Fatalf("create inventory: %v", err)
	}
	return store.ID, sku.ID, applicant.ID, reviewer.ID
}

func newLossService(db *gorm.DB) (LossOrderService, repository.StoreInventoryRepository, repository.StockRecordRepository) {
	logger := slog.Default()
	invRepo := repository.NewStoreInventoryRepository(db)
	skuRepo := repository.NewSKURepository(db)
	recordRepo := repository.NewStockRecordRepository(db)
	storeRepo := repository.NewStoreRepository(db)
	invSvc := NewStoreInventoryService(invRepo, skuRepo, db, logger)
	recSvc := NewStockRecordService(recordRepo, invRepo, storeRepo, skuRepo, invSvc, db, logger)
	lossSvc := NewLossOrderService(repository.NewLossOrderRepository(db), invSvc, recSvc, db, logger)
	return lossSvc, invRepo, recordRepo
}

func qtyOf(t *testing.T, db *gorm.DB, storeID, skuID uint) int {
	t.Helper()
	var inv model.StoreInventory
	if err := db.Where("store_id = ? AND sku_id = ?", storeID, skuID).First(&inv).Error; err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	return inv.Quantity
}

// 通过审批：扣减库存并生成损耗出库记录。
func TestLossOrderApproveDeductsStockAndCreatesRecord(t *testing.T) {
	db := newLossTestDB(t)
	storeID, skuID, applicantID, reviewerID := seedLossFixture(t, db)
	lossSvc, _, recordRepo := newLossService(db)

	order, err := lossSvc.Create(storeID, skuID, 3, "商品破损", applicantID)
	if err != nil {
		t.Fatalf("create loss: %v", err)
	}
	if order.Status != constants.LossPending {
		t.Fatalf("new order status = %s, want pending", order.Status)
	}

	if _, err := lossSvc.Approve(order.ID, reviewerID); err != nil {
		t.Fatalf("approve loss: %v", err)
	}

	if got := qtyOf(t, db, storeID, skuID); got != 7 {
		t.Fatalf("quantity after approve = %d, want 7", got)
	}
	records, total, err := recordRepo.List(1, 50, storeID, skuID, constants.RecordLoss)
	if err != nil {
		t.Fatalf("list records: %v", err)
	}
	if total != 1 || len(records) != 1 {
		t.Fatalf("loss records = %d (total %d), want 1", len(records), total)
	}
	if records[0].Quantity != 3 || records[0].RecordType != constants.RecordLoss {
		t.Fatalf("loss record wrong: %+v", records[0])
	}
	if records[0].RelatedOrderID == nil || *records[0].RelatedOrderID != order.ID {
		t.Fatalf("loss record related_order_id = %v, want %d", records[0].RelatedOrderID, order.ID)
	}
}

// 库存不足：审批失败、单据保持待审核、库存与记录不变。
func TestLossOrderApproveInsufficientStockStaysPending(t *testing.T) {
	db := newLossTestDB(t)
	storeID, skuID, applicantID, reviewerID := seedLossFixture(t, db)
	lossSvc, _, recordRepo := newLossService(db)

	order, err := lossSvc.Create(storeID, skuID, 20, "过期", applicantID)
	if err != nil {
		t.Fatalf("create loss: %v", err)
	}

	if _, err := lossSvc.Approve(order.ID, reviewerID); !errors.Is(err, util.ErrStockNotEnough) {
		t.Fatalf("approve insufficient err = %v, want ErrStockNotEnough", err)
	}

	if got := qtyOf(t, db, storeID, skuID); got != 10 {
		t.Fatalf("quantity after failed approve = %d, want 10 (unchanged)", got)
	}
	if _, total, err := recordRepo.List(1, 50, storeID, skuID, constants.RecordLoss); err != nil || total != 0 {
		t.Fatalf("loss records total = %d err = %v, want 0", total, err)
	}
	got, err := lossSvc.GetByID(order.ID)
	if err != nil {
		t.Fatalf("reload order: %v", err)
	}
	if got.Status != constants.LossPending {
		t.Fatalf("order status after failed approve = %s, want pending", got.Status)
	}
}

// 驳回：必须填写原因，库存不变，单据进入终态。
func TestLossOrderReject(t *testing.T) {
	db := newLossTestDB(t)
	storeID, skuID, applicantID, reviewerID := seedLossFixture(t, db)
	lossSvc, _, recordRepo := newLossService(db)

	order, err := lossSvc.Create(storeID, skuID, 2, "丢失", applicantID)
	if err != nil {
		t.Fatalf("create loss: %v", err)
	}

	if _, err := lossSvc.Reject(order.ID, reviewerID, "  "); !errors.Is(err, util.ErrValidation) {
		t.Fatalf("reject blank reason err = %v, want ErrValidation", err)
	}

	rejected, err := lossSvc.Reject(order.ID, reviewerID, "报损依据不足，请补充照片")
	if err != nil {
		t.Fatalf("reject loss: %v", err)
	}
	if rejected.Status != constants.LossRejected || rejected.RejectReason == "" || rejected.ReviewerID == nil {
		t.Fatalf("rejected order wrong: %+v", rejected)
	}
	if got := qtyOf(t, db, storeID, skuID); got != 10 {
		t.Fatalf("quantity after reject = %d, want 10 (unchanged)", got)
	}
	if _, total, err := recordRepo.List(1, 50, storeID, skuID, constants.RecordLoss); err != nil || total != 0 {
		t.Fatalf("loss records total = %d err = %v, want 0", total, err)
	}
}

// 每张单据只能处理一次：通过后再次通过/驳回均冲突。
func TestLossOrderProcessedOnlyOnce(t *testing.T) {
	db := newLossTestDB(t)
	storeID, skuID, applicantID, reviewerID := seedLossFixture(t, db)
	lossSvc, _, _ := newLossService(db)

	order, err := lossSvc.Create(storeID, skuID, 1, "破损", applicantID)
	if err != nil {
		t.Fatalf("create loss: %v", err)
	}
	if _, err := lossSvc.Approve(order.ID, reviewerID); err != nil {
		t.Fatalf("first approve: %v", err)
	}
	if _, err := lossSvc.Approve(order.ID, reviewerID); !errors.Is(err, util.ErrConflict) {
		t.Fatalf("second approve err = %v, want ErrConflict", err)
	}
	if _, err := lossSvc.Reject(order.ID, reviewerID, "原因"); !errors.Is(err, util.ErrConflict) {
		t.Fatalf("reject after approve err = %v, want ErrConflict", err)
	}
	if got := qtyOf(t, db, storeID, skuID); got != 9 {
		t.Fatalf("quantity = %d, want 9 (deducted exactly once)", got)
	}
}

// 创建校验：数量非法或原因为空。
func TestLossOrderCreateValidation(t *testing.T) {
	db := newLossTestDB(t)
	storeID, skuID, applicantID, _ := seedLossFixture(t, db)
	lossSvc, _, _ := newLossService(db)

	if _, err := lossSvc.Create(storeID, skuID, 0, "破损", applicantID); !errors.Is(err, util.ErrValidation) {
		t.Fatalf("create qty 0 err = %v, want ErrValidation", err)
	}
	if _, err := lossSvc.Create(storeID, skuID, 1, "", applicantID); !errors.Is(err, util.ErrValidation) {
		t.Fatalf("create blank reason err = %v, want ErrValidation", err)
	}
}

// 列表门店隔离：按门店过滤。
func TestLossOrderListStoreScope(t *testing.T) {
	db := newLossTestDB(t)
	storeID, skuID, applicantID, _ := seedLossFixture(t, db)
	lossSvc, _, _ := newLossService(db)

	other := &model.Store{Code: "STT2", Name: "其他门店"}
	if err := db.Create(other).Error; err != nil {
		t.Fatalf("create other store: %v", err)
	}
	if _, err := lossSvc.Create(storeID, skuID, 1, "破损", applicantID); err != nil {
		t.Fatalf("create loss own: %v", err)
	}
	if _, err := lossSvc.Create(other.ID, skuID, 1, "破损", applicantID); err != nil {
		t.Fatalf("create loss other: %v", err)
	}

	own, total, err := lossSvc.List(1, 50, storeID, "")
	if err != nil {
		t.Fatalf("list own: %v", err)
	}
	if total != 1 || len(own) != 1 || own[0].StoreID != storeID {
		t.Fatalf("own store list wrong: total=%d len=%d", total, len(own))
	}
	_, allTotal, err := lossSvc.List(1, 50, 0, "")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if allTotal != 2 {
		t.Fatalf("all store total = %d, want 2", allTotal)
	}
}
