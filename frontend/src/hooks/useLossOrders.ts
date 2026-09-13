import { ref } from 'vue'
import { listLossOrders, approveLossOrder, rejectLossOrder } from '@/api/lossOrder'
import type { LossOrder } from '@/types'
import type { LossOrderStatusValue } from '@/constants/lossOrder'

// useLossOrders：报损单列表与审批状态管理
export function useLossOrders() {
  const list = ref<LossOrder[]>([])
  const total = ref(0)
  const loading = ref(false)
  const page = ref(1)
  const pageSize = ref(10)
  const status = ref<LossOrderStatusValue | ''>('')
  const storeId = ref<number | undefined>(undefined)

  async function load() {
    loading.value = true
    try {
      const params: { page: number; page_size: number; status?: LossOrderStatusValue; store_id?: number } = {
        page: page.value,
        page_size: pageSize.value
      }
      if (status.value) params.status = status.value
      if (storeId.value) params.store_id = storeId.value
      const res = await listLossOrders(params)
      list.value = res.list
      total.value = res.total
    } finally {
      loading.value = false
    }
  }

  async function approve(id: number) {
    await approveLossOrder(id)
    await load()
  }

  async function reject(id: number, reason: string) {
    await rejectLossOrder(id, reason)
    await load()
  }

  return { list, total, loading, page, pageSize, status, storeId, load, approve, reject }
}
