import { defineStore } from 'pinia'

import {
  AskAgent,
  GetAgentSettings,
  GetAgentState,
  GetAgentTimeline,
  InterruptAgent,
  ProbeAgent,
  RespondAgentApproval,
  RetryAgent,
  SaveAgentSettings,
  StartWakeWordTest,
  StopWakeWordTest,
} from '../../wailsjs/go/wails/AgentBinding'

export interface AgentApproval {
  id: string
  tool: string
  target: string
  parameter_summary: string
  risk: string
}

export interface AgentRuntimeProjection {
  state: string
  meeting_id: string
  turn_id: string
  partial: string
  approval?: AgentApproval
  error_code: string
  revision: number
}

export interface AgentTimelineEntry {
  seq: number
  kind: string
  occurred_at: number
  turn_id: string
  text?: string
  reason?: string
}

export interface AgentEventEnvelope {
  data?: {
    meeting_id: string
    turn_id: string
    type: string
    delta?: string
    approval?: AgentApproval
    error_code?: string
    revision: number
  }
}

export interface WakeTestEnvelope {
  data?: {
    state: string
    matched: number
    required: number
    asr_state: string
    error_code?: string
  }
}

/** normalizeSettings 补齐 Wails 可选字段，保持 Pinia 状态形状稳定。 */
function normalizeSettings(value: {
  wake_word: string
  codex_executable_path: string
  updated_at: number
  availability: {
    state: string
    version?: string
    account_state: string
    protocol_state: string
    message: string
  }
}) {
  return {
    ...value,
    availability: {
      ...value.availability,
      version: value.availability.version ?? '',
    },
  }
}

/** normalizeWakeTest 补齐可选错误码。 */
function normalizeWakeTest(value: {
  state: string
  matched: number
  required: number
  asr_state: string
  error_code?: string
}) {
  return { ...value, error_code: value.error_code ?? '' }
}

const emptyRuntime = (): AgentRuntimeProjection => ({
  state: 'unchecked',
  meeting_id: '',
  turn_id: '',
  partial: '',
  error_code: '',
  revision: 0,
})

