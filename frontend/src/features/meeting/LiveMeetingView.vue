<script lang="ts" setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { EventsOn } from '../../../wailsjs/runtime/runtime'
import QRCode from 'qrcode'

import { useMeetingStore } from '../../stores/meeting'
import { useASRStore } from '../../stores/asr'
import { useLANStore } from '../../stores/lan'
import { useAgentStore } from '../../stores/agent'
import { useGapStore } from '../../stores/gap'
import { useSpeakerStore } from '../../stores/speaker'
import { useTimelineStore } from '../../stores/timeline'
import FinalizingView from '../finalization/FinalizingView.vue'
import MeetingTimelinePanel from './components/MeetingTimelinePanel.vue'

const meeting = useMeetingStore()
const asr = useASRStore()
const lan = useLANStore()
const agent = useAgentStore()
const postGaps = useGapStore()
const speaker = useSpeakerStore()
const timeline = useTimelineStore()
const router = useRouter()
const emit = defineEmits<{
  openCorrection: []
  openGap: [gapId: string]
  openMinutes: []
}>()
const confirmEnd = ref(false)
const showGuestEntry = ref(false)
const showAskAgent = ref(false)
const agentQuestion = ref('')
const qrDataURL = ref('')
const copyFeedback = ref('')
const modalElement = ref<HTMLElement | null>(null)
let returnFocus: HTMLElement | null = null
const now = ref(Date.now())
let refreshTicks = 0
let stopPartialListener: (() => void) | undefined
let stopPartialClearListener: (() => void) | undefined
let stopStateListener: (() => void) | undefined
let stopAgentChangedListener: (() => void) | undefined
let stopAgentDeltaListener: (() => void) | undefined
let stopAgentApprovalListener: (() => void) | undefined
let stopAgentTimelineListener: (() => void) | undefined
let stopAgentWakeListener: (() => void) | undefined
let stopGapListener: (() => void) | undefined
let stopTimelineListener: (() => void) | undefined
let stopAttachmentListener: (() => void) | undefined
let stopSpeakerListener: (() => void) | undefined
const timer = window.setInterval(() => {
  now.value = Date.now()
  refreshTicks++
  // 查询是事件丢失后的事实重同步，也能及时显示设备断开产生的 interrupted。
  if (refreshTicks % 2 === 0 && !meeting.saving) {
    void meeting.refreshCurrentMeeting()
    if (meeting.current?.id) void asr.restoreTimeline(meeting.current.id)
    if (meeting.current?.id) void agent.refreshState(meeting.current.id)
    if (meeting.current?.id && refreshTicks % 10 === 0)
      void speaker.refresh(meeting.current.id)
    if (meeting.current?.id) {
      void timeline.refreshStatus()
      if (refreshTicks % 10 === 0) {
        void timeline.recoverAfter()
        void timeline.refreshLatestProjection()
      }
    }
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
      speaker.refresh(meetingID),
      timeline.loadLatest(meetingID),
    ])
    await timeline.refreshStatus()
  }
  await Promise.all([lan.loadInterfaces(), lan.refreshStatus()])
  if (meetingID && meeting.current?.lifecycle_state === 'ended')
    await postGaps.refresh(meetingID)
  stopPartialListener = EventsOn('meeting.asr.partial', (event) => {
    asr.applyPartial(event)
    timeline.applyPartial(event)
  })
  stopPartialClearListener = EventsOn(
    'meeting.asr.partial.cleared',
    (event) => {
      asr.applyPartialClear(event)
      timeline.applyPartialClear(event)
    },
  )
  stopStateListener = EventsOn('meeting.asr.changed', (event) => {
    asr.applyRealtimeState(event)
    if (meeting.current?.id) void asr.restoreTimeline(meeting.current.id)
  })
  const applyAgentEvent = (event: Parameters<typeof agent.applyEvent>[0]) => {
    agent.applyEvent(event)
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
  stopAgentWakeListener = EventsOn('meeting.agent.wake.changed', (event) => {
    agent.applyWakeCommandEvent(event)
  })
  stopGapListener = EventsOn('meeting.gap.changed', (event) => {
    if (postGaps.applyEvent(event) && meeting.current?.id)
      void postGaps.refresh(meeting.current.id)
  })
  stopTimelineListener = EventsOn('meeting.timeline.changed', (event) => {
    if (event.data?.meeting_id === timeline.meetingID)
      void timeline.recoverAfter()
  })
  stopAttachmentListener = EventsOn(
    'meeting.attachment.upload.changed',
    (event) => timeline.applyAttachmentState(event),
  )
  stopSpeakerListener = EventsOn('meeting.speaker.changed', (event) => {
    if (event.data?.meeting_id === timeline.meetingID) {
      void timeline.refreshLatestProjection()
      void speaker.refresh(timeline.meetingID)
    }
  })
})

