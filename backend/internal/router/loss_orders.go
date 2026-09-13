package router

import (
	"github.com/gin-gonic/gin"

	"github.com/ld/storeinventory/internal/constants"
	"github.com/ld/storeinventory/internal/handler"
	"github.com/ld/storeinventory/internal/middleware"
)

// registerLossOrderRoutes 库存报损单路由：店长提交本店单据，总部/管理员审批。
func registerLossOrderRoutes(v1 *gin.RouterGroup, h *handler.LossOrderHandler, auth gin.HandlerFunc, managerRoles []constants.UserRole, limiter gin.HandlerFunc) {
	losses := v1.Group("/loss-orders", auth)
	losses.GET("", h.List)
	losses.GET("/:id", h.Get)
	losses.POST("", middleware.RequireRole(managerRoles...), limiter, h.Create)
	// 每张单据仅总部/管理员可处理一次（通过/驳回）。
	losses.PUT("/:id/approve", middleware.RequireRole(constants.RoleAdmin, constants.RoleHQ), h.Approve)
	losses.PUT("/:id/reject", middleware.RequireRole(constants.RoleAdmin, constants.RoleHQ), h.Reject)
}
