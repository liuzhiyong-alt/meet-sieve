<script lang="ts" setup>
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { OpenResource } from '../../../../wailsjs/go/wails/ResourceBinding'

import SafeMarkdown from '../../../components/content/SafeMarkdown.vue'
import { useTimelineStore } from '../../../stores/timeline'
import type { TimelineEntry } from '../contentApi'

const props = defineProps<{
  subject: string
  agentPartial?: string
  agentAvailable: boolean
  transcriptionPaused: boolean
  ended: boolean
}>()
const emit = defineEmits<{ askAgent: []; interruptAgent: [] }>()
const timeline = useTimelineStore()
const draft = ref('')
const isComposerPointerFocused = ref(false)
const scrollElement = ref<HTMLElement | null>(null)
const followsLatest = ref(true)
const unreadCount = ref(0)
const previousLatestSeq = ref(0)

interface UtteranceGroup {
  kind: 'utterance-group'
  key: string
  speakerKey: string
  speakerLabel: string
  entries: TimelineEntry[]
}

type TimelineRow = TimelineEntry | UtteranceGroup

/** isUtteranceGroup 区分合并后的连续说话人组。 */
function isUtteranceGroup(row: TimelineRow): row is UtteranceGroup {
  return row.kind === 'utterance-group'
}

/** rowKey 返回 Vue 列表使用的稳定持久键。 */
function rowKey(row: TimelineRow): string | number {
  return isUtteranceGroup(row) ? row.key : row.seq
}

/** rowClass 返回事件行的来源视觉状态。 */
function rowClass(row: TimelineRow): Record<string, boolean> {
  if (isUtteranceGroup(row)) return {}
  return {
    'is-host-message': row.source === 'host',
    'is-agent': row.kind.startsWith('ai_'),
  }
}

const rows = computed<TimelineRow[]>(() => {
  const grouped: TimelineRow[] = []
  for (const entry of timeline.entries) {
    if (entry.kind !== 'utterance') {
      grouped.push(entry)
      continue
    }
    const speakerKey = entry.speaker_key || entry.speaker_label || 'unknown'
    const previous = grouped.at(-1)
    if (
      previous &&
      isUtteranceGroup(previous) &&
      previous.speakerKey === speakerKey
    ) {
      previous.entries.push(entry)
      continue
    }
    grouped.push({
      kind: 'utterance-group',
      key: `utterance-${entry.seq}`,
      speakerKey,
      speakerLabel: entry.speaker_label || '未知说话人',
      entries: [entry],
    })
  }
  return grouped
})

onMounted(async () => {
  await nextTick()
  scrollToLatest(false)
  previousLatestSeq.value = timeline.latestSeq
})

watch(
  () => timeline.latestSeq,
  async (latestSeq) => {
    const added = timeline.entries.filter(
      (entry) => entry.seq > previousLatestSeq.value,
    ).length
    previousLatestSeq.value = latestSeq
    if (!added) return
    if (followsLatest.value) {
      await nextTick()
      scrollToLatest(true)
    } else {
      unreadCount.value += added
    }
  },
)

watch(
  () => [
    timeline.orderedPartials.map((item) => item.text).join('\n'),
    props.agentPartial,
  ],
  async () => {
    if (!followsLatest.value) return
    await nextTick()
    scrollToLatest(false)
  },
)

/** handleScroll 只有用户处在底部阈值内时才继续自动跟随新消息。 */
function handleScroll(): void {
  const element = scrollElement.value
  if (!element) return
  followsLatest.value =
    element.scrollHeight - element.scrollTop - element.clientHeight <= 24
  if (followsLatest.value) unreadCount.value = 0
}

/** scrollToLatest 回到底部并恢复自动跟随。 */
function scrollToLatest(smooth: boolean): void {
  const element = scrollElement.value
  if (!element) return
  element.scrollTo({
    top: element.scrollHeight,
    behavior: smooth ? 'smooth' : 'auto',
  })
  followsLatest.value = true
  unreadCount.value = 0
}

/** loadOlder 保持当前可见锚点，避免旧消息插入后页面跳动。 */
async function loadOlder(): Promise<void> {
  const element = scrollElement.value
  if (!element) return
  const oldHeight = element.scrollHeight
  await timeline.loadOlder()
  await nextTick()
  element.scrollTop += element.scrollHeight - oldHeight
}

/** submitMessage 发送成功才清空草稿。 */
async function submitMessage(): Promise<void> {
  if (await timeline.sendText(draft.value)) draft.value = ''
}

/** handleComposerKey 实现 Enter 发送、Shift+Enter 换行。 */
function handleComposerKey(event: KeyboardEvent): void {
  if (event.key !== 'Enter' || event.shiftKey || event.isComposing) return
  event.preventDefault()
  void submitMessage()
}

