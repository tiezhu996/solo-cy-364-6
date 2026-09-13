package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/ld/storeinventory/internal/config"
	"github.com/ld/storeinventory/internal/constants"
	"github.com/ld/storeinventory/internal/handler"
	"github.com/ld/storeinventory/internal/middleware"
	"github.com/ld/storeinventory/internal/model"
	"github.com/ld/storeinventory/internal/repository"
	"github.com/ld/storeinventory/internal/service"
	"github.com/ld/storeinventory/internal/util"
)

var lossHTTPDBCounter int64

func setupLossHTTP(t *testing.T) (*gin.Engine, *gorm.DB, map[string]string, uint, uint) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:loss_http_%d?mode=memory&cache=shared", atomic.AddInt64(&lossHTTPDBCounter, 1))
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

	st1 := model.Store{Code: "S1", Name: "门店一"}
	st2 := model.Store{Code: "S2", Name: "门店二"}
	db.Create(&st1)
	db.Create(&st2)
	sku := model.SKU{Code: "P1", Name: "商品", Unit: "件"}
	db.Create(&sku)
	mgr := model.User{Username: "mgr", PasswordHash: "x", Name: "店长", Role: constants.RoleStoreManager, StoreID: &st1.ID}
	adm := model.User{Username: "adm", PasswordHash: "x", Name: "管理员", Role: constants.RoleAdmin}
	db.Create(&mgr)
	db.Create(&adm)
	db.Create(&model.StoreInventory{StoreID: st1.ID, SKUID: sku.ID, Quantity: 5})
	db.Create(&model.StoreInventory{StoreID: st2.ID, SKUID: sku.ID, Quantity: 100})
	db.Create(&model.LossOrder{StoreID: st1.ID, SKUID: sku.ID, Quantity: 1, Reason: "破损", Status: constants.LossPending, ApplicantID: &mgr.ID})
	db.Create(&model.LossOrder{StoreID: st2.ID, SKUID: sku.ID, Quantity: 1, Reason: "过期", Status: constants.LossPending, ApplicantID: &mgr.ID})

	logger := util.GetLogger()
	invRepo := repository.NewStoreInventoryRepository(db)
	skuRepo := repository.NewSKURepository(db)
	recordRepo := repository.NewStockRecordRepository(db)
	storeRepo := repository.NewStoreRepository(db)
	invSvc := service.NewStoreInventoryService(invRepo, skuRepo, db, logger)
	recSvc := service.NewStockRecordService(recordRepo, invRepo, storeRepo, skuRepo, invSvc, db, logger)
	lossSvc := service.NewLossOrderService(repository.NewLossOrderRepository(db), invSvc, recSvc, db, logger)
	lossHandler := handler.NewLossOrderHandler(lossSvc)

	cfg := &config.Config{JWTSecret: "test-secret", RateLimit: 100000, RateWindowSec: 60}
	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	v1 := engine.Group("/api/v1")
	registerLossOrderRoutes(v1, lossHandler, middleware.AuthRequired(cfg), middleware.RateLimit(cfg))

	tokens := map[string]string{}
	for name, uid := range map[string]uint{"mgr": mgr.ID, "adm": adm.ID} {
		u := model.User{}
		db.First(&u, uid)
		tok, err := util.GenerateToken(cfg.JWTSecret, 2, u.ID, u.Username, u.Role, u.StoreID)
		if err != nil {
			t.Fatalf("gen token: %v", err)
		}
		tokens[name] = tok
	}
	return engine, db, tokens, st1.ID, st2.ID
}

