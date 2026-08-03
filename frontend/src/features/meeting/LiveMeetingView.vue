<script lang="ts" setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { EventsOn } from '../../../wailsjs/runtime/runtime'
import QRCode from 'qrcode'

import { useMeetingStore } from '../../stores/meeting'
import { useASRStore } from '../../stores/asr'
import { useLANStore } from '../../stores/lan'
import { useAgentStore } from '../../stores/agent'
import { useGapStore } from '../../stores/gap'
import FinalizingView from '../finalization/FinalizingView.vue'

const meeting = useMeetingStore()
const asr = useASRStore()
const lan = useLANStore()
const agent = useAgentStore()
const postGaps = useGapStore()
const emit = defineEmits<{
  openCorrection: []
  openGap: [gapId: string]
  openMinutes: []
}>()
const confirmEnd = ref(false)
const showGuestEntry = ref(false)
const showAskAgent = ref(false)
const agentQuestion = ref('')
const agentBusySince = ref(0)
const qrDataURL = ref('')
const copyFeedback = ref('')
const modalElement = ref<HTMLElement | null>(null)
let returnFocus: HTMLElement | null = null
const now = ref(Date.now())
let refreshTicks = 0
let stopPartialListener: (() => void) | undefined
let stopStateListener: (() => void) | undefined
let stopAgentChangedListener: (() => void) | undefined
let stopAgentDeltaListener: (() => void) | undefined
let stopAgentApprovalListener: (() => void) | undefined
let stopAgentTimelineListener: (() => void) | undefined
let stopGapListener: (() => void) | undefined
const timer = window.setInterval(() => {
  now.value = Date.now()
  refreshTicks++
  // 查询是事件丢失后的事实重同步，也能及时显示设备断开产生的 interrupted。
  if (refreshTicks % 2 === 0 && !meeting.saving) {
    void meeting.refreshCurrentMeeting()
    if (meeting.current?.id) void asr.restoreTimeline(meeting.current.id)
    if (meeting.current?.id) void agent.refreshState(meeting.current.id)
    if (meeting.current?.id && meeting.current.lifecycle_state === 'ended')
      void postGaps.refresh(meeting.current.id)
    void lan.refreshStatus()
  }
}, 1000)

onMounted(async () => {
  const meetingID = meeting.current?.id
  if (meetingID) {
    asr.realtimeState = meeting.current?.realtime_asr_state || 'idle'
    await asr.restoreTimeline(meetingID)
    await Promise.all([
      agent.refreshState(meetingID),
      agent.restoreTimeline(meetingID),
    ])
  }
  await Promise.all([lan.loadInterfaces(), lan.refreshStatus()])
  if (meetingID && meeting.current?.lifecycle_state === 'ended')
    await postGaps.refresh(meetingID)
  stopPartialListener = EventsOn('meeting.asr.partial', (event) => {
    asr.applyPartial(event)
  })
  stopStateListener = EventsOn('meeting.asr.changed', (event) => {
    asr.applyRealtimeState(event)
    if (meeting.current?.id) void asr.restoreTimeline(meeting.current.id)
  })
  const applyAgentEvent = (event: Parameters<typeof agent.applyEvent>[0]) => {
    agent.applyEvent(event)
    if (agent.runtime.state === 'busy' && !agentBusySince.value)
      agentBusySince.value = Date.now()
    if (agent.runtime.state !== 'busy') agentBusySince.value = 0
  }
  stopAgentChangedListener = EventsOn('meeting.agent.changed', applyAgentEvent)
  stopAgentDeltaListener = EventsOn('meeting.agent.delta', applyAgentEvent)
  stopAgentApprovalListener = EventsOn(
    'meeting.agent.approval.requested',
    applyAgentEvent,
  )
  stopAgentTimelineListener = EventsOn('meeting.agent.timeline.changed', () => {
    if (meeting.current?.id) void agent.restoreTimeline(meeting.current.id)
  })
  stopGapListener = EventsOn('meeting.gap.changed', (event) => {
    if (postGaps.applyEvent(event) && meeting.current?.id)
      void postGaps.refresh(meeting.current.id)
  })
})

