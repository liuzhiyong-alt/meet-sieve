<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { EventsOn } from '../../../wailsjs/runtime/runtime'

import { useGapStore } from '../../stores/gap'
import { useMinutesStore } from '../../stores/minutes'
import type { Step8EventEnvelope } from '../../stores/finalization'
import { dirtyEditRegistry } from '../../router/dirty'

const props = defineProps<{ meetingId: string; meetingNo: string }>()
const emit = defineEmits<{ back: []; history: [] }>()
const minutes = useMinutesStore()
const gaps = useGapStore()
const tab = ref<'edit' | 'preview'>('edit')
let stopListener: (() => void) | undefined
let unregisterDirty: (() => void) | undefined

const current = computed(() => minutes.projection.current)
const generationBlocked = computed(() => gaps.state === 'processing')
const currentLabel = computed(() => {
  if (!current.value) return '尚未生成'
  const source =
    current.value.source === 'ai'
      ? 'AI 草稿'
      : current.value.source === 'restored'
        ? '恢复版本'
        : '人工草稿'
  return `${source} v${current.value.version_no}`
})

onMounted(async () => {
  await Promise.all([
    minutes.refresh(props.meetingId),
    minutes.loadHistory(props.meetingId),
    gaps.refresh(props.meetingId),
  ])
  stopListener = EventsOn(
    'meeting.minutes.changed',
    (event: Step8EventEnvelope) => {
      if (minutes.applyEvent(event)) void minutes.refresh(props.meetingId)
    },
  )
  unregisterDirty = dirtyEditRegistry.register({
    id: `minutes-${props.meetingId}`,
    label: '会议纪要',
    isDirty: () => minutes.dirty,
    canSave: () => Boolean(minutes.baseVersionID && minutes.draft.trim()),
    save: () => minutes.saveDraft(),
    discard: () => minutes.resetDraft(),
  })
})
onBeforeUnmount(() => {
  stopListener?.()
  unregisterDirty?.()
})

/** sourceLabel 映射版本来源。 */
function sourceLabel(source?: string): string {
  return source === 'ai'
    ? 'AI 生成'
    : source === 'restored'
      ? '历史恢复'
      : source === 'human'
        ? '人工修改'
        : '尚未生成'
}

/** formatTime 以本机时区展示版本时间。 */
function formatTime(value?: number): string {
  if (!value) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(new Date(value))
}
</script>

