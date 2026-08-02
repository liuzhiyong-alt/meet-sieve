<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { EventsOn } from '../../../wailsjs/runtime/runtime'

import { useMeetingStore } from '../../stores/meeting'
import { useASRStore } from '../../stores/asr'

const meeting = useMeetingStore()
const asr = useASRStore()
const emit = defineEmits<{ openCorrection: [] }>()
const confirmEnd = ref(false)
const now = ref(Date.now())
let refreshTicks = 0
let stopPartialListener: (() => void) | undefined
let stopStateListener: (() => void) | undefined
const timer = window.setInterval(() => {
  now.value = Date.now()
  refreshTicks++
  // 查询是事件丢失后的事实重同步，也能及时显示设备断开产生的 interrupted。
  if (refreshTicks % 2 === 0 && !meeting.saving) {
    void meeting.refreshCurrentMeeting()
    if (meeting.current?.id) void asr.restoreTimeline(meeting.current.id)
  }
}, 1000)

onMounted(async () => {
  const meetingID = meeting.current?.id
  if (meetingID) {
    asr.realtimeState = meeting.current?.realtime_asr_state || 'idle'
    await asr.restoreTimeline(meetingID)
  }
  stopPartialListener = EventsOn('meeting.asr.partial', (event) => {
    asr.applyPartial(event)
  })
  stopStateListener = EventsOn('meeting.asr.changed', (event) => {
    asr.applyRealtimeState(event)
    if (meeting.current?.id) void asr.restoreTimeline(meeting.current.id)
  })
})

onBeforeUnmount(() => {
  window.clearInterval(timer)
  stopPartialListener?.()
  stopStateListener?.()
})

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
const gaps = computed(() =>
  asr.timeline.filter((entry) => entry.kind === 'asr.gap'),
)

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

/** endMeeting 关闭确认框并等待唯一后端收尾结果。 */
async function endMeeting(): Promise<void> {
  confirmEnd.value = false
  await meeting.endMeeting()
}
</script>

<template>
  <section
    v-if="projection"
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

  <div
    v-if="['reconnecting', 'unavailable'].includes(asrState)"
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
    v-if="asr.rawRecordState === 'failed'"
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

  <section v-if="projection" class="ms-meeting-split ms-live-layout">
    <div class="ms-card ms-meeting-card">
      <p class="ms-eyebrow">会议时间线</p>
      <h2 v-if="isInterrupted">现有录音材料已保留</h2>
      <h2 v-else-if="isFinalizing">正在完成会议记录</h2>
      <h2 v-else-if="isEnded">完整录音已安全保存</h2>
      <h2 v-else>会议正在录音</h2>
      <p v-if="isInterrupted || isEnded || isFinalizing" class="ms-lead">
        {{
          isInterrupted
            ? '可以重试文件对账和合并，不能在原会议上续录。'
            : '录音按 60 秒精确分片，并以 2 秒检查点同步到本地磁盘。'
        }}
      </p>
      <ol v-if="isFinalizing" class="ms-finalizing-list" aria-live="polite">
        <li class="is-complete">麦克风已停止</li>
        <li class="is-current">等待尾部转写，最多 15 秒</li>
        <li>合并并校验本地录音</li>
        <li>刷新会议原始记录</li>
      </ol>
      <div
        v-else-if="asr.timeline.length || asr.orderedPartials.length"
        class="ms-transcript-list"
        aria-live="polite"
      >
        <article
          v-for="entry in asr.timeline"
          :key="entry.seq"
          class="ms-transcript-entry"
          :class="{ 'is-gap': entry.kind === 'asr.gap' }"
        >
          <time>{{ sampleTime(entry.start_sample) }}</time>
          <div v-if="entry.kind === 'utterance.final'">
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
          @click="meeting.startNewMeeting()"
        >
          开始新会议
        </button>
      </div>
      <div v-if="isEnded && gaps.length" class="ms-gap-summary">
        <strong>存在 {{ gaps.length }} 段转写缺口</strong>
        <p>本地录音已保存。后续文件补转写会基于这些精确范围处理。</p>
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
          <li><span>Codex</span><strong>未启用</strong></li>
          <li><span>访客页</span><strong>未启用</strong></li>
        </ul>
      </div>
    </aside>
  </section>

  <Teleport to="body">
    <div
      v-if="confirmEnd"
      class="ms-modal-backdrop"
      @click.self="confirmEnd = false"
    >
      <section
        class="ms-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="end-meeting-title"
      >
        <h2 id="end-meeting-title">结束并保存会议？</h2>
        <p class="ms-lead">将停止麦克风、关闭当前分片、合并并校验完整录音。</p>
        <div class="ms-actions ms-modal-actions">
          <button
            class="ms-button ms-button--quiet"
            type="button"
            autofocus
            @click="confirmEnd = false"
          >
            继续会议
          </button>
          <button
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

  <Teleport
    v-if="projection && !isInterrupted && !isEnded"
    defer
    to="#meeting-titlebar-actions"
  >
    <button
      class="ms-button ms-button--danger"
      type="button"
      :disabled="meeting.saving"
      @click="confirmEnd = true"
    >
      {{ isFinalizing ? '正在保存' : '结束会议' }}
    </button>
  </Teleport>
</template>
