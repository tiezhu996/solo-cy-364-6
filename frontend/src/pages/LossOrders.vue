<template>
  <div>
    <el-card>
      <div class="toolbar">
        <el-select
          v-if="isApprover"
          v-model="storeId"
          placeholder="按门店筛选"
          clearable
          style="width: 200px"
          @change="load"
        >
          <el-option v-for="s in stores" :key="s.id" :label="s.name" :value="s.id" />
        </el-select>
        <el-select v-model="statusFilter" placeholder="状态筛选" clearable style="width: 150px" @change="load">
          <el-option v-for="s in LOSS_ORDER_STATUS_OPTIONS" :key="s.value" :label="s.label" :value="s.value" />
        </el-select>
        <el-button type="primary" @click="load">查询</el-button>
        <div class="spacer"></div>
        <el-button type="primary" @click="openCreate">提交报损</el-button>
      </div>

      <el-table :data="list" v-loading="loading" border stripe>
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column label="门店" min-width="140">
          <template #default="{ row }">{{ row.store?.name || `#${row.store_id}` }}</template>
        </el-table-column>
        <el-table-column label="商品" min-width="150">
          <template #default="{ row }">{{ row.sku?.name || `#${row.sku_id}` }}</template>
        </el-table-column>
        <el-table-column prop="quantity" label="报损数量" width="90" />
        <el-table-column prop="reason" label="报损原因" min-width="150" show-overflow-tooltip />
        <el-table-column label="状态" width="100">
          <template #default="{ row }"><LossOrderStatusBadge :status="row.status" /></template>
        </el-table-column>
        <el-table-column label="申请人" width="110">
          <template #default="{ row }">{{ row.applicant?.name || (row.applicant_id ? `#${row.applicant_id}` : '-') }}</template>
        </el-table-column>
        <el-table-column label="审核人" width="110">
          <template #default="{ row }">{{ row.reviewer?.name || (row.reviewer_id ? `#${row.reviewer_id}` : '-') }}</template>
        </el-table-column>
        <el-table-column label="驳回原因" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">{{ row.reject_reason || '-' }}</template>
        </el-table-column>
        <el-table-column label="提交时间" width="160">
          <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="170" fixed="right">
          <template #default="{ row }">
            <el-button
              v-if="isApprover && canApprove(row)"
              link
              type="success"
              size="small"
              @click="doApprove(row)"
            >通过</el-button>
            <el-button
              v-if="isApprover && canReject(row)"
              link
              type="danger"
              size="small"
              @click="openReject(row)"
            >驳回</el-button>
            <span v-if="!isApprover || !canApprove(row)" class="muted">-</span>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        class="mt"
        layout="total, prev, pager, next"
        :total="total"
        :page-size="pageSize"
        v-model:current-page="page"
        @current-change="load"
      />
    </el-card>

    <el-dialog v-model="createVisible" title="提交库存报损单" width="480px">
      <el-form ref="createFormRef" :model="form" :rules="rules" label-width="90px">
        <el-form-item label="门店" prop="store_id">
          <el-select v-model="form.store_id" :disabled="!isApprover" style="width: 100%">
            <el-option v-for="s in stores" :key="s.id" :label="s.name" :value="s.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="商品" prop="sku_id">
          <el-select v-model="form.sku_id" filterable style="width: 100%">
            <el-option v-for="s in skus" :key="s.id" :label="`${s.name}（${s.code}）`" :value="s.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="报损数量" prop="quantity">
          <el-input-number v-model="form.quantity" :min="1" />
        </el-form-item>
        <el-form-item label="报损原因" prop="reason">
          <el-input v-model="form.reason" type="textarea" :rows="3" maxlength="255" show-word-limit placeholder="请填写报损原因，如破损、过期、丢失等" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="onCreate">提交</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="rejectVisible" title="驳回报损单" width="440px">
      <el-form ref="rejectFormRef" :model="rejectForm" :rules="rejectRules" label-width="90px">
        <el-form-item label="驳回原因" prop="reject_reason">
          <el-input v-model="rejectForm.reject_reason" type="textarea" :rows="3" maxlength="255" show-word-limit placeholder="驳回必须填写原因" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="rejectVisible = false">取消</el-button>
        <el-button type="danger" :loading="saving" @click="onReject">确认驳回</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import LossOrderStatusBadge from '@/components/common/LossOrderStatusBadge.vue'
import { listLossOrders, createLossOrder, approveLossOrder, rejectLossOrder } from '@/api/lossOrder'
import { listAllStores } from '@/api/store'
import { listSkus } from '@/api/sku'
import { LOSS_ORDER_STATUS_OPTIONS, LossOrderStatus, canLossTransition, type LossOrderStatusValue } from '@/constants/lossOrder'
import { useAuthStore } from '@/stores/authStore'
import { formatDateTime } from '@/utils/dateFormat'
import type { Store, SKU, LossOrder } from '@/types'