onBeforeUnmount(() => {
  window.clearInterval(timer)
  stopPartialListener?.()
  stopStateListener?.()
  stopAgentChangedListener?.()
  stopAgentDeltaListener?.()
  stopAgentApprovalListener?.()
  stopAgentTimelineListener?.()
  stopGapListener?.()
  document.querySelector('.ms-app-shell')?.removeAttribute('inert')
})

watch(
  () => lan.status.join_url,
  async (joinURL) => {
    qrDataURL.value = joinURL
      ? await QRCode.toDataURL(joinURL, { width: 272, margin: 1 })
      : ''
  },
  { immediate: true },
)

watch(
  () =>
    confirmEnd.value ||
    showGuestEntry.value ||
    showAskAgent.value ||
    Boolean(agent.runtime.approval),
  async (open) => {
    const shell = document.querySelector('.ms-app-shell')
    if (open) {
      returnFocus =
        document.activeElement instanceof HTMLElement
          ? document.activeElement
          : null
      shell?.setAttribute('inert', '')
      await nextTick()
      modalElement.value
        ?.querySelector<HTMLElement>('button, [href], input, select, textarea')
        ?.focus()
    } else {
      shell?.removeAttribute('inert')
      returnFocus?.focus()
      returnFocus = null
    }
  },
)

const projection = computed(() => meeting.current)
const elapsed = computed(() => {
  const startedAt = projection.value?.started_at
  if (!startedAt) return '00:00:00'
  // 已结束或中断的会议必须冻结计时，不能把重启后的时间误算为会议时长。
  const isFinished = ['ended', 'interrupted'].includes(
    projection.value?.lifecycle_state ?? '',
  )
  const finishedAt =
    projection.value?.ended_at ??
    (isFinished ? projection.value?.updated_at : now.value)
  const seconds = Math.max(0, Math.floor((finishedAt - startedAt) / 1000))
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const remainder = seconds % 60
  return [hours, minutes, remainder]
    .map((value) => String(value).padStart(2, '0'))
    .join(':')
})
const isInterrupted = computed(
  () => projection.value?.lifecycle_state === 'interrupted',
)
const isEnded = computed(() => projection.value?.lifecycle_state === 'ended')
const isFinalizing = computed(
  () => meeting.saving || projection.value?.lifecycle_state === 'finalizing',
)
const asrState = computed(
  () => projection.value?.realtime_asr_state || asr.realtimeState || 'idle',
)
const activeUploads = computed(() => lan.status.active_uploads ?? [])
const unifiedTimeline = computed(() =>
  [
    ...asr.timeline.map((entry) => ({ ...entry, source: 'asr' as const })),
    ...agent.timeline.map((entry) => ({
      ...entry,
      source: 'agent' as const,
      start_sample: 0,
      end_sample: 0,
    })),
  ].sort((left, right) => left.seq - right.seq),
)
const agentBusyLong = computed(
  () =>
    agent.runtime.state === 'busy' &&
    agentBusySince.value > 0 &&
    now.value - agentBusySince.value >= 30_000,
)
const agentQuestionBytes = computed(
  () => new TextEncoder().encode(agentQuestion.value).length,
)
const maskedJoinURL = computed(() => {
  const joinURL = lan.status.join_url ?? ''
  const marker = joinURL.indexOf('#')
  return marker >= 0 ? `${joinURL.slice(0, marker)}#k=••••••••` : joinURL
})

/** asrStateText 把内部状态码映射为统一中文状态动词。 */
function asrStateText(state: string): string {
  const labels: Record<string, string> = {
    idle: '等待开始',
    connecting: '正在连接',
    streaming: '实时转写中',
    reconnecting: '正在重连',
    unavailable: '实时转写不可用',
    stopped: '已停止',
  }
  return labels[state] ?? '状态待确认'
}

/** recordingStateText 把录音生命周期映射为用户可理解的独立状态。 */
function recordingStateText(state: string): string {
  const labels: Record<string, string> = {
    preparing: '正在准备',
    recording: '录音中',
    finalizing: '正在结束',
    ended: '已结束',
    interrupted: '已中断',
  }
  return labels[state] ?? '状态待确认'
}

