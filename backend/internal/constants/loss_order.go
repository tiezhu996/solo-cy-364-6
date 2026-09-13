package constants

// LossOrderStatus 报损单状态枚举，前后端共享定义（frontend/src/constants/lossOrder.ts 对应实现）。
type LossOrderStatus string

const (
	LossPending  LossOrderStatus = "pending"  // 待审核
	LossApproved LossOrderStatus = "approved" // 已通过
	LossRejected LossOrderStatus = "rejected" // 已驳回
)

func (s LossOrderStatus) Valid() bool {
	switch s {
	case LossPending, LossApproved, LossRejected:
		return true
	}
	return false
}

// LossOrderStatusFlow 定义报损单允许的合法状态流转。
// 待审核可被通过/驳回；已通过、已驳回为终态，每张单据只能处理一次（状态机同时耦合于 service、前端按钮、日志模板、错误码、formatters）。
var LossOrderStatusFlow = map[LossOrderStatus][]LossOrderStatus{
	LossPending:  {LossApproved, LossRejected},
	LossApproved: {},
	LossRejected: {},
}

// CanLossTransition 判断从 from 能否流转到 to。
func CanLossTransition(from, to LossOrderStatus) bool {
	for _, next := range LossOrderStatusFlow[from] {
		if next == to {
			return true
		}
	}
	return false
}
