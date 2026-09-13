import request from '@/utils/request'
import type { LossOrder, PageResult } from '@/types'
import type { LossOrderStatusValue } from '@/constants/lossOrder'

export function listLossOrders(params: { page?: number; page_size?: number; store_id?: number; status?: LossOrderStatusValue } = {}): Promise<PageResult<LossOrder>> {
  return request.get('/loss-orders', { params })
}

export function getLossOrder(id: number): Promise<LossOrder> {
  return request.get(`/loss-orders/${id}`)
}

// 门店由后端依据登录店长绑定门店确定，前端无需也不能指定 store_id。
export function createLossOrder(data: { sku_id: number; quantity: number; reason: string }): Promise<LossOrder> {
  return request.post('/loss-orders', data)
}

export function approveLossOrder(id: number): Promise<LossOrder> {
  return request.put(`/loss-orders/${id}/approve`)
}

export function rejectLossOrder(id: number, reject_reason: string): Promise<LossOrder> {
  return request.put(`/loss-orders/${id}/reject`, { reject_reason })
}