/** localSaveStateText 把本地保存状态映射为用户文案，不与录音状态合并。 */
function localSaveStateText(state: string): string {
  const labels: Record<string, string> = {
    saving: '正在保存',
    saved: '已保存',
    failed: '保存失败',
  }
  return labels[state] ?? '状态待确认'
}

/** gapReasonText 把缺口原因转换为不暴露内部错误的用户说明。 */
function gapReasonText(reason?: string): string {
  const labels: Record<string, string> = {
    connect_failed: '连接失败',
    disconnected: '连接中断',
    backpressure: '实时处理拥塞',
    tail_timeout: '尾部结果等待超时',
    recovery: '应用异常退出后恢复',
    record_only: '会议选择仅录音',
  }
  return labels[reason ?? ''] ?? '实时转写缺口'
}

/** sampleTime 把 16 kHz 全局样本位置格式化为会议相对时间。 */
function sampleTime(sample: number): string {
  const total = Math.max(0, Math.floor(sample / 16000))
  const minutes = Math.floor(total / 60)
  const seconds = total % 60
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
}

/** eventTime 把 AI 事件毫秒时间格式化为本地时分秒。 */
function eventTime(value: number): string {
  return new Intl.DateTimeFormat('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).format(new Date(value))
}

/** endMeeting 关闭确认框并等待唯一后端收尾结果。 */
async function endMeeting(): Promise<void> {
  confirmEnd.value = false
  await meeting.endMeeting()
}

/** openEndConfirmation 先读取活动上传事实，再显示对应的确认后果。 */
async function openEndConfirmation(): Promise<void> {
  await lan.refreshStatus()
  confirmEnd.value = true
}

/** endMeetingAndCancelUploads 显式取消活动上传后复用唯一会议收尾流程。 */
async function endMeetingAndCancelUploads(): Promise<void> {
  await Promise.all(
    activeUploads.value.map((item) => lan.cancelUpload(item.request_id)),
  )
  await endMeeting()
}

/** copyJoinURL 仅响应用户动作把本代入口写入系统剪贴板。 */
async function copyJoinURL(): Promise<void> {
  const joinURL = lan.status.join_url
  if (!joinURL) return
  await navigator.clipboard.writeText(joinURL)
  copyFeedback.value = '已复制入口'
  window.setTimeout(() => {
    copyFeedback.value = ''
  }, 1800)
}

/** formatBytes 以二进制单位显示服务端报告的真实落盘进度。 */
function formatBytes(bytes: number): string {
  if (bytes < 1024 ** 2) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 ** 2).toFixed(1)} MB`
}

/** uploadPercent 返回活动上传的有界百分比。 */
function uploadPercent(written: number, total: number): number {
  return total > 0 ? Math.min(100, Math.round((written / total) * 100)) : 0
}

/** lanStateText 映射独立 LAN 状态轴，不与录音状态合并。 */
function lanStateText(state: string): string {
  const labels: Record<string, string> = {
    disabled: '未启用',
    starting: '正在启动',
    serving: '运行中',
    stopping: '正在停止',
    stopped: '已停止',
    failed: '启动失败',
  }
  return labels[state] ?? '状态待确认'
}

/** agentStateText 把独立 Codex 状态轴映射为稳定中文。 */
function agentStateText(state: string): string {
  const labels: Record<string, string> = {
    unchecked: '尚未检测',
    initializing: '正在初始化',
    available: 'AI 可参与',
    busy: '正在回答',
    unavailable: '暂不可用',
    unsynced: '结束同步失败',
  }
  return labels[state] ?? '状态待确认'
}

/** submitAgentQuestion 关闭输入框并让异步 Binding 持续接收事件。 */
function submitAgentQuestion(): void {
  const question = agentQuestion.value.trim()
  if (
    !question ||
    question.length > 10000 ||
    agent.runtime.state !== 'available'
  )
    return
  showAskAgent.value = false
  agentQuestion.value = ''
  void agent.ask(question)
}

/** trapModal 把 Tab 焦点限制在当前模态框，并允许 Escape 返回。 */
function trapModal(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    confirmEnd.value = false
    showGuestEntry.value = false
    showAskAgent.value = false
    return
  }
  if (event.key !== 'Tab' || !modalElement.value) return
  const focusable = Array.from(
    modalElement.value.querySelectorAll<HTMLElement>(
      'button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled)',
    ),
  )
  if (!focusable.length) return
  const first = focusable[0]
  const last = focusable.at(-1)!
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}
</script>

<template>
  <section
    v-if="projection && !isFinalizing"
    class="ms-live-stage"
    :class="{ 'is-interrupted': isInterrupted }"
  >
    <div>
      <p class="ms-recording-line" aria-live="polite">
        <span class="ms-recording-dot" aria-hidden="true" />
        <span v-if="isInterrupted">录音已中断 · 不能继续原录音</span>
        <span v-else-if="isEnded">录音已结束 · 本地保存完成</span>
        <span v-else-if="isFinalizing">录音已停止 · 正在安全保存录音</span>
        <span v-else>录音中 · 正在本地保存</span>
      </p>
      <h1>{{ projection.subject }}</h1>
      <p class="ms-lead">
        录音、本地保存和实时转写分别运行；转写中断不会停止本地录音。
      </p>
    </div>
    <div class="ms-live-clock" :aria-label="`会议已进行 ${elapsed}`">
      {{ elapsed }}
    </div>
  </section>

  <p
    v-if="meeting.errorMessage"
    class="ms-notice ms-notice--danger"
    role="alert"
  >
    {{ meeting.errorMessage }}
  </p>

  <FinalizingView
    v-if="projection && isFinalizing"
    :meeting-id="projection.id"
    :meeting-no="projection.meeting_no"
    :recording-state="projection.lifecycle_state"
    :local-save-state="projection.local_save_state"
    :asr-state="asrState"
    :lan-state="lan.status.state"
    :agent-state="agent.runtime.state"
  />

  <div
    v-if="!isFinalizing && ['reconnecting', 'unavailable'].includes(asrState)"
    class="ms-notice ms-notice--warning"
    role="status"
    aria-live="polite"
  >
    <div>
      <strong>{{
        asrState === 'reconnecting' ? '实时转写正在重连' : '实时转写暂时不可用'
      }}</strong>
      <p>录音仍在本地保存。未获得 final 的范围会作为转写缺口保留。</p>
      <button
        v-if="asrState === 'unavailable'"
        class="ms-button ms-button--quiet"
        type="button"
        :disabled="asr.retrying"
        @click="asr.retryRealtime()"
      >
        {{ asr.retrying ? '正在重试…' : '重试实时转写' }}
      </button>
    </div>
  </div>

  <div
    v-if="!isFinalizing && asr.rawRecordState === 'failed'"
    class="ms-notice ms-notice--danger"
    role="alert"
  >
    <div>
      <strong>会议原始记录刷新失败</strong>
      <p>
        SQLite
        中的转写事实仍然保留。结束会议会再次强制刷新；在刷新成功前不会显示原始记录已完成。
      </p>
    </div>
  </div>

  <section
    v-if="projection && !isFinalizing"
    class="ms-meeting-split ms-live-layout"
  >
    <div class="ms-card ms-meeting-card">
      <div class="ms-card-head">
        <div>
          <p class="ms-eyebrow">统一时间线</p>
          <span class="ms-help">最近发言与 AI 参与记录</span>
        </div>
        <button
          v-if="!isInterrupted && !isEnded"
          class="ms-button ms-button--primary"
          type="button"
          :disabled="agent.runtime.state !== 'available'"
          @click="showAskAgent = true"
        >
          请 AI 参与
        </button>
      </div>
      <h2 v-if="isInterrupted">现有录音材料已保留</h2>
      <h2 v-else-if="isEnded">完整录音已安全保存</h2>
      <h2 v-else>会议正在录音</h2>
      <p v-if="isInterrupted || isEnded" class="ms-lead">
        {{
          isInterrupted
            ? '可以重试文件对账和合并，不能在原会议上续录。'
            : '录音按 60 秒精确分片，并以 2 秒检查点同步到本地磁盘。'
        }}
      </p>
      <div
        v-if="
          unifiedTimeline.length ||
          asr.orderedPartials.length ||
          agent.runtime.partial
        "
        class="ms-transcript-list"
        aria-live="polite"
      >
        <article
          v-for="entry in unifiedTimeline"
          :key="entry.seq"
          class="ms-transcript-entry"
          :class="{
            'is-gap': entry.kind === 'asr.gap',
            'is-agent': entry.source === 'agent',
          }"
        >
          <time>{{
            entry.source === 'agent'
              ? eventTime(entry.occurred_at)
              : sampleTime(entry.start_sample)
          }}</time>
          <div v-if="entry.source === 'agent'">
            <strong>{{
              entry.kind === 'ai.question'
                ? '你 · AI 问题'
                : entry.kind === 'ai.answer'
                  ? 'AI 助手 · 未经人工确认'
                  : 'AI 状态'
            }}</strong>
            <p>
              {{
                entry.text ||
                (entry.kind === 'ai.cancelled'
                  ? '回答已停止。'
                  : '回答未完成。')
              }}
            </p>
          </div>
          <div v-else-if="entry.kind === 'utterance.final'">
            <strong>{{
              entry.speaker_label
                ? `说话人 ${entry.speaker_label}`
                : `说话人 · 会话 ${entry.session_order || 1}`
            }}</strong>
            <p>{{ entry.text }}</p>
          </div>
          <div v-else>
            <strong>{{ gapReasonText(entry.gap_reason) }}</strong>
            <p>
              {{ sampleTime(entry.start_sample) }}–{{
                sampleTime(entry.end_sample)
              }}
              暂无实时文字，录音已保留。
            </p>
          </div>
        </article>
        <article
          v-if="agent.runtime.partial"
          class="ms-transcript-entry is-agent is-partial"
        >
          <time>现在</time>
          <div>
            <strong>AI 助手 · 正在回答</strong>
            <p>{{ agent.runtime.partial }}</p>
            <p class="ms-help">
              当前内容仅主持人可见。最终回答成功后会自动公开到局域网访客页。
            </p>
            <button
              class="ms-button ms-button--quiet"
              type="button"
              @click="agent.interrupt()"
            >
              停止回答
            </button>
          </div>
        </article>
        <article
          v-for="partial in asr.orderedPartials"
          :key="partial.result_id"
          class="ms-transcript-entry is-partial"
        >
          <time>{{ sampleTime(partial.start_sample) }}</time>
          <div>
            <strong>正在识别</strong>
            <p>{{ partial.text }}</p>
          </div>
        </article>
      </div>
      <div v-else-if="!isInterrupted && !isEnded" class="ms-transcript-empty">
        <h2>
          {{ asrState === 'streaming' ? '正在聆听' : asrStateText(asrState) }}
        </h2>
        <p>final 结果会保存在 SQLite，并同步生成会议原始记录。</p>
      </div>
      <div v-if="isInterrupted" class="ms-actions">
        <button
          class="ms-button ms-button--primary"
          type="button"
          :disabled="meeting.saving"
          @click="meeting.retryRecovery()"
        >
          {{ meeting.saving ? '正在重试保存…' : '重试保存' }}
        </button>
        <button
          class="ms-button ms-button--quiet"
          type="button"
          @click="emit('openCorrection')"
        >
          校对原始记录
        </button>
        <button
          class="ms-button ms-button--quiet"
          type="button"
          @click="meeting.startNewMeeting()"
        >
          开始新会议
        </button>
      </div>
      <div v-else-if="isEnded" class="ms-actions">
        <button
          class="ms-button ms-button--primary"
          type="button"
          @click="emit('openCorrection')"
        >
          校对原始记录
        </button>
        <button
          class="ms-button ms-button--quiet"
          type="button"
          @click="emit('openMinutes')"
        >
          会议纪要
        </button>
        <button
          v-if="postGaps.conflictGap"
          class="ms-button ms-button--quiet"
          type="button"
          @click="emit('openGap', postGaps.conflictGap.id)"
        >
          处理补转写冲突
        </button>
        <button
          v-else-if="postGaps.state === 'failed'"
          class="ms-button ms-button--quiet"
          type="button"
          :disabled="postGaps.submitting"
          @click="postGaps.retryFailed(projection.id)"
        >
          重试补转写
        </button>
        <button
          v-else-if="postGaps.state === 'processing'"
          class="ms-button ms-button--quiet"
          type="button"
          :disabled="postGaps.submitting"
          @click="postGaps.stop(projection.id)"
        >
          停止补转写
        </button>
        <button
          class="ms-button ms-button--quiet"
          type="button"
          @click="meeting.startNewMeeting()"
        >
          开始新会议
        </button>
      </div>
      <div v-if="isEnded && postGaps.gaps.length" class="ms-gap-summary">
        <strong>存在 {{ postGaps.gaps.length }} 段转写缺口</strong>
        <p>
          本地录音已保存。当前补转写状态：{{
            postGaps.state
          }}；外部处理不影响本地保存。
        </p>
      </div>
      <div
        v-if="agentBusyLong"
        class="ms-notice ms-notice--warning"
        role="status"
      >
        <div>
          <strong>AI 仍在处理</strong>
          <p>工具调用和等待主持人审批都计入本次任务的 10 分钟总时限。</p>
        </div>
      </div>
    </div>

    <aside class="ms-stack ms-live-aside">
      <div class="ms-card ms-meeting-card">
        <div class="ms-card-head"><h2>系统状态</h2></div>
        <ul class="ms-status-list">
          <li>
            <span>录音</span
            ><strong>{{
              recordingStateText(projection.lifecycle_state)
            }}</strong>
          </li>
          <li>
            <span>本地保存</span
            ><strong>{{
              localSaveStateText(projection.local_save_state)
            }}</strong>
          </li>
          <li>
            <span>实时转写</span
            ><strong :class="{ 'is-ok': asrState === 'streaming' }">{{
              asrStateText(asrState)
            }}</strong>
          </li>
          <li>
            <span>原始记录</span
            ><strong :class="{ 'is-ok': asr.rawRecordState === 'current' }">{{
              asr.rawRecordState === 'failed'
                ? '刷新失败'
                : ['writing', 'dirty'].includes(asr.rawRecordState)
                  ? '正在刷新'
                  : asr.rawRecordState === 'current'
                    ? '已刷新'
                    : '等待转写'
            }}</strong>
          </li>
          <li>
            <span>Codex</span
            ><strong
              :class="{ 'is-ok': agent.runtime.state === 'available' }"
              >{{ agentStateText(agent.runtime.state) }}</strong
            >
          </li>
          <li>
            <span>访客页</span
            ><strong :class="{ 'is-ok': lan.status.state === 'serving' }">{{
              lanStateText(lan.status.state)
            }}</strong>
          </li>
        </ul>
      </div>
      <div
        v-if="agent.runtime.state === 'unavailable'"
        class="ms-card ms-meeting-card"
      >
        <div class="ms-card-head"><h2>AI 暂不可用</h2></div>
        <p class="ms-help">
          录音和实时转写不受影响。检查 Codex 登录和版本后可重新检测。
        </p>
        <p v-if="agent.errorMessage" class="ms-help is-danger" role="alert">
          {{ agent.errorMessage }}
        </p>
        <div class="ms-actions">
          <button
            class="ms-button ms-button--quiet"
            type="button"
            @click="agent.retry()"
          >
            重新检测
          </button>
        </div>
      </div>
      <div
        v-if="lan.status.state !== 'disabled'"
        class="ms-card ms-meeting-card ms-lan-live-card"
      >
        <div class="ms-card-head">
          <div>
            <h2>局域网访客页</h2>
            <p class="ms-help">
              {{ lan.status.address || '等待网络地址' }}
            </p>
          </div>
          <strong :class="{ 'is-ok': lan.status.state === 'serving' }">
            {{
              lan.status.state === 'serving'
                ? `${lan.status.online_count} 人在线`
                : lanStateText(lan.status.state)
            }}
          </strong>
        </div>
        <p v-if="maskedJoinURL" class="ms-lan-address">
          {{ maskedJoinURL }}
        </p>
        <p v-if="lan.errorMessage" class="ms-help is-danger" role="alert">
          {{ lan.errorMessage }}
        </p>
        <div class="ms-actions">
          <button
            v-if="lan.status.state === 'serving'"
            class="ms-button ms-button--quiet"
            type="button"
            @click="showGuestEntry = true"
          >
            显示入口
          </button>
          <button
            v-if="['failed', 'stopped', 'serving'].includes(lan.status.state)"
            class="ms-button ms-button--quiet"
            type="button"
            :disabled="lan.loading || !lan.selectedInterfaceID"
            @click="lan.retry()"
          >
            {{ lan.loading ? '正在启动…' : '重新启动' }}
          </button>
        </div>
        <p class="ms-help">
          访客只能查看会议消息和已公开资料，不能查看实时转写。
        </p>
      </div>
    </aside>
  </section>

  <Teleport to="body">
    <div
      v-if="showAskAgent"
      class="ms-modal-backdrop"
      @click.self="showAskAgent = false"
      @keydown="trapModal"
    >
      <section
        ref="modalElement"
        class="ms-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="ask-agent-title"
      >
        <h2 id="ask-agent-title">请 AI 参与</h2>
        <p class="ms-lead">
          AI
          会读取本次提问开始前已提交的会议文字和文件。成功的最终回答会公开到访客页。
        </p>
        <div class="ms-field">
          <label for="agent-question">问题</label>
          <textarea
            id="agent-question"
            v-model="agentQuestion"
            class="ms-input ms-textarea"
            maxlength="10000"
            rows="5"
            autofocus
            @keydown.meta.enter="submitAgentQuestion"
            @keydown.ctrl.enter="submitAgentQuestion"
          />
          <p class="ms-help">{{ agentQuestionBytes }} / 10000 bytes</p>
        </div>
        <div class="ms-actions ms-modal-actions">
          <button
            class="ms-button ms-button--quiet"
            type="button"
            @click="showAskAgent = false"
          >
            取消
          </button>
          <button
            class="ms-button ms-button--primary"
            type="button"
            :disabled="!agentQuestion.trim() || agentQuestionBytes > 10000"
            @click="submitAgentQuestion"
          >
            提交问题
          </button>
        </div>
      </section>
    </div>
  </Teleport>

  <Teleport to="body">
    <div
      v-if="agent.runtime.approval"
      class="ms-modal-backdrop"
      @keydown="trapModal"
    >
      <section
        ref="modalElement"
        class="ms-modal"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="agent-approval-title"
      >
        <h2 id="agent-approval-title">Codex 请求本次操作权限</h2>
        <p class="ms-lead">
          此请求来自 Codex 原生权限系统。MeetSieve 不会保存“始终允许”。
        </p>
        <div class="ms-model-facts">
          <p>
            <span>工具</span><strong>{{ agent.runtime.approval.tool }}</strong>
          </p>
          <p>
            <span>目标</span
            ><strong>{{ agent.runtime.approval.target || '未提供' }}</strong>
          </p>
          <p>
            <span>参数摘要</span
            ><strong>{{
              agent.runtime.approval.parameter_summary || '未提供'
            }}</strong>
          </p>
          <p>
            <span>风险说明</span
            ><strong>{{ agent.runtime.approval.risk }}</strong>
          </p>
        </div>
        <div class="ms-actions ms-modal-actions">
          <button
            class="ms-button ms-button--quiet"
            type="button"
            autofocus
            @click="agent.respondApproval('decline')"
          >
            拒绝
          </button>
          <button
            class="ms-button ms-button--primary"
            type="button"
            @click="agent.respondApproval('allow')"
          >
            仅允许本次
          </button>
        </div>
      </section>
    </div>
  </Teleport>

  <Teleport to="body">
    <div
      v-if="confirmEnd"
      class="ms-modal-backdrop"
      @click.self="confirmEnd = false"
      @keydown="trapModal"
    >
      <section
        ref="modalElement"
        class="ms-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="end-meeting-title"
      >
        <template v-if="activeUploads.length">
          <h2 id="end-meeting-title">还有附件正在上传</h2>
          <p class="ms-lead">
            结束会议会立即停止访客访问，并取消尚未完成的上传。已保存的录音、消息和附件不会受影响。
          </p>
          <div
            v-for="item in activeUploads"
            :key="item.request_id"
            class="ms-upload-item"
          >
            <div class="ms-card-head">
              <div class="ms-upload-copy">
                <strong>{{ item.name }}</strong>
                <p class="ms-help">
                  {{ formatBytes(item.written) }} /
                  {{ formatBytes(item.total) }}
                </p>
              </div>
              <strong>上传中</strong>
            </div>
            <div
              class="ms-progress"
              role="progressbar"
              aria-label="附件上传进度"
              aria-valuemin="0"
              aria-valuemax="100"
              :aria-valuenow="uploadPercent(item.written, item.total)"
            >
              <span
                :style="{
                  width: `${uploadPercent(item.written, item.total)}%`,
                }"
              />
            </div>
          </div>
        </template>
        <template v-else>
          <h2 id="end-meeting-title">结束并保存会议？</h2>
          <p class="ms-lead">
            将立即停止访客访问，再停止麦克风、完成尾部转写、合并并校验完整录音。
          </p>
        </template>
        <div class="ms-actions ms-modal-actions ms-end-upload-actions">
          <button
            v-if="activeUploads.length"
            class="ms-button ms-button--primary"
            type="button"
            @click="confirmEnd = false"
          >
            等待上传完成
          </button>
          <button
            v-if="activeUploads.length"
            class="ms-button ms-button--danger"
            type="button"
            @click="endMeetingAndCancelUploads"
          >
            结束并取消上传
          </button>
          <button
            class="ms-button ms-button--quiet"
            type="button"
            autofocus
            @click="confirmEnd = false"
          >
            继续会议
          </button>
          <button
            v-if="!activeUploads.length"
            class="ms-button ms-button--primary"
            type="button"
            @click="endMeeting"
          >
            结束并保存
          </button>
        </div>
      </section>
    </div>
  </Teleport>

  <Teleport to="body">
    <div
      v-if="showGuestEntry"
      class="ms-modal-backdrop"
      @click.self="showGuestEntry = false"
      @keydown="trapModal"
    >
      <section
        ref="modalElement"
        class="ms-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="guest-entry-title"
      >
        <h2 id="guest-entry-title">局域网访客入口</h2>
        <div class="ms-guest-entry-grid">
          <img
            v-if="qrDataURL"
            class="ms-guest-qr"
            :src="qrDataURL"
            alt="访客入口二维码"
          />
          <div>
            <strong>同一私有网络内扫码进入</strong>
            <p class="ms-lan-address">{{ maskedJoinURL }}</p>
            <p class="ms-lead">会议结束或重新启动访客页后，此入口立即失效。</p>
          </div>
        </div>
        <div class="ms-notice ms-notice--warning">
          <div>
            <strong>不要在公共网络分享</strong>
            <p>访客页使用 HTTP，仅适合可信私有网络。</p>
          </div>
        </div>
        <p class="ms-help" aria-live="polite">{{ copyFeedback }}</p>
        <div class="ms-actions ms-modal-actions">
          <button
            class="ms-button ms-button--quiet"
            type="button"
            @click="showGuestEntry = false"
          >
            返回会议
          </button>
          <button
            class="ms-button ms-button--primary"
            type="button"
            @click="copyJoinURL"
          >
            复制入口
          </button>
        </div>
      </section>
    </div>
  </Teleport>

  <Teleport
    v-if="projection && !isInterrupted && !isEnded"
    defer
    to="#meeting-titlebar-actions"
  >
    <button
      class="ms-button ms-button--danger"
      type="button"
      :disabled="meeting.saving"
      @click="openEndConfirmation"
    >
      {{ isFinalizing ? '正在保存' : '结束会议' }}
    </button>
  </Teleport>
</template>
