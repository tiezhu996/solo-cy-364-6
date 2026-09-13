package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/ld/storeinventory/internal/constants"
	"github.com/ld/storeinventory/internal/model"
	"github.com/ld/storeinventory/internal/util"
)

// newLossConcurrencyDB 使用临时文件 + WAL 的 SQLite，支持多个连接并发读写，
// 文件位于 t.TempDir()，每次运行互不干扰，用例结束自动清理，可连续重复执行。
func newLossConcurrencyDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbFile := filepath.Join(t.TempDir(), "loss_concurrency.db")
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=10000&_synchronous=NORMAL", dbFile)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	// 允许多个连接，制造真实并发；写冲突由 WAL + busy_timeout 串行化。
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(8)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(
		&model.User{}, &model.Store{}, &model.SKU{}, &model.StoreInventory{},
		&model.TransferOrder{}, &model.StockRecord{}, &model.Stocktake{}, &model.LossOrder{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// 并发审批：大量 goroutine 同时通过同一张单据，必须恰好一次成功，库存只扣一次。
func TestLossOrderConcurrentApproveSucceedsOnce(t *testing.T) {
	db := newLossConcurrencyDB(t)
	lossSvc, _, recordRepo := newLossService(db)

	store := &model.Store{Code: "C1", Name: "并发门店"}
	sku := &model.SKU{Code: "CP1", Name: "并发商品", Unit: "件"}
	if err := db.Create(store).Error; err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := db.Create(sku).Error; err != nil {
		t.Fatalf("create sku: %v", err)
	}
	const initialQty = 100
	const lossQty = 5
	if err := db.Create(&model.StoreInventory{StoreID: store.ID, SKUID: sku.ID, Quantity: initialQty}).Error; err != nil {
		t.Fatalf("create inventory: %v", err)
	}
	order, err := lossSvc.Create(store.ID, sku.ID, lossQty, "批量破损", 1)
	if err != nil {
		t.Fatalf("create loss: %v", err)
	}

	const n = 24
	var (
		wg       sync.WaitGroup
		start    = make(chan struct{})
		success  int32
		conflict int32
		other    int32
	)
	wg.Add(n)
	for i := 0; i < n; i++ {
		reviewerID := uint(100 + i)
		go func() {
			defer wg.Done()
			<-start // 同时起跑
			if _, err := lossSvc.Approve(order.ID, reviewerID); err != nil {
				if errors.Is(err, util.ErrConflict) {
					atomic.AddInt32(&conflict, 1)
				} else {
					// SQLite 写锁竞争导致的数据库错误（锁忙等），不影响业务幂等结论。
					atomic.AddInt32(&other, 1)
				}
			} else {
				atomic.AddInt32(&success, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if success != 1 {
		t.Fatalf("concurrent approve success = %d (conflict=%d other=%d), want exactly 1",
			success, conflict, other)
	}

	// 库存恰好扣减一次。
	var inv model.StoreInventory
	if err := db.Where("store_id = ? AND sku_id = ?", store.ID, sku.ID).First(&inv).Error; err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	if inv.Quantity != initialQty-lossQty {
		t.Fatalf("quantity = %d, want %d (deducted exactly once)", inv.Quantity, initialQty-lossQty)
	}

	// 损耗出库记录恰好一条。
	_, total, err := recordRepo.List(1, 100, store.ID, sku.ID, constants.RecordLoss)
	if err != nil {
		t.Fatalf("list records: %v", err)
	}
	if total != 1 {
		t.Fatalf("loss records total = %d, want 1", total)
	}

	final, err := lossSvc.GetByID(order.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if final.Status != constants.LossApproved || final.ReviewerID == nil {
		t.Fatalf("final order status=%s reviewer=%v, want approved with reviewer", final.Status, final.ReviewerID)
	}
}

// 并发混合通过/驳回：同一单据同时被通过和驳回，最终只能落到一个终态，库存变动与之一致。
func TestLossOrderConcurrentMixedReviewOneOutcome(t *testing.T) {
	db := newLossConcurrencyDB(t)
	lossSvc, _, _ := newLossService(db)

	store := &model.Store{Code: "C2", Name: "混合门店"}
	sku := &model.SKU{Code: "CP2", Name: "混合商品", Unit: "件"}
	db.Create(store)
	db.Create(sku)
	const initialQty = 50
	const lossQty = 3
	db.Create(&model.StoreInventory{StoreID: store.ID, SKUID: sku.ID, Quantity: initialQty})
	order, err := lossSvc.Create(store.ID, sku.ID, lossQty, "过期", 1)
	if err != nil {
		t.Fatalf("create loss: %v", err)
	}

	const each = 16
	var (
		wg      sync.WaitGroup
		start   = make(chan struct{})
		success int32
	)
	act := func(approve bool) {
		defer wg.Done()
		<-start
		var err error
		if approve {
			_, err = lossSvc.Approve(order.ID, 200)
		} else {
			_, err = lossSvc.Reject(order.ID, 200, "驳回原因")
		}
		if err == nil {
			atomic.AddInt32(&success, 1)
		}
	}
	wg.Add(each * 2)
	for i := 0; i < each; i++ {
		go act(true)
		go act(false)
	}
	close(start)
	wg.Wait()

	if success != 1 {
		t.Fatalf("mixed review success = %d, want exactly 1", success)
	}

	final, err := lossSvc.GetByID(order.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	var inv model.StoreInventory
	db.Where("store_id = ? AND sku_id = ?", store.ID, sku.ID).First(&inv)
	switch final.Status {
	case constants.LossApproved:
		if inv.Quantity != initialQty-lossQty {
			t.Fatalf("approved but quantity = %d, want %d", inv.Quantity, initialQty-lossQty)
		}
		if final.RejectReason != "" {
			t.Fatalf("approved order should not carry reject reason")
		}
	case constants.LossRejected:
		if inv.Quantity != initialQty {
			t.Fatalf("rejected but quantity = %d, want unchanged %d", inv.Quantity, initialQty)
		}
		if final.RejectReason == "" {
			t.Fatalf("rejected order must carry reject reason")
		}
	default:
		t.Fatalf("final status = %s, want a terminal status", final.Status)
	}
}