func doLossRequest(t *testing.T, engine *gin.Engine, method, path, token string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

func lossRespList(t *testing.T, w *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var env struct {
		Code int `json:"code"`
		Data struct {
			List []map[string]any `json:"list"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if env.Code != 0 {
		t.Fatalf("business code=%d message=%s", env.Code, env.Message)
	}
	return env.Data.List
}

// 店长列表只返回本店单据。
func TestLossHTTPManagerListScopedToOwnStore(t *testing.T) {
	engine, _, tokens, _, _ := setupLossHTTP(t)
	w := doLossRequest(t, engine, http.MethodGet, "/api/v1/loss-orders", tokens["mgr"], nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	list := lossRespList(t, w)
	if len(list) != 1 {
		t.Fatalf("manager sees %d orders, want 1", len(list))
	}
}

// 管理员列表可见全部门店。
func TestLossHTTPAdminListSeesAllStores(t *testing.T) {
	engine, _, tokens, _, _ := setupLossHTTP(t)
	w := doLossRequest(t, engine, http.MethodGet, "/api/v1/loss-orders", tokens["adm"], nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if list := lossRespList(t, w); len(list) != 2 {
		t.Fatalf("admin sees %d orders, want 2", len(list))
	}
}

// 店长不能为其他门店提交报损单。
func TestLossHTTPManagerCannotCreateForOtherStore(t *testing.T) {
	engine, _, tokens, _, st2 := setupLossHTTP(t)
	w := doLossRequest(t, engine, http.MethodPost, "/api/v1/loss-orders", tokens["mgr"], map[string]any{
		"store_id": st2, "sku_id": 1, "quantity": 1, "reason": "破损",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-store create status=%d want 403 body=%s", w.Code, w.Body.String())
	}
}

// 店长可以为自己门店提交报损单（无需传 store_id，后端按登录态归属）。
func TestLossHTTPManagerCreateOwnStore(t *testing.T) {
	engine, _, tokens, st1, _ := setupLossHTTP(t)
	w := doLossRequest(t, engine, http.MethodPost, "/api/v1/loss-orders", tokens["mgr"], map[string]any{
		"sku_id": 1, "quantity": 1, "reason": "临期破损",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("own-store create status=%d want 200 body=%s", w.Code, w.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int(env.Data["store_id"].(float64)) != int(st1) {
		t.Fatalf("created order store_id = %v, want %d", env.Data["store_id"], st1)
	}
}

// 总部/管理员不能提交报损单，仅可审批。
func TestLossHTTPAdminCannotCreate(t *testing.T) {
	engine, _, tokens, st1, _ := setupLossHTTP(t)
	w := doLossRequest(t, engine, http.MethodPost, "/api/v1/loss-orders", tokens["adm"], map[string]any{
		"store_id": st1, "sku_id": 1, "quantity": 1, "reason": "破损",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("admin create status=%d want 403 body=%s", w.Code, w.Body.String())
	}
}

// 店长无审批权限。
func TestLossHTTPManagerCannotApprove(t *testing.T) {
	engine, _, tokens, _, _ := setupLossHTTP(t)
	w := doLossRequest(t, engine, http.MethodPut, "/api/v1/loss-orders/1/approve", tokens["mgr"], nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("manager approve status=%d want 403", w.Code)
	}
}

// 管理员审批通过扣减库存；驳回必须带原因。
func TestLossHTTPAdminApproveAndReject(t *testing.T) {
	engine, db, tokens, st1, _ := setupLossHTTP(t)

	// 通过门店一的单据（库存 5 -> 4）。
	w := doLossRequest(t, engine, http.MethodPut, "/api/v1/loss-orders/1/approve", tokens["adm"], nil)
	if w.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", w.Code, w.Body.String())
	}
	var inv model.StoreInventory
	db.Where("store_id = ? AND sku_id = ?", st1, 1).First(&inv)
	if inv.Quantity != 4 {
		t.Fatalf("quantity after approve = %d, want 4", inv.Quantity)
	}
	// 重复审批冲突（每张单据只能处理一次）。
	w = doLossRequest(t, engine, http.MethodPut, "/api/v1/loss-orders/1/approve", tokens["adm"], nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("re-approve status=%d want 409", w.Code)
	}
	// 驳回门店二单据，缺原因 -> 422。
	w = doLossRequest(t, engine, http.MethodPut, "/api/v1/loss-orders/2/reject", tokens["adm"], map[string]any{"reject_reason": ""})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("reject blank status=%d want 422", w.Code)
	}
	// 带原因驳回成功。
	w = doLossRequest(t, engine, http.MethodPut, "/api/v1/loss-orders/2/reject", tokens["adm"], map[string]any{"reject_reason": "依据不足"})
	if w.Code != http.StatusOK {
		t.Fatalf("reject status=%d body=%s", w.Code, w.Body.String())
	}
}