const auth = useAuthStore()
const isApprover = computed(() => auth.role === 'admin' || auth.role === 'hq')
// 店长只能提交/查看本店单据
const ownStoreId = computed<number | null>(() => auth.user?.store_id ?? null)

const list = ref<LossOrder[]>([])
const stores = ref<Store[]>([])
const skus = ref<SKU[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const statusFilter = ref<LossOrderStatusValue | ''>('')
const storeId = ref<number | undefined>(undefined)
const loading = ref(false)
const saving = ref(false)
const createVisible = ref(false)
const rejectVisible = ref(false)
const createFormRef = ref<FormInstance>()
const rejectFormRef = ref<FormInstance>()

const form = reactive({ store_id: 0, sku_id: 0, quantity: 1, reason: '' })
const rejectForm = reactive({ id: 0, reject_reason: '' })

const rules: FormRules = {
  store_id: [{ required: true, message: '请选择门店', trigger: 'change' }],
  sku_id: [{ required: true, message: '请选择商品', trigger: 'change' }],
  quantity: [{ required: true, message: '报损数量需大于 0', trigger: 'change' }],
  reason: [{ required: true, message: '请填写报损原因', trigger: 'blur' }]
}
const rejectRules: FormRules = {
  reject_reason: [{ required: true, message: '驳回必须填写原因', trigger: 'blur' }]
}

async function load() {
  loading.value = true
  try {
    const params: { page: number; page_size: number; status?: LossOrderStatusValue; store_id?: number } = {
      page: page.value,
      page_size: pageSize.value
    }
    if (statusFilter.value) params.status = statusFilter.value
    // 店长后端强制按本店过滤，前端同样固定门店，避免越权传参。
    const filterStore = isApprover.value ? storeId.value : ownStoreId.value ?? undefined
    if (filterStore) params.store_id = filterStore
    const res = await listLossOrders(params)
    list.value = res.list
    total.value = res.total
  } finally {
    loading.value = false
  }
}

function openCreate() {
  form.store_id = isApprover.value ? (storeId.value || stores.value[0]?.id || 0) : (ownStoreId.value || 0)
  form.sku_id = skus.value[0]?.id || 0
  form.quantity = 1
  form.reason = ''
  createVisible.value = true
}

async function onCreate() {
  if (!createFormRef.value) return
  await createFormRef.value.validate(async (valid) => {
    if (!valid) return
    saving.value = true
    try {
      await createLossOrder({ store_id: form.store_id, sku_id: form.sku_id, quantity: form.quantity, reason: form.reason })
      ElMessage.success('报损单已提交，等待审核')
      createVisible.value = false
      await load()
    } finally {
      saving.value = false
    }
  })
}

function canApprove(row: LossOrder) {
  return canLossTransition(row.status, LossOrderStatus.APPROVED)
}
function canReject(row: LossOrder) {
  return canLossTransition(row.status, LossOrderStatus.REJECTED)
}

async function doApprove(row: LossOrder) {
  try {
    await ElMessageBox.confirm('确认通过该报损单？通过后将扣减对应库存并生成损耗出库记录。', '审批确认', {
      type: 'warning',
      confirmButtonText: '确认通过',
      cancelButtonText: '取消'
    })
  } catch {
    return
  }
  saving.value = true
  try {
    // 库存不足时后端返回失败，单据保持待审核。
    await approveLossOrder(row.id)
    ElMessage.success('报损单已通过，库存已扣减')
    await load()
  } finally {
    saving.value = false
  }
}

function openReject(row: LossOrder) {
  rejectForm.id = row.id
  rejectForm.reject_reason = ''
  rejectVisible.value = true
}

async function onReject() {
  if (!rejectFormRef.value) return
  await rejectFormRef.value.validate(async (valid) => {
    if (!valid) return
    saving.value = true
    try {
      await rejectLossOrder(rejectForm.id, rejectForm.reject_reason)
      ElMessage.success('报损单已驳回')
      rejectVisible.value = false
      await load()
    } finally {
      saving.value = false
    }
  })
}

onMounted(async () => {
  if (!auth.user) await auth.fetchMe()
  stores.value = await listAllStores()
  const res = await listSkus({ page: 1, page_size: 500 })
  skus.value = res.list
  await load()
})
</script>

<style scoped>
.toolbar { display: flex; gap: 10px; margin-bottom: 14px; align-items: center; }
.spacer { flex: 1; }
.mt { margin-top: 14px; }
.muted { color: #c0c4cc; }
</style>