/** handleComposerPointerDown 记录指针进入输入框，鼠标或触摸输入只保留文字光标。 */
function handleComposerPointerDown(): void {
  isComposerPointerFocused.value = true
}

/** handleComposerBlur 清理指针焦点来源，使后续键盘进入时恢复焦点提示。 */
function handleComposerBlur(): void {
  isComposerPointerFocused.value = false
}

/** eventTime 格式化持久事件业务时间。 */
function eventTime(value: number): string {
  return new Intl.DateTimeFormat('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).format(new Date(value))
}

/** avatarText 返回说话人或来源的短头像。 */
function avatarText(label: string): string {
  return label === 'AI 助手' ? 'AI' : label.slice(-1)
}

/** sourceLabel 返回统一事件的人类可读类型。 */
function sourceLabel(entry: TimelineEntry): string {
  if (entry.kind === 'message')
    return entry.source === 'host' ? '主持人消息' : '文字消息'
  if (entry.kind === 'resource') return '附件'
  if (entry.kind === 'ai_question') return 'AI 问题'
  if (entry.kind === 'ai_answer') return 'AI 回答'
  if (entry.kind === 'gap') return '转写缺口'
  return '会议事件'
}

/** displayName 返回后端投影的事件来源；AI 问题随说话人校对更新，AI 回答固定为助手。 */
function displayName(entry: TimelineEntry): string {
  if (entry.kind.startsWith('ai_'))
    return entry.kind === 'ai_question'
      ? entry.display_name || '未识别说话人'
      : 'AI 助手'
  return entry.display_name || (entry.source === 'host' ? '你' : '访客')
}

/** formatBytes 用紧凑单位展示附件真实大小。 */
function formatBytes(value = 0): string {
  if (value < 1024 * 1024) return `${Math.max(0, value / 1024).toFixed(1)} KB`
  return `${(value / 1024 / 1024).toFixed(1)} MB`
}

/** openAttachment 通过后端资源 ID 调用系统默认应用，不接触本地路径。 */
async function openAttachment(resourceID?: string): Promise<void> {
  if (resourceID) await OpenResource(resourceID)
}
</script>

<template>
  <article class="ms-card timeline-workspace">
    <header class="timeline-heading">
      <div>
        <p class="ms-eyebrow">{{ subject }}</p>
        <h1>会议发言与消息</h1>
      </div>
      <span class="ms-status-pill is-ok">实时更新中</span>
    </header>

    <div class="conversation-shell">
      <div
        ref="scrollElement"
        class="conversation-scroll"
        @scroll="handleScroll"
      >
        <button
          v-if="timeline.hasOlder"
          class="ms-button ms-button--quiet load-history"
          type="button"
          :disabled="timeline.loadingOlder"
          @click="loadOlder"
        >
          {{ timeline.loadingOlder ? '正在加载…' : '查看更早消息' }}
        </button>

        <ol class="conversation-list" aria-live="polite">
          <li
            v-for="row in rows"
            :key="rowKey(row)"
            class="conversation-item"
            :class="rowClass(row)"
          >
            <template v-if="isUtteranceGroup(row)">
              <span class="avatar speaker-avatar" aria-hidden="true">{{
                avatarText(row.speakerLabel)
              }}</span>
              <article class="conversation-body">
                <header class="message-head">
                  <strong>{{ row.speakerLabel }}</strong>
                  <span class="event-kind">会议发言</span>
                  <time class="ms-meta ms-input--mono">{{
                    eventTime(row.entries[0].occurred_at)
                  }}</time>
                </header>
                <p v-for="entry in row.entries" :key="entry.seq">
                  {{ entry.text }}
                </p>
              </article>
            </template>
            <template v-else>
              <span class="avatar speaker-avatar" aria-hidden="true">{{
                avatarText(displayName(row))
              }}</span>
              <article class="conversation-body">
                <header class="message-head">
                  <strong>{{ displayName(row) }}</strong>
                  <span class="event-kind">{{ sourceLabel(row) }}</span>
                  <time class="ms-meta ms-input--mono">{{
                    eventTime(row.occurred_at)
                  }}</time>
                </header>
                <button
                  v-if="row.kind === 'resource'"
                  class="attachment-card"
                  type="button"
                  @click="openAttachment(row.entity_id)"
                >
                  <span class="attachment-icon" aria-hidden="true"
                    ><svg viewBox="0 0 24 24">
                      <path
                        d="M15 7 8.5 13.5a2.1 2.1 0 0 0 3 3L18 10a4 4 0 0 0-5.7-5.6L5.8 10.9a6 6 0 0 0 8.5 8.5l6-6"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.8"
                        stroke-linecap="round"
                        stroke-linejoin="round"
                      /></svg
                  ></span>
                  <span
                    ><strong>{{ row.original_name }}</strong
                    ><small
                      >{{ formatBytes(row.size_bytes) }} ·
                      {{ row.media_type || '附件' }}</small
                    ></span
                  >
                  <span class="attachment-open">打开</span>
                </button>
                <SafeMarkdown
                  v-else-if="row.content_format === 'markdown'"
                  :content="row.text || ''"
                />
                <p v-else>{{ row.text || row.reason || '事件未完成。' }}</p>
                <span v-if="row.kind === 'ai_answer'" class="message-note"
                  >AI 内容未经人工确认</span
                >
              </article>
            </template>
          </li>

          <li
            v-for="upload in timeline.activeUploads"
            :key="upload.request_id"
            class="conversation-item is-host-message is-partial"
          >
            <span class="avatar speaker-avatar" aria-hidden="true">你</span>
            <article class="conversation-body">
              <header class="message-head">
                <strong>你</strong
                ><span class="event-kind">{{
                  upload.state === 'failed' ? '发送失败' : '正在发送附件'
                }}</span
                ><time class="ms-meta">现在</time>
              </header>
              <p>{{ upload.name }} · {{ formatBytes(upload.size_bytes) }}</p>
            </article>
          </li>

          <li v-if="agentPartial" class="conversation-item is-agent is-partial">
            <span class="avatar speaker-avatar" aria-hidden="true">AI</span>
            <article class="conversation-body typewriter-line">
              <header class="message-head">
                <strong>AI 助手</strong><span class="event-kind">正在回答</span
                ><time class="ms-meta">现在</time>
              </header>
              <SafeMarkdown :content="agentPartial" />
              <button
                class="ms-button ms-button--quiet"
                type="button"
                @click="emit('interruptAgent')"
              >
                停止回答
              </button>
            </article>
          </li>

          <li
            v-for="partial in transcriptionPaused
              ? []
              : timeline.orderedPartials"
            :key="`${partial.session_id}:${partial.result_id}`"
            class="conversation-item is-partial"
          >
            <span class="avatar speaker-avatar" aria-hidden="true">…</span>
            <article class="conversation-body typewriter-line">
              <header class="message-head">
                <strong>正在识别</strong><span class="event-kind">实时转写</span
                ><time class="ms-meta">现在</time>
              </header>
              <p>{{ partial.text }}</p>
            </article>
          </li>
        </ol>
      </div>
      <button
        v-if="unreadCount"
        class="new-message-indicator"
        type="button"
        @click="scrollToLatest(true)"
      >
        <span>{{ unreadCount }} 条新消息</span
        ><span class="new-message-action">查看最新</span>
      </button>
    </div>

    <form
      v-if="!ended"
      class="meeting-composer"
      aria-label="发送会议消息"
      @submit.prevent="submitMessage"
    >
      <label class="ms-label" for="meeting-message">会议消息</label>
      <div class="composer-box">
        <textarea
          id="meeting-message"
          v-model="draft"
          class="composer-input"
          :class="{ 'is-pointer-focused': isComposerPointerFocused }"
          rows="1"
          maxlength="10000"
          placeholder="输入需要写入本场会议记录的文字"
          @pointerdown="handleComposerPointerDown"
          @blur="handleComposerBlur"
          @keydown="handleComposerKey"
        />
        <div class="composer-footer">
          <button
            class="ms-button ms-button--quiet composer-icon-button"
            type="button"
            aria-label="发送附件"
            title="发送附件"
            :disabled="timeline.choosingAttachment"
            @click="timeline.chooseAttachment()"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M15 7 8.5 13.5a2.1 2.1 0 0 0 3 3L18 10a4 4 0 0 0-5.7-5.6L5.8 10.9a6 6 0 0 0 8.5 8.5l6-6"
                fill="none"
                stroke="currentColor"
                stroke-width="1.8"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
          </button>
          <div class="composer-actions">
            <span class="ms-help">Enter 发送，Shift + Enter 换行</span>
            <button
              class="ms-button ms-button--quiet"
              type="button"
              :disabled="!agentAvailable"
              @click="emit('askAgent')"
            >
              请 AI 参与
            </button>
            <button
              class="ms-button ms-button--primary"
              type="submit"
              :disabled="!draft.trim() || timeline.sending"
            >
              {{ timeline.sending ? '发送中…' : '发送' }}
            </button>
          </div>
        </div>
      </div>
    </form>
    <div v-else class="meeting-composer meeting-composer--followup">
      <slot name="followup" />
    </div>
    <p v-if="timeline.errorMessage" class="ms-help is-danger" role="alert">
      {{ timeline.errorMessage }}
    </p>
  </article>
</template>
