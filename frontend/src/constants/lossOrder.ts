// LossOrderStatus 报损单状态枚举（与 backend/internal/constants/loss_order.go 对应）
export const LossOrderStatus = {
  PENDING: 'pending',
  APPROVED: 'approved',
  REJECTED: 'rejected'
} as const

export type LossOrderStatusValue = (typeof LossOrderStatus)[keyof typeof LossOrderStatus]

export const LOSS_ORDER_STATUS_OPTIONS: { label: string; value: LossOrderStatusValue }[] = [
  { label: '待审核', value: LossOrderStatus.PENDING },
  { label: '已通过', value: LossOrderStatus.APPROVED },
  { label: '已驳回', value: LossOrderStatus.REJECTED }
]

export const LOSS_ORDER_STATUS_TEXT: Record<LossOrderStatusValue, string> = {
  [LossOrderStatus.PENDING]: '待审核',
  [LossOrderStatus.APPROVED]: '已通过',
  [LossOrderStatus.REJECTED]: '已驳回'
}

export const LOSS_ORDER_STATUS_TAG: Record<LossOrderStatusValue, string> = {
  [LossOrderStatus.PENDING]: 'warning',
  [LossOrderStatus.APPROVED]: 'success',
  [LossOrderStatus.REJECTED]: 'danger'
}

// 状态机：允许的流转，与后端 CanLossTransition 对应（每张单据只能处理一次，终态不可再流转）
export const LOSS_ORDER_STATUS_FLOW: Record<LossOrderStatusValue, LossOrderStatusValue[]> = {
  [LossOrderStatus.PENDING]: [LossOrderStatus.APPROVED, LossOrderStatus.REJECTED],
  [LossOrderStatus.APPROVED]: [],
  [LossOrderStatus.REJECTED]: []
}

export function canLossTransition(from: LossOrderStatusValue, to: LossOrderStatusValue): boolean {
  return LOSS_ORDER_STATUS_FLOW[from]?.includes(to) ?? false
}
