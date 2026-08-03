<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted } from 'vue'
import { EventsOn } from '../../../wailsjs/runtime/runtime'

import {
  useFinalizationStore,
  type Step8EventEnvelope,
} from '../../stores/finalization'

const props = defineProps<{
  meetingId: string
  meetingNo: string
  recordingState: string
  localSaveState: string
  asrState: string
  lanState: string
  agentState: string
}>()
const finalization = useFinalizationStore()
let stopListener: (() => void) | undefined

const steps = [
  {
    stage: 'stop_lan',
    title: '停止访客访问',
    detail: '局域网入口和会议令牌失效',
  },
  {
    stage: 'stop_capture',
    title: '停止录音采集',
    detail: '安全关闭最后一个 WAV 分片',
  },
  {
    stage: 'wait_tail_final',
    title: '等待尾部转写',
    detail: '保存尾部 final，并登记未覆盖范围',
  },
  {
    stage: 'merge_recording',
    title: '合并并校验录音',
    detail: '生成并核对 recording.wav',
  },
  {
    stage: 'flush_raw_record',
    title: '刷新会议原始记录',
    detail: '从 SQLite 重建 Markdown 投影',
  },
  {
    stage: 'commit_local_saved',
    title: '确认本地已保存',
    detail: '提交会议本地保存终态',
  },
]

const stageIndex = computed(() => {
  if (finalization.projection.stage === 'persist_transcript') return 2
  return Math.max(
    0,
    steps.findIndex((item) => item.stage === finalization.projection.stage),
  )
})
const completedCount = computed(() => {
  if (finalization.projection.state === 'completed') return steps.length
  return Math.max(0, stageIndex.value)
})
const failed = computed(() => finalization.projection.state === 'failed')

onMounted(async () => {
  await finalization.refresh(props.meetingId)
  stopListener = EventsOn(
    'meeting.finalization.changed',
    (event: Step8EventEnvelope) => {
      if (finalization.applyEvent(event))
        void finalization.refresh(props.meetingId)
    },
  )
})
onBeforeUnmount(() => stopListener?.())

/** stepState 返回单个 OperationStep 的稳定视觉状态。 */
function stepState(
  index: number,
): 'complete' | 'current' | 'failed' | 'waiting' {
  if (index < stageIndex.value || finalization.projection.state === 'completed')
    return 'complete'
  if (index === stageIndex.value) return failed.value ? 'failed' : 'current'
  return 'waiting'
}

/** stateLabel 映射独立状态轴，避免把外部同步与本地保存合并。 */
function stateLabel(axis: string, value: string): string {
  const labels: Record<string, Record<string, string>> = {
    recording: { finalizing: '已停止', ended: '已停止', interrupted: '已中断' },
    save: { saving: '正在保存', saved: '已保存', failed: '保存失败' },
    asr: { stopped: '已停止', unavailable: '已停止', reconnecting: '正在收尾' },
    lan: { stopped: '已停止', disabled: '未启用', failed: '已停止' },
    agent: {
      available: '等待结束同步',
      busy: '正在结束同步',
      unsynced: '结束同步失败',
      unchecked: '未启用',
      unavailable: '已关闭',
    },
  }
  return labels[axis]?.[value] ?? '状态待确认'
}
</script>

<template>
  <section class="ms-finalization-stage" aria-labelledby="finalization-title">
    <div class="ms-finalization-stage__head">
      <div>
        <p class="ms-eyebrow">{{ meetingNo }}</p>
        <h1 id="finalization-title">
          {{ failed ? '会议收尾未完成' : '正在结束并保存会议' }}
        </h1>
        <p class="ms-lead">
          {{
            failed
              ? '录音分片和会议事件仍在本地。完整录音尚未通过校验，不能标记为本地已保存。'
              : '访客入口和录音已停止。本地保存完成后会自动进入会后处理。'
          }}
        </p>
      </div>
      <span class="ms-status-pill" :class="failed ? 'is-danger' : 'is-warning'">
        {{ failed ? '需要处理' : '请保持应用打开' }}
      </span>
    </div>
    <div
      class="ms-progress"
      role="progressbar"
      aria-label="会议收尾进度"
      aria-valuemin="0"
      :aria-valuemax="steps.length"
      :aria-valuenow="completedCount"
    >
      <span :style="{ width: `${(completedCount / steps.length) * 100}%` }" />
    </div>
    <span class="ms-help"
      >已完成 {{ completedCount }} / {{ steps.length }} 项</span
    >
  </section>

  <p
    v-if="finalization.errorMessage"
    class="ms-notice ms-notice--danger"
    role="alert"
  >
    {{ finalization.errorMessage }}
  </p>
  <section class="ms-step8-operation-layout">
    <article class="ms-card ms-step8-card">
      <div v-if="failed" class="ms-notice ms-notice--danger" role="alert">
        <div>
          <strong>本地安全收尾失败</strong>
          <p>
            已关闭的录音分片仍然保留。补转写、Codex 结束同步和纪要生成尚未开始。
          </p>
        </div>
      </div>
      <div class="ms-operation-list" aria-live="polite">
        <div
          v-for="(step, index) in steps"
          :key="step.stage"
          class="ms-operation-step"
          :class="`is-${stepState(index)}`"
          :aria-current="stepState(index) === 'current' ? 'step' : undefined"
        >
          <div class="ms-operation-step__main">
            <span class="ms-operation-step__index">{{ index + 1 }}</span>
            <div class="ms-operation-step__copy">
              <strong>{{ step.title }}</strong
              ><span>{{ step.detail }}</span>
            </div>
          </div>
          <span class="ms-status-pill" :class="`is-${stepState(index)}`">
            {{
              stepState(index) === 'complete'
                ? '已完成'
                : stepState(index) === 'current'
                  ? '处理中'
                  : stepState(index) === 'failed'
                    ? '失败'
                    : '等待'
            }}
          </span>
        </div>
      </div>
      <div v-if="failed" class="ms-actions ms-step8-actions">
        <button
          class="ms-button ms-button--primary"
          type="button"
          :disabled="finalization.retrying"
          @click="finalization.retry(meetingId)"
        >
          {{ finalization.retrying ? '正在重试…' : '重试本地保存' }}
        </button>
        <span class="ms-help"
          >错误码：{{
            finalization.projection.error_code || 'FINALIZATION_FAILED'
          }}</span
        >
      </div>
    </article>
    <aside class="ms-card ms-step8-card">
      <div class="ms-card-head"><h2>当前状态</h2></div>
      <ul class="ms-status-list">
        <li>
          <span>录音</span
          ><strong>{{ stateLabel('recording', recordingState) }}</strong>
        </li>
        <li>
          <span>本地保存</span
          ><strong>{{ stateLabel('save', localSaveState) }}</strong>
        </li>
        <li>
          <span>实时转写</span
          ><strong>{{ stateLabel('asr', asrState) }}</strong>
        </li>
        <li>
          <span>访客页</span><strong>{{ stateLabel('lan', lanState) }}</strong>
        </li>
        <li>
          <span>Codex</span
          ><strong>{{ stateLabel('agent', agentState) }}</strong>
        </li>
      </ul>
      <div class="ms-notice ms-notice--warning">
        <span>外部补转写和 Codex 同步不会阻塞本地保存。</span>
      </div>
    </aside>
  </section>
</template>