<template>
  <section class="ms-page-head">
    <div>
      <button class="ms-link-button" type="button" @click="emit('back')">
        返回会议详情
      </button>
      <p class="ms-eyebrow">{{ meetingNo }}</p>
      <h1>会议纪要版本</h1>
      <p class="ms-lead">
        编辑当前草稿、确认正式版本，或查看历史。重新生成不会覆盖人工修改。
      </p>
    </div>
    <span
      class="ms-status-pill"
      :class="current?.state === 'confirmed' ? 'is-complete' : 'is-warning'"
      >{{ currentLabel }}</span
    >
  </section>
  <p
    v-if="minutes.errorMessage"
    class="ms-notice ms-notice--danger"
    role="alert"
  >
    {{ minutes.errorMessage }}
  </p>
  <p v-if="minutes.notice" class="ms-notice ms-notice--info" aria-live="polite">
    {{ minutes.notice }}
  </p>
  <div
    v-if="gaps.state !== 'none' && gaps.state !== 'completed'"
    class="ms-notice ms-notice--warning"
  >
    <div>
      <strong>{{
        generationBlocked ? '补转写仍在处理' : '本版本会保留转写缺口提示'
      }}</strong>
      <p>
        {{
          generationBlocked
            ? '补转写状态仍在变化，暂时不能生成纪要。'
            : '失败或冲突范围不会被当作完整会议事实。'
        }}
      </p>
    </div>
  </div>

  <section v-if="!current" class="ms-card ms-step8-card ms-empty-minutes">
    <h2>尚未生成会议纪要</h2>
    <p class="ms-lead">
      生成只读取已保存的会议白名单事实，不会把 AI
      回答、推理或工具输出当作会议结论。
    </p>
    <button
      class="ms-button ms-button--primary"
      type="button"
      :disabled="generationBlocked || minutes.processing"
      @click="
        minutes.generate(
          meetingId,
          gaps.state !== 'none' && gaps.state !== 'completed',
        )
      "
    >
      {{ minutes.processing ? '正在生成…' : '生成会议纪要' }}
    </button>
    <button
      v-if="minutes.processing"
      class="ms-button ms-button--quiet"
      type="button"
      @click="minutes.stop()"
    >
      停止生成
    </button>
    <p v-if="generationBlocked" class="ms-help">
      等待补转写结束，或先停止补转写。
    </p>
  </section>

  <section v-else class="ms-minute-layout">
    <article class="ms-card ms-step8-card">
      <div class="ms-minute-toolbar">
        <div>
          <strong>当前版本 v{{ current.version_no }}</strong>
          <p class="ms-help">
            {{ sourceLabel(current.source) }} ·
            {{ formatTime(current.created_at) }}
          </p>
        </div>
        <div class="ms-tabs" role="tablist" aria-label="纪要查看方式">
          <button
            class="ms-tab"
            :class="{ 'is-active': tab === 'edit' }"
            type="button"
            role="tab"
            :aria-selected="tab === 'edit'"
            @click="tab = 'edit'"
          >
            编辑
          </button>
          <button
            class="ms-tab"
            :class="{ 'is-active': tab === 'preview' }"
            type="button"
            role="tab"
            :aria-selected="tab === 'preview'"
            @click="tab = 'preview'"
          >
            预览
          </button>
        </div>
      </div>
      <label v-if="tab === 'edit'" class="ms-field"
        >纪要内容<textarea
          class="ms-input ms-textarea ms-minute-editor"
          :value="minutes.draft"
          @input="
            minutes.setDraft(($event.target as HTMLTextAreaElement).value)
          "
        />
      </label>
      <pre v-else class="ms-minute-preview" tabindex="0">{{
        minutes.draft
      }}</pre>
      <p class="ms-help">
        保存修改会创建新的人工草稿版本，旧版本仍可查看和恢复。
      </p>
      <div class="ms-actions">
        <button
          class="ms-button ms-button--primary"
          type="button"
          :disabled="!minutes.dirty || minutes.saving"
          @click="minutes.saveDraft()"
        >
          {{ minutes.saving ? '正在保存…' : '保存修改' }}</button
        ><button
          class="ms-button ms-button--quiet"
          type="button"
          :disabled="!minutes.dirty"
          @click="minutes.resetDraft()"
        >
          放弃未保存修改
        </button>
      </div>
    </article>
    <aside class="ms-minute-sidebar">
      <article class="ms-card ms-step8-card">
        <div class="ms-card-head">
          <h2>当前版本</h2>
          <span
            class="ms-status-pill"
            :class="
              minutes.dirty
                ? 'is-warning'
                : current.state === 'confirmed'
                  ? 'is-complete'
                  : ''
            "
            >{{
              minutes.dirty
                ? '有未保存修改'
                : current.state === 'confirmed'
                  ? '已确认'
                  : '草稿'
            }}</span
          >
        </div>
        <ul class="ms-status-list">
          <li>
            <span>来源</span><strong>{{ sourceLabel(current.source) }}</strong>
          </li>
          <li>
            <span>状态</span
            ><strong>{{
              current.state === 'confirmed' ? '已确认' : '草稿'
            }}</strong>
          </li>
          <li>
            <span>文件投影</span
            ><strong>{{
              minutes.projection.projection_state === 'current'
                ? '已刷新'
                : minutes.projection.projection_state === 'failed'
                  ? '刷新失败'
                  : '等待刷新'
            }}</strong>
          </li>
        </ul>
        <div class="ms-actions">
          <button
            class="ms-button ms-button--primary"
            type="button"
            :disabled="!minutes.canConfirm"
            aria-describedby="confirm-help"
            @click="minutes.confirm()"
          >
            确认当前版本</button
          ><button
            class="ms-button ms-button--quiet"
            type="button"
            :disabled="generationBlocked || minutes.processing"
            @click="
              minutes.generate(
                meetingId,
                gaps.state !== 'none' && gaps.state !== 'completed',
              )
            "
          >
            {{ minutes.processing ? '正在生成…' : '重新生成 AI 草稿' }}</button
          ><button
            v-if="minutes.processing"
            class="ms-button ms-button--quiet"
            type="button"
            @click="minutes.stop()"
          >
            停止生成
          </button>
        </div>
        <p id="confirm-help" class="ms-help">
          {{
            minutes.dirty
              ? '请先保存修改，再确认当前版本。'
              : '新的 AI 草稿只会按版本规则成为候选。'
          }}
        </p>
      </article>
      <article
        v-if="
          minutes.projection.latest_candidate &&
          minutes.projection.latest_candidate.id !== current.id
        "
        class="ms-card ms-step8-card"
      >
        <h2>新的 AI 候选</h2>
        <p class="ms-help">
          v{{ minutes.projection.latest_candidate.version_no }}
          已生成，但没有覆盖当前人工版本。
        </p>
      </article>
      <article class="ms-card ms-step8-card">
        <div class="ms-card-head">
          <h2>版本历史</h2>
          <button
            class="ms-button ms-button--quiet"
            type="button"
            @click="emit('history')"
          >
            查看全部
          </button>
        </div>
        <div class="ms-version-list">
          <div
            v-for="version in minutes.history.slice(0, 3)"
            :key="version.id"
            class="ms-version-item"
            :class="{ 'is-current': version.is_current }"
          >
            <div>
              <strong
                >v{{ version.version_no }} ·
                {{ sourceLabel(version.source) }}</strong
              >
              <p class="ms-help">
                {{ version.is_current ? '当前版本 · ' : ''
                }}{{ formatTime(version.created_at) }}
              </p>
            </div>
            <span
              class="ms-status-pill"
              :class="version.state === 'confirmed' ? 'is-complete' : ''"
              >{{
                version.state === 'confirmed'
                  ? '已确认'
                  : version.is_current
                    ? '当前'
                    : '候选'
              }}</span
            >
          </div>
        </div>
      </article>
    </aside>
  </section>
</template>
