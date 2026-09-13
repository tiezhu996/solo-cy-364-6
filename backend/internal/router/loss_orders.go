package router

import (
	"github.com/gin-gonic/gin"

	"github.com/ld/storeinventory/internal/constants"
	"github.com/ld/storeinventory/internal/handler"
	"github.com/ld/storeinventory/internal/middleware"
)

// registerLossOrderRoutes 库存报损单路由：仅店长可提交本店单据，总部/管理员仅负责审批。
func registerLossOrderRoutes(v1 *gin.RouterGroup, h *handler.LossOrderHandler, auth gin.HandlerFunc, limiter gin.HandlerFunc) {
	losses := v1.Group("/loss-orders", auth)
	losses.GET("", h.List)
	losses.GET("/:id", h.Get)
	// 提交入口仅对店长开放（且只能提交本店单据，handler 内再次校验）。
	losses.POST("", middleware.RequireRole(constants.RoleStoreManager), limiter, h.Create)
	// 每张单据仅总部/管理员可处理一次（通过/驳回）。
	losses.PUT("/:id/approve", middleware.RequireRole(constants.RoleAdmin, constants.RoleHQ), h.Approve)
	losses.PUT("/:id/reject", middleware.RequireRole(constants.RoleAdmin, constants.RoleHQ), h.Reject)
}
