<script lang="ts" setup>
import BaseButton from '../../components/base/BaseButton.vue'
import AppNotice from '../../components/base/AppNotice.vue'
import { useBootstrapStore } from '../../stores/bootstrap'

defineEmits<{ select: [] }>()
const bootstrap = useBootstrapStore()

/** quit 使用平台窗口关闭能力结束当前应用。 */
function quit(): void {
  window.close()
}
</script>

<template>
  <main class="ms-blocking-view">
    <section
      class="ms-blocking-view__panel"
      aria-labelledby="workspace-unavailable-title"
    >
      <h1 id="workspace-unavailable-title">工作目录不可用</h1>
      <AppNotice variant="danger" title="无法继续打开本地数据">{{
        bootstrap.state.message || '请重新选择会议工作目录。'
      }}</AppNotice>
      <div class="ms-actions">
        <BaseButton variant="primary" @click="$emit('select')"
          >重新选择目录</BaseButton
        >
        <BaseButton @click="quit">退出应用</BaseButton>
      </div>
    </section>
  </main>
</template>
