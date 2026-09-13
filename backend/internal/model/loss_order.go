package model

import (
	"time"

	"github.com/ld/storeinventory/internal/constants"
)

// LossOrder 库存报损单：店长提交本店商品的报损数量与原因，总部/管理员审批通过后扣减库存并生成损耗出库记录。
type LossOrder struct {
	ID           uint                      `gorm:"primaryKey" json:"id"`
	StoreID      uint                      `gorm:"index;not null" json:"store_id"`
	Store        *Store                    `gorm:"foreignKey:StoreID" json:"store,omitempty"`
	SKUID        uint                      `gorm:"column:sku_id;index;not null" json:"sku_id"`
	SKU          *SKU                      `gorm:"foreignKey:SKUID" json:"sku,omitempty"`
	Quantity     int                       `gorm:"not null" json:"quantity"`
	Reason       string                    `gorm:"size:255;not null" json:"reason"`
	Status       constants.LossOrderStatus `gorm:"size:16;index;not null;default:pending" json:"status"`
	ApplicantID  *uint                     `gorm:"index" json:"applicant_id"`
	Applicant    *User                     `gorm:"foreignKey:ApplicantID" json:"applicant,omitempty"`
	ReviewerID   *uint                     `gorm:"index" json:"reviewer_id"`
	Reviewer     *User                     `gorm:"foreignKey:ReviewerID" json:"reviewer,omitempty"`
	RejectReason string                    `gorm:"size:255" json:"reject_reason"`
	ReviewedAt   *time.Time                `json:"reviewed_at"`
	CreatedAt    time.Time                 `json:"created_at"`
	UpdatedAt    time.Time                 `json:"updated_at"`
}
