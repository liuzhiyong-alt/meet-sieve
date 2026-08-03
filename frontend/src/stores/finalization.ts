import { defineStore } from 'pinia'

import {
  GetFinalizationState,
  RetryAgentFinalSync,
  RetryFinalization,
} from '../../wailsjs/go/wails/FinalizationBinding'

export type FinalizationPhase = 'idle' | 'running' | 'completed' | 'failed'

export interface FinalizationProjection {
  meeting_id: string
  state: FinalizationPhase
  stage: string
  error_code: string
  revision: number
}

export interface Step8EventEnvelope {
  version?: number
  data?: {
    meeting_id: string
    state: string
    stage?: string
    error_code?: string
    revision: number
  }
}

const emptyState = (): FinalizationProjection => ({
  meeting_id: '',
  state: 'idle',
  stage: '',
  error_code: '',
  revision: 0,
})

/** useFinalizationStore 保存可由 runtime/SQLite 重建的核心收尾投影。 */
export const useFinalizationStore = defineStore('finalization', {
  state: () => ({
    projection: emptyState(),
    loading: false,
    retrying: false,
    syncRetrying: false,
    errorMessage: '',
  }),
  actions: {
    /** refresh 始终以 Get binding 恢复真实收尾状态。 */
    async refresh(meetingID: string): Promise<void> {
      if (!meetingID) return
      this.loading = true
      const result = await GetFinalizationState(meetingID)
      this.loading = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return
      }
      this.projection = {
        meeting_id: result.data.meeting_id,
        state: result.data.state as FinalizationPhase,
        stage: result.data.stage,
        error_code: result.data.error_code ?? '',
        revision: result.data.revision,
      }
    },
    /** retry 复用后端唯一 EndMeeting owner，不在前端猜测缺失步骤。 */
    async retry(meetingID: string): Promise<boolean> {
      if (!meetingID || this.retrying) return false
      this.retrying = true
      this.errorMessage = ''
      const result = await RetryFinalization(meetingID)
      this.retrying = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        await this.refresh(meetingID)
        return false
      }
      await this.refresh(meetingID)
      return true
    },
    /** retryFinalSync 只重试独立 Codex 结束同步，不改变本地保存状态。 */
    async retryFinalSync(meetingID: string): Promise<boolean> {
      if (!meetingID || this.syncRetrying) return false
      this.syncRetrying = true
      this.errorMessage = ''
      const result = await RetryAgentFinalSync(meetingID, crypto.randomUUID())
      this.syncRetrying = false
      if (result.code !== 200) {
        this.errorMessage = result.message
        return false
      }
      return true
    },
    /** applyEvent 按会议和 revision 去重，并由调用方重新读取事实源。 */
    applyEvent(event: Step8EventEnvelope): boolean {
      const data = event.data
      if (
        event.version !== 1 ||
        !data ||
        data.meeting_id !== this.projection.meeting_id ||
        data.revision <= this.projection.revision
      )
        return false
      this.projection = {
        meeting_id: data.meeting_id,
        state: data.state as FinalizationPhase,
        stage: data.stage ?? '',
        error_code: data.error_code ?? '',
        revision: data.revision,
      }
      return true
    },
  },
})
