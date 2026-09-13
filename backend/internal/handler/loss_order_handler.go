package handler

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ld/storeinventory/internal/constants"
	"github.com/ld/storeinventory/internal/dto"
	"github.com/ld/storeinventory/internal/middleware"
	"github.com/ld/storeinventory/internal/service"
	"github.com/ld/storeinventory/internal/util"
)

// LossOrderHandler 库存报损单接口处理器。
type LossOrderHandler struct {
	lossSvc service.LossOrderService
}

// NewLossOrderHandler 构造报损单处理器。
func NewLossOrderHandler(lossSvc service.LossOrderService) *LossOrderHandler {
	return &LossOrderHandler{lossSvc: lossSvc}
}

// Create 店长提交本店商品的报损数量和原因，单据进入待审核。
func (h *LossOrderHandler) Create(c *gin.Context) {
	claims, err := middleware.CurrentUser(c)
	if err != nil {
		c.Error(util.Unauthorized(constants.MsgUnauthorized, err))
		return
	}
	var req dto.LossOrderCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(util.Validation(constants.MsgInvalidRequest, err))
		return
	}
	// 店长只能提交本店的报损单；总部/管理员可代任意门店提交。
	storeID := req.StoreID
	if claims.Role == constants.RoleStoreManager {
		if claims.StoreID == nil || *claims.StoreID != storeID {
			c.Error(util.Forbidden(constants.MsgLossStoreMismatch,
				fmt.Errorf("role[%s] create loss for store[%d], own store[%v]", claims.Role, storeID, claims.StoreID)))
			return
		}
	}
	order, err := h.lossSvc.Create(storeID, req.SKUID, req.Quantity, req.Reason, claims.UserID)
	if err != nil {
		c.Error(fmt.Errorf("handler create loss order role[%s]: %w", claims.Role, err))
		return
	}
	util.OKMessage(c, constants.MsgLossCreateSuccess, order)
}

// List 报损单列表：店长只看本店，总部/管理员看全部门店（可按门店筛选）。
func (h *LossOrderHandler) List(c *gin.Context) {
	claims, err := middleware.CurrentUser(c)
	if err != nil {
		c.Error(util.Unauthorized(constants.MsgUnauthorized, err))
		return
	}
	var q dto.LossOrderListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		c.Error(util.Validation(constants.MsgInvalidRequest, err))
		return
	}
	q.Normalize()
	storeID := uint(0)
	if claims.Role == constants.RoleStoreManager {
		if claims.StoreID == nil {
			c.Error(util.Forbidden(constants.MsgLossStoreMismatch, fmt.Errorf("role[%s] has no bound store", claims.Role)))
			return
		}
		storeID = *claims.StoreID
	} else {
		sid, _ := strconv.ParseUint(c.Query("store_id"), 10, 64)
		storeID = uint(sid)
	}
	orders, total, err := h.lossSvc.List(q.Page, q.PageSize, storeID, q.Status)
	if err != nil {
		c.Error(fmt.Errorf("handler list loss orders role[%s]: %w", claims.Role, err))
		return
	}
	util.OK(c, gin.H{"list": orders, "total": total, "page": q.Page, "page_size": q.PageSize})
}

// Get 报损单详情：店长仅能查看本店单据。
func (h *LossOrderHandler) Get(c *gin.Context) {
	claims, err := middleware.CurrentUser(c)
	if err != nil {
		c.Error(util.Unauthorized(constants.MsgUnauthorized, err))
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.Error(util.Validation("无效的报损单ID", err))
		return
	}
	order, err := h.lossSvc.GetByID(uint(id))
	if err != nil {
		c.Error(fmt.Errorf("handler get loss order[id=%d]: %w", id, err))
		return
	}
	if claims.Role == constants.RoleStoreManager && (claims.StoreID == nil || *claims.StoreID != order.StoreID) {
		c.Error(util.Forbidden(constants.MsgLossStoreMismatch,
			fmt.Errorf("role[%s] get loss order store[%d]", claims.Role, order.StoreID)))
		return
	}
	util.OK(c, order)
}

// Approve 总部/管理员审批通过，扣减库存并生成损耗出库记录。
func (h *LossOrderHandler) Approve(c *gin.Context) {
	h.review(c, true)
}

// Reject 总部/管理员驳回，必须填写驳回原因，库存不变。
func (h *LossOrderHandler) Reject(c *gin.Context) {
	h.review(c, false)
}

func (h *LossOrderHandler) review(c *gin.Context, approve bool) {
	claims, err := middleware.CurrentUser(c)
	if err != nil {
		c.Error(util.Unauthorized(constants.MsgUnauthorized, err))
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.Error(util.Validation("无效的报损单ID", err))
		return
	}
	if approve {
		order, err := h.lossSvc.Approve(uint(id), claims.UserID)
		if err != nil {
			c.Error(fmt.Errorf("handler approve loss order[id=%d] reviewer[%s]: %w", id, claims.Username, err))
			return
		}
		util.OKMessage(c, constants.MsgLossApproveSuccess, order)
		return
	}
	var req dto.LossOrderRejectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(util.Validation(constants.MsgLossRejectReasonNeeded, err))
		return
	}
	order, err := h.lossSvc.Reject(uint(id), claims.UserID, req.RejectReason)
	if err != nil {
		c.Error(fmt.Errorf("handler reject loss order[id=%d] reviewer[%s]: %w", id, claims.Username, err))
		return
	}
	util.OKMessage(c, constants.MsgLossRejectSuccess, order)
}
