<template>
  <el-tag :type="tagType" size="small">{{ LOSS_ORDER_STATUS_TEXT[status] || status }}</el-tag>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { LOSS_ORDER_STATUS_TAG, LOSS_ORDER_STATUS_TEXT, type LossOrderStatusValue } from '@/constants/lossOrder'

const props = defineProps<{ status: LossOrderStatusValue }>()

const tagType = computed(() => LOSS_ORDER_STATUS_TAG[props.status] || 'info')
</script>