onBeforeUnmount(() => {
  window.clearInterval(timer)
  stopPartialListener?.()
  stopPartialClearListener?.()
  stopStateListener?.()
  stopAgentChangedListener?.()
  stopAgentDeltaListener?.()
  stopAgentApprovalListener?.()
  stopAgentTimelineListener?.()
  stopAgentWakeListener?.()
  stopGapListener?.()
  stopTimelineListener?.()
  stopAttachmentListener?.()
  stopSpeakerListener?.()
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
const mediaPausedForAI = computed(
  () =>
    agent.wakeCommand.state === 'codex_busy' ||
    asr.realtimeState === 'paused_for_ai' ||
    timeline.status.realtime_asr_state === 'paused_for_ai',
)
const mediaResumeFailed = computed(
  () =>
    agent.wakeCommand.state === 'failed' &&
    agent.wakeCommand.error_code === 'MEETING_MEDIA_RESUME_FAILED',
)
const asrState = computed(
  () =>
    (mediaPausedForAI.value && 'paused_for_ai') ||
    projection.value?.realtime_asr_state ||
    asr.realtimeState ||
    'idle',
)
const activeUploads = computed(() => lan.status.active_uploads ?? [])
const agentQuestionBytes = computed(
  () => new TextEncoder().encode(agentQuestion.value).length,
)
const maskedJoinURL = computed(() => {
  const joinURL = lan.status.join_url ?? ''
  const marker = joinURL.indexOf('#')
  return marker >= 0 ? `${joinURL.slice(0, marker)}#k=••••••••` : joinURL
})
const visibleSpeakers = computed(() => {
  const speakers = new Map<string, string>()
  for (const entry of timeline.entries) {
    if (entry.kind !== 'utterance') continue
    const key = entry.speaker_key || entry.speaker_label || 'unknown'
    speakers.set(key, entry.speaker_label || '未知说话人')
  }
  return [...speakers.entries()].map(([key, label]) => ({ key, label }))
})

/** speakerStateText 把自动识别门禁映射为不会误报成功的会中状态。 */
function speakerStateText(state: string): string {
  const labels: Record<string, string> = {
    ready: '可用',
    model_unavailable: '模型不可用',
    profile_missing: '缺少校准档案',
    profile_mismatch: '校准档案不匹配',
    voice_rebuild_required: '声纹需重建',
    unknown: '状态待确认',
  }
  return labels[state] ?? '状态待确认'
}

/** speakerUnavailableHelp 解释自动识别不可用的原因和对录音链路的影响。 */
function speakerUnavailableHelp(state: string): string {
  const messages: Record<string, string> = {
    model_unavailable:
      '说话人识别模型当前不可用。录音和转写仍会保存，模型恢复后会继续处理。',
    profile_missing:
      '声纹样本已保存，但缺少与当前模型匹配的正式校准档案，暂时无法自动关联成员。',
    profile_mismatch:
      '正式校准档案与当前模型不匹配，暂时无法自动关联成员。录音和转写不受影响。',
    voice_rebuild_required:
      '部分声纹样本还没有当前模型的特征，请在设置中完成声纹重建。',
    unknown: '暂时无法读取说话人识别状态，录音和转写仍会正常保存。',
  }
  return messages[state] ?? ''
}

/** isRecognizedSpeaker 只有正式成员名才表示自动或人工识别已完成。 */
function isRecognizedSpeaker(label: string): boolean {
  return (
    !label.startsWith('未知说话人') &&
    !label.startsWith('未识别说话人') &&
    !label.startsWith('说话人 ')
  )
}

/** asrStateText 把内部状态码映射为统一中文状态动词。 */
function asrStateText(state: string): string {
  const labels: Record<string, string> = {
    idle: '等待开始',
    connecting: '正在连接',
    streaming: '实时转写中',
    reconnecting: '正在重连',
    paused_for_ai: 'AI 回答期间已暂停',
    unavailable: '实时转写不可用',
    stopped: '已停止',
  }
  return labels[state] ?? '状态待确认'
}

/** visibleRecordingStateText 在语音 AI turn 期间如实表达 PCM 未写入会议。 */
function visibleRecordingStateText(state: string): string {
  return mediaPausedForAI.value
    ? 'AI 回答期间已暂停'
    : recordingStateText(state)
}

/** visibleLocalSaveStateText 区分保存至暂停边界和持续写入。 */
function visibleLocalSaveStateText(state: string): string {
  return mediaPausedForAI.value ? '已保存至暂停点' : localSaveStateText(state)
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

/** statusTime 格式化系统状态卡的最近 final 时间。 */
function statusTime(value?: number): string {
  if (!value) return '等待转写'
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

/** startNewMeeting 清除旧会议投影后进入会前页，由用户确认新会议配置。 */
function startNewMeeting(): void {
  meeting.startNewMeeting()
  void router.replace('/meetings/new')
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

/** visibleAgentStateText 优先展示短暂的语音指令阶段，不覆盖持久 Codex 可用性。 */
function visibleAgentStateText(): string {
  if (mediaResumeFailed.value) return '录音或转写恢复失败'
  const wakeLabels: Record<string, string> = {
    waiting_command: '已唤醒，请说指令',
    collecting: '正在听取指令',
    codex_busy: '正在回答',
    failed: '语音指令未提交',
  }
  return (
    wakeLabels[agent.wakeCommand.state] ?? agentStateText(agent.runtime.state)
  )
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
    <MeetingTimelinePanel
      :subject="projection?.subject || ''"
      :agent-partial="agent.runtime.partial"
      :agent-available="agent.runtime.state === 'available'"
      :transcription-paused="mediaPausedForAI"
      :ended="isEnded || isInterrupted"
      @ask-agent="showAskAgent = true"
      @interrupt-agent="agent.interrupt()"
    >
      <template #followup>
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
            @click="startNewMeeting"
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
            @click="startNewMeeting"
          >
            开始新会议
          </button>
        </div>
        <p v-if="isEnded && postGaps.gaps.length" class="ms-help">
          存在 {{ postGaps.gaps.length }} 段转写缺口；当前状态：{{
            postGaps.state
          }}。
        </p>
      </template>
    </MeetingTimelinePanel>
    <aside class="ms-stack ms-live-aside">
      <div class="ms-card ms-meeting-card">
        <div class="ms-card-head"><h2>系统状态</h2></div>
        <ul class="ms-status-list">
          <li>
            <span>录音时长</span
            ><strong class="ms-input--mono">{{ elapsed }}</strong>
          </li>
          <li>
            <span>录音</span
            ><strong>{{
              visibleRecordingStateText(projection.lifecycle_state)
            }}</strong>
          </li>
          <li>
            <span>麦克风</span
            ><strong
              :class="{
                'is-ok':
                  timeline.status.microphone_state === 'capturing' &&
                  !mediaPausedForAI,
              }"
              >{{
                mediaPausedForAI
                  ? '输入未写入会议'
                  : timeline.status.microphone_state === 'capturing'
                    ? '输入正常'
                    : timeline.status.microphone_state === 'unavailable'
                      ? '不可用'
                      : '已停止'
              }}</strong
            >
          </li>
          <li>
            <span>本地保存</span
            ><strong>{{
              visibleLocalSaveStateText(projection.local_save_state)
            }}</strong>
          </li>
          <li>
            <span>实时转写</span
            ><strong :class="{ 'is-ok': asrState === 'streaming' }">{{
              asrStateText(asrState)
            }}</strong>
          </li>
          <li>
            <span>说话人识别</span
            ><strong
              :class="{
                'is-ok': speaker.state === 'ready' && !mediaPausedForAI,
              }"
              >{{
                mediaPausedForAI
                  ? 'AI 回答期间已暂停'
                  : speakerStateText(speaker.state)
              }}</strong
            >
          </li>
          <li>
            <span>最近 final</span
            ><strong class="ms-input--mono">{{
              statusTime(timeline.status.latest_final_at)
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
              >{{ visibleAgentStateText() }}</strong
            >
          </li>
          <li>
            <span>访客页</span
            ><strong :class="{ 'is-ok': lan.status.state === 'serving' }">{{
              lanStateText(lan.status.state)
            }}</strong>
          </li>
        </ul>
        <p
          v-if="
            speaker.meetingID && speaker.state !== 'ready' && !mediaPausedForAI
          "
          class="ms-help is-danger"
          role="status"
        >
          {{ speakerUnavailableHelp(speaker.state) }}
        </p>
        <p v-if="mediaPausedForAI" class="ms-help" role="status">
          AI 回答期间，本地录音继续保存；实时转写已暂停，回答结束后自动恢复。
        </p>
        <p
          v-if="agent.wakeCommand.state === 'failed'"
          class="ms-help is-danger"
          role="alert"
        >
          <template v-if="mediaResumeFailed">
            AI
            已结束，但录音或实时转写尚未完整恢复。请结束当前会议并检查录音文件后重新开始会议。
          </template>
          <template v-else>
            语音指令未能提交给 Codex，请确认 Codex
            当前可用且没有其他任务正在执行，然后重新唤醒。
          </template>
        </p>
      </div>
      <div v-if="visibleSpeakers.length" class="ms-card ms-meeting-card">
        <div class="ms-card-head"><h2>说话人</h2></div>
        <div
          class="ms-participant-live-list"
          tabindex="0"
          aria-label="会中说话人列表"
        >
          <div
            v-for="visibleSpeaker in visibleSpeakers"
            :key="visibleSpeaker.key"
          >
            <span class="ms-avatar" aria-hidden="true">{{
              visibleSpeaker.label.slice(-1)
            }}</span>
            <span
              ><strong>{{ visibleSpeaker.label }}</strong
              ><small>{{
                isRecognizedSpeaker(visibleSpeaker.label)
                  ? '已识别为正式成员'
                  : '等待识别或人工校对'
              }}</small></span
            >
          </div>
        </div>
        <p class="ms-help ms-participant-help">
          低置信度结果显示为未知说话人，不会强行关联正式成员。
        </p>
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
            :disabled="agent.retrying"
            :aria-busy="agent.retrying"
            @click="agent.retry()"
          >
            {{ agent.retrying ? '正在检测' : '重新检测' }}
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
