<script lang="ts" setup>
defineProps<{
  hasPrevious: boolean
  hasNext: boolean
  pageNumber: number
  currentCount: number
  loading?: boolean
}>()

defineEmits<{
  previous: []
  next: []
}>()
</script>

<template>
  <nav class="ms-cursor-pagination" aria-label="列表分页">
    <button
      class="ms-button ms-button--quiet"
      type="button"
      :disabled="loading || !hasPrevious"
      @click="$emit('previous')"
    >
      上一页
    </button>
    <span class="ms-meta"
      >第 {{ pageNumber }} 页 · 当前 {{ currentCount }} 条</span
    >
    <button
      class="ms-button ms-button--quiet"
      type="button"
      :disabled="loading || !hasNext"
      @click="$emit('next')"
    >
      下一页
    </button>
  </nav>
</template>
