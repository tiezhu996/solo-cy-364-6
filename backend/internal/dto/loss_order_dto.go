package dto

import "github.com/ld/storeinventory/internal/constants"

// LossOrderCreateRequest 报损单创建请求。
type LossOrderCreateRequest struct {
	StoreID  uint   `json:"store_id" binding:"required"`
	SKUID    uint   `json:"sku_id" binding:"required"`
	Quantity int    `json:"quantity" binding:"required,min=1"`
	Reason   string `json:"reason" binding:"required,max=255"`
}

// LossOrderRejectRequest 报损单驳回请求，必须填写驳回原因。
type LossOrderRejectRequest struct {
	RejectReason string `json:"reject_reason" binding:"required,max=255"`
}

// LossOrderListQuery 报损单列表查询。
type LossOrderListQuery struct {
	ListQuery
	Status constants.LossOrderStatus `form:"status"`
}

// Normalize 归一化分页参数。
func (q *LossOrderListQuery) Normalize() {
	q.ListQuery.Normalize()
}
