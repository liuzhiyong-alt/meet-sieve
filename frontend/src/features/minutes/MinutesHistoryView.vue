<script lang="ts" setup>
import { computed, onMounted, ref } from 'vue'

import { useMinutesStore } from '../../stores/minutes'

const props = defineProps<{ meetingId: string; meetingNo: string }>()
const emit = defineEmits<{ back: [] }>()
const minutes = useMinutesStore()
const selectedID = ref('')
const selected = computed(
  () =>
    minutes.history.find((item) => item.id === selectedID.value) ??
    minutes.history[0],
)

onMounted(async () => {
  await Promise.all([
    minutes.refresh(props.meetingId),
    minutes.loadHistory(props.meetingId),
  ])
  selectedID.value = minutes.history[0]?.id ?? ''
})

/** sourceLabel 映射不可变版本来源。 */
function sourceLabel(source?: string): string {
  return source === 'ai'
    ? 'AI 生成'
    : source === 'restored'
      ? '历史恢复'
      : '人工修改'
}

/** formatTime 以本机时区展示审计时间。 */
function formatTime(value?: number): string {
  if (!value) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    dateStyle: 'short',
    timeStyle: 'short',
  }).format(new Date(value))
}

/** restore 复制历史正文为新 current 后返回工作区。 */
async function restore(): Promise<void> {
  if (selected.value && (await minutes.restore(selected.value.id))) emit('back')
}
</script>

<template>
  <section class="ms-page-head">
    <div>
      <button class="ms-link-button" type="button" @click="emit('back')">
        返回当前纪要
      </button>
      <p class="ms-eyebrow">
        {{ selected ? `纪要版本 v${selected.version_no}` : meetingNo }}
      </p>
      <h1>查看历史版本</h1>
      <p class="ms-lead">
        历史内容保持只读。恢复会复制出新的当前版本，不会覆盖任何已有版本。
      </p>
    </div>
    <span
      v-if="selected"
      class="ms-status-pill"
      :class="selected.state === 'confirmed' ? 'is-complete' : ''"
      >{{ selected.state === 'confirmed' ? '已确认' : '草稿' }}</span
    >
  </section>
  <p
    v-if="minutes.errorMessage"
    class="ms-notice ms-notice--danger"
    role="alert"
  >
    {{ minutes.errorMessage }}
  </p>
  <section v-if="selected" class="ms-history-layout">
    <article class="ms-card ms-step8-card ms-history-preview">
      <pre>{{ selected.content_markdown }}</pre>
    </article>
    <aside class="ms-minute-sidebar">
      <article class="ms-card ms-step8-card">
        <div class="ms-card-head">
          <h2>版本信息</h2>
          <span
            class="ms-status-pill"
            :class="selected.state === 'confirmed' ? 'is-complete' : ''"
            >{{ selected.state === 'confirmed' ? '已确认' : '草稿' }}</span
          >
        </div>
        <ul class="ms-status-list">
          <li>
            <span>版本</span><strong>v{{ selected.version_no }}</strong>
          </li>
          <li>
            <span>来源</span><strong>{{ sourceLabel(selected.source) }}</strong>
          </li>
          <li>
            <span>生成时间</span
            ><strong>{{ formatTime(selected.created_at) }}</strong>
          </li>
          <li>
            <span>确认时间</span
            ><strong>{{ formatTime(selected.confirmed_at) }}</strong>
          </li>
        </ul>
        <div class="ms-notice ms-notice--info">
          恢复后会创建一个新版本并设为当前版本；当前和历史版本继续保留。
        </div>
        <button
          class="ms-button ms-button--primary"
          type="button"
          :disabled="selected.is_current"
          @click="restore"
        >
          恢复为新版本
        </button>
      </article>
      <article class="ms-card ms-step8-card">
        <h2>其他版本</h2>
        <div class="ms-version-list">
          <button
            v-for="version in minutes.history"
            :key="version.id"
            class="ms-version-item ms-version-button"
            :class="{
              'is-current': version.is_current,
              'is-selected': version.id === selected.id,
            }"
            type="button"
            @click="selectedID = version.id"
          >
            <span
              ><strong
                >v{{ version.version_no }} ·
                {{ sourceLabel(version.source) }}</strong
              ><small>{{
                version.is_current ? '当前版本' : formatTime(version.created_at)
              }}</small></span
            ><span class="ms-status-pill">查看</span>
          </button>
        </div>
      </article>
    </aside>
  </section>
  <section v-else class="ms-card ms-step8-card">
    <p>暂无纪要历史。</p>
    <button
      class="ms-button ms-button--quiet"
      type="button"
      @click="emit('back')"
    >
      返回当前纪要
    </button>
  </section>
</template>