/** useAgentStore 只保存可由 SQLite 或 GetAgentState 重建的 AI 页面投影。 */
export const useAgentStore = defineStore('agent', {
  state: () => ({
    meetingID: '',
    runtime: emptyRuntime(),
    timeline: [] as AgentTimelineEntry[],
    settings: {
      wake_word: 'AI 助手',
      codex_executable_path: '',
      updated_at: 0,
      availability: {
        state: 'unchecked',
        version: '',
        account_state: 'unknown',
        protocol_state: 'unchecked',
        message: '尚未检测',
      },
    },
    wakeTest: {
      state: 'idle',
      matched: 0,
      required: 3,
      asr_state: 'idle',
      error_code: '',
    },
    loading: false,
    saving: false,
    probing: false,
    retrying: false,
    asking: false,
    errorMessage: '',
    notice: '',
  }),
  getters: {
    latestSeq: (state) =>
      state.timeline.reduce((latest, entry) => Math.max(latest, entry.seq), 0),
    busy: (state) => state.runtime.state === 'busy',
  },
  actions: {
    /** loadSettings 读取唤醒词和脱敏可用性。 */
    async loadSettings(): Promise<void> {
      this.loading = true
      const result = await GetAgentSettings()
      this.loading = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return
      }
      this.settings = normalizeSettings(result.data)
    },
    /** saveSettings 保存两个明确字段，不保存账号或权限。 */
    async saveSettings(
      wakeWord: string,
      executablePath: string,
    ): Promise<boolean> {
      this.saving = true
      this.errorMessage = ''
      this.notice = ''
      const result = await SaveAgentSettings({
        wake_word: wakeWord,
        codex_executable_path: executablePath,
      })
      this.saving = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return false
      }
      this.settings = normalizeSettings(result.data)
      this.notice = 'Codex 设置已保存'
      return true
    },
    /** probe 真实检查 executable、schema、握手和登录状态。 */
    async probe(): Promise<boolean> {
      this.probing = true
      this.errorMessage = ''
      const result = await ProbeAgent()
      this.probing = false
      if (result.data)
        this.settings.availability = {
          ...result.data,
          version: result.data.version ?? '',
        }
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return false
      }
      return true
    },
    /** refreshState 从内存态或 SQLite 恢复当前会议状态。 */
    async refreshState(meetingID: string): Promise<void> {
      if (!meetingID) return
      if (this.meetingID !== meetingID) {
        this.meetingID = meetingID
        this.timeline = []
      }
      const result = await GetAgentState(meetingID)
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return
      }
      this.runtime = { ...emptyRuntime(), ...result.data }
    },
    /** restoreTimeline 按统一 seq 增量补拉持久 AI 事件。 */
    async restoreTimeline(meetingID: string): Promise<void> {
      if (!meetingID) return
      if (this.meetingID !== meetingID) {
        this.meetingID = meetingID
        this.timeline = []
      }
      const result = await GetAgentTimeline(meetingID, this.latestSeq, 200)
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return
      }
      const known = new Set(this.timeline.map((entry) => entry.seq))
      for (const entry of result.data as AgentTimelineEntry[]) {
        if (!known.has(entry.seq)) this.timeline.push(entry)
      }
      this.timeline.sort((left, right) => left.seq - right.seq)
    },
    /** ask 提交主持人问题；持续状态由 Wails 事件即时更新。 */
    async ask(question: string): Promise<boolean> {
      if (!this.meetingID || this.asking || this.busy) return false
      this.asking = true
      this.errorMessage = ''
      const requestID = crypto.randomUUID()
      const result = await AskAgent(this.meetingID, question, requestID)
      this.asking = false
      if (result.code !== 200) {
        this.errorMessage = result.message
        await this.refreshState(this.meetingID)
        return false
      }
      await Promise.all([
        this.refreshState(this.meetingID),
        this.restoreTimeline(this.meetingID),
      ])
      return true
    },
    /** interrupt 停止当前本地 turn。 */
    async interrupt(): Promise<void> {
      if (!this.meetingID || !this.runtime.turn_id) return
      const result = await InterruptAgent(this.meetingID, this.runtime.turn_id)
      if (result.code !== 200) this.errorMessage = result.message
    },
    /** respondApproval 只提交 allow/decline，不提供持久授权。 */
    async respondApproval(decision: 'allow' | 'decline'): Promise<void> {
      const approval = this.runtime.approval
      if (!approval || !this.meetingID || !this.runtime.turn_id) return
      const result = await RespondAgentApproval(
        this.meetingID,
        this.runtime.turn_id,
        approval.id,
        decision,
      )
      if (result.code !== 200) this.errorMessage = result.message
      else this.runtime.approval = undefined
    },
    /** retry 让不可用会议优先恢复原 thread。 */
    async retry(): Promise<void> {
      if (!this.meetingID || this.retrying) return
      this.retrying = true
      this.errorMessage = ''
      try {
        const result = await RetryAgent(this.meetingID)
        if (result.code !== 200) this.errorMessage = result.message
        await this.refreshState(this.meetingID)
      } finally {
        this.retrying = false
      }
    },
    /** startWakeTest 启动真实 ASR 三次测试。 */
    async startWakeTest(): Promise<void> {
      const result = await StartWakeWordTest()
      if (result.code !== 200 || !result.data)
        this.errorMessage = result.message
      else this.wakeTest = normalizeWakeTest(result.data)
    },
    /** stopWakeTest 停止并等待麦克风和 ASR 释放。 */
    async stopWakeTest(): Promise<void> {
      const result = await StopWakeWordTest()
      if (result.code !== 200 || !result.data)
        this.errorMessage = result.message
      else this.wakeTest = normalizeWakeTest(result.data)
    },
    /** applyEvent 按 revision 合并 delta，并在终态清除 partial。 */
    applyEvent(event: AgentEventEnvelope): void {
      const data = event.data
      if (
        !data ||
        data.meeting_id !== this.meetingID ||
        data.revision <= this.runtime.revision
      )
        return
      this.runtime.revision = data.revision
      this.runtime.turn_id = data.turn_id
      if (data.type === 'answer_delta') {
        this.runtime.state = 'busy'
        this.runtime.partial += data.delta ?? ''
      } else if (data.type === 'approval_requested') {
        this.runtime.state = 'busy'
        this.runtime.approval = data.approval
      } else if (['completed', 'cancelled', 'failed'].includes(data.type)) {
        this.runtime.partial = ''
        this.runtime.approval = undefined
        this.runtime.error_code = data.error_code ?? ''
        this.runtime.state =
          data.type === 'completed'
            ? 'available'
            : data.type === 'cancelled'
              ? 'available'
              : 'unavailable'
      }
    },
    /** applyWakeTestEvent 更新独立于 Codex 的真实 ASR 测试进度。 */
    applyWakeTestEvent(event: WakeTestEnvelope): void {
      if (event.data)
        this.wakeTest = {
          ...event.data,
          error_code: event.data.error_code ?? '',
        }
    },
  },
})
