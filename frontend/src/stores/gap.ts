import { defineStore } from 'pinia'

import {
  GetGapConflict,
  GetGapState,
  ResolveGapConflict,
  RetryGapCompensation,
  StartGapCompensation,
  StopGapCompensation,
} from '../../wailsjs/go/wails/GapBinding'

import type { Step8EventEnvelope } from './finalization'

export type GapAggregateState =
  'none' | 'pending' | 'processing' | 'failed' | 'conflict' | 'completed'

export type GapResolution =
  'keep_existing' | 'use_file_text' | 'save_manual_text'

export interface GapItem {
  id: string
  start_sample: number
  end_sample: number
  state: string
  attempt_count: number
  error_code?: string
}

export interface ConflictUtterance {
  id: string
  seq: number
  original_text: string
  current_text: string
  start_sample: number
  end_sample: number
  text_revision: number
}

export interface GapCandidate {
  text: string
  speaker_id?: string
  start_sample: number
  end_sample: number
}

export interface GapConflict {
  gap_id: string
  revision: number
  core_start_sample: number
  core_end_sample: number
  audio_start_sample: number
  audio_end_sample: number
  audio_clip_url: string
  audio_clip_expires_at: number
  candidates: GapCandidate[]
  existing: ConflictUtterance[]
  context: ConflictUtterance[]
}

/** useGapStore 管理补转写状态和主持人明确提交的冲突解决。 */
export const useGapStore = defineStore('gap', {
  state: () => ({
    meetingID: '',
    state: 'none' as GapAggregateState,
    gaps: [] as GapItem[],
    activeAttemptID: '',
    revision: 0,
    conflict: null as GapConflict | null,
    loading: false,
    submitting: false,
    errorMessage: '',
  }),
  getters: {
    failedGapIDs: (state) =>
      state.gaps
        .filter((item) => item.state === 'failed')
        .map((item) => item.id),
    conflictGap: (state) =>
      state.gaps.find((item) => item.state === 'conflict') ?? null,
  },
  actions: {
    /** refresh 从 SQLite 重建聚合、明细和活动 attempt。 */
    async refresh(meetingID: string): Promise<void> {
      if (!meetingID) return
      this.loading = true
      const result = await GetGapState(meetingID)
      this.loading = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return
      }
      this.meetingID = meetingID
      this.state = result.data.state as GapAggregateState
      this.gaps = result.data.gaps ?? []
      this.activeAttemptID = result.data.active_attempt_id ?? ''
      this.revision = result.data.revision
    },
    /** start 使用新的显式请求 ID 启动首轮补转写。 */
    async start(meetingID: string): Promise<boolean> {
      return this.runCommand(meetingID, () =>
        StartGapCompensation(meetingID, crypto.randomUUID()),
      )
    },
    /** retryFailed 只提交当前明确失败的 gap，不自动重复计费。 */
    async retryFailed(meetingID: string): Promise<boolean> {
      const ids = this.failedGapIDs
      if (!ids.length) return false
      return this.runCommand(meetingID, () =>
        RetryGapCompensation(meetingID, ids, crypto.randomUUID()),
      )
    },
    /** stop 取消当前活动 attempt，终态仍由后端持久化。 */
    async stop(meetingID: string): Promise<boolean> {
      if (!this.activeAttemptID) return false
      return this.runCommand(meetingID, () =>
        StopGapCompensation(meetingID, this.activeAttemptID),
      )
    },
    /** loadConflict 获取实时 current/original、文件候选和短期音频 URL。 */
    async loadConflict(meetingID: string, gapID: string): Promise<boolean> {
      this.loading = true
      const result = await GetGapConflict(meetingID, gapID)
      this.loading = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return false
      }
      this.meetingID = meetingID
      this.conflict = {
        ...result.data,
        candidates: result.data.candidates ?? [],
        existing: result.data.existing ?? [],
        context: result.data.context ?? [],
      }
      return true
    },
    /** resolveConflict 逐条携带目标 revision，服务端负责二次冲突检测。 */
    async resolveConflict(
      resolution: GapResolution,
      texts: Record<string, string>,
    ): Promise<boolean> {
      const conflict = this.conflict
      if (!conflict || this.submitting) return false
      const edits =
        resolution === 'keep_existing'
          ? []
          : conflict.existing.map((item) => ({
              target_id: item.id,
              expected_revision: item.text_revision,
              text: texts[item.id]?.trim() ?? '',
            }))
      this.submitting = true
      this.errorMessage = ''
      const result = await ResolveGapConflict(
        this.meetingID,
        conflict.gap_id,
        conflict.revision,
        resolution,
        edits,
        crypto.randomUUID(),
      )
      this.submitting = false
      if (result.code !== 200) {
        this.errorMessage = result.message
        await this.loadConflict(this.meetingID, conflict.gap_id)
        return false
      }
      this.conflict = null
      await this.refresh(this.meetingID)
      return true
    },
    /** applyEvent 只接受新 revision，并提示调用方重新读取 SQLite。 */
    applyEvent(event: Step8EventEnvelope): boolean {
      const data = event.data
      if (
        event.version !== 1 ||
        !data ||
        data.meeting_id !== this.meetingID ||
        data.revision <= this.revision
      )
        return false
      this.revision = data.revision
      this.state = data.state as GapAggregateState
      return true
    },
    /** runCommand 统一处理后台命令结果并恢复事实状态。 */
    async runCommand(
      meetingID: string,
      command: () => Promise<{ code: number; message: string }>,
    ): Promise<boolean> {
      if (this.submitting) return false
      this.submitting = true
      this.errorMessage = ''
      const result = await command()
      this.submitting = false
      if (result.code !== 200) {
        this.errorMessage = result.message
        return false
      }
      await this.refresh(meetingID)
      return true
    },
  },
})
