import request from '@/utils/request'
import type { LossOrder, PageResult } from '@/types'
import type { LossOrderStatusValue } from '@/constants/lossOrder'

export function listLossOrders(params: { page?: number; page_size?: number; store_id?: number; status?: LossOrderStatusValue } = {}): Promise<PageResult<LossOrder>> {
  return request.get('/loss-orders', { params })
}

export function getLossOrder(id: number): Promise<LossOrder> {
  return request.get(`/loss-orders/${id}`)
}

export function createLossOrder(data: { store_id: number; sku_id: number; quantity: number; reason: string }): Promise<LossOrder> {
  return request.post('/loss-orders', data)
}

export function approveLossOrder(id: number): Promise<LossOrder> {
  return request.put(`/loss-orders/${id}/approve`)
}

export function rejectLossOrder(id: number, reject_reason: string): Promise<LossOrder> {
  return request.put(`/loss-orders/${id}/reject`, { reject_reason })
}
