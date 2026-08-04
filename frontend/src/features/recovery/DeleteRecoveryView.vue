<script lang="ts" setup>
import { onMounted } from 'vue'
import { useRouter, RouterLink } from 'vue-router'
import { useDeletionStore } from '../../stores/deletion'

const props = defineProps<{ id: string }>()
const deletion = useDeletionStore()
const router = useRouter()
onMounted(() => void deletion.loadJob(props.id))

/** retry 仅重试原 manifest 的持久剩余项。 */
async function retry(): Promise<void> {
  if (!(await deletion.retry())) return
  if (deletion.job?.state === 'completed')
    await router.replace(
      deletion.job.kind === 'meeting' ? '/meetings' : `/meetings/${props.id}`,
    )
}
</script>

<template>
  <section class="ms-page-head">
    <div>
      <p class="ms-eyebrow">安全删除恢复</p>
      <h1>删除尚未全部完成</h1>
      <p>已删除的项目不会回滚；重试只处理原清单中尚未完成的项目。</p>
    </div>
  </section>
  <p
    v-if="deletion.errorMessage"
    class="ms-notice ms-notice--danger"
    role="alert"
  >
    {{ deletion.errorMessage }}
  </p>
  <section v-if="deletion.job" class="ms-card ms-settings-card">
    <div class="ms-card-head">
      <h2>{{ deletion.job.kind === 'meeting' ? '整场会议' : '本地录音' }}</h2>
      <span class="ms-status-pill is-danger">{{ deletion.job.state }}</span>
    </div>
    <p>
      已尝试 {{ deletion.job.attempt_count }} 次，剩余
      {{ deletion.job.remaining.length }} 项。
    </p>
    <ul class="ms-operation-list">
      <li
        v-for="item in deletion.job.remaining"
        :key="item.item_id"
        class="ms-operation-step is-failed"
      >
        <span>{{ item.safe_name }}</span
        ><code>{{ item.code }}</code>
      </li>
    </ul>
    <div class="ms-actions">
      <button
        class="ms-button ms-button--primary"
        :disabled="deletion.loading"
        @click="retry"
      >
        重试剩余项目</button
      ><RouterLink
        class="ms-button ms-button--quiet"
        :to="`/meetings/${props.id}`"
        >返回会议详情</RouterLink
      >
    </div>
  </section>
</template>
