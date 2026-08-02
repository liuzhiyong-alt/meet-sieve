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
      aria-labelledby="database-upgrade-title"
    >
      <h1 id="database-upgrade-title">正在升级本地数据…</h1>
      <AppNotice variant="info" title="正在安全处理数据库"
        >正在创建备份并验证升级结果，请勿退出 MeetSieve。</AppNotice
      >
      <div v-if="bootstrap.errorMessage" class="ms-stack">
        <AppNotice variant="danger" title="升级未完成">{{
          bootstrap.errorMessage
        }}</AppNotice>
        <div class="ms-actions">
          <BaseButton
            variant="primary"
            :busy="bootstrap.loading"
            @click="bootstrap.retryDatabaseUpgrade"
            >重试</BaseButton
          >
          <BaseButton @click="$emit('select')">重新选择目录</BaseButton>
          <BaseButton @click="quit">退出应用</BaseButton>
        </div>
      </div>
      <p v-else class="ms-progress-label" aria-live="polite">
        <span class="ms-spinner" aria-hidden="true" /> 正在升级本地数据
      </p>
    </section>
  </main>
</template>
