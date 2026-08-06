import { defineStore } from 'pinia'
import { wails } from '../../wailsjs/go/models'

import {
  GetASRSettings,
  GetASRTimeline,
  GetRawRecordState,
  RetryRealtimeASR,
  SaveASRSettings,
  TestASRConnection,
} from '../../wailsjs/go/wails/ASRBinding'

export interface ASRSettingsProjection {
  api_key_configured: boolean
  api_key_mask: string
  requires_api_key_upgrade: boolean
  updated_at: number
}

export interface ASRTimelineEntry {
  seq: number
  kind: 'utterance.final' | 'asr.gap'
  occurred_at: number
  start_sample: number
  end_sample: number
  text?: string
  speaker_label?: string
  session_order?: number
  gap_reason?: string
}

export interface ASRPartial {
  meeting_id: string
  session_id: string
  generation: number
  result_id: string
  revision: number
  text: string
  start_sample: number
  end_sample: number
}

export interface ASRPartialClear {
  meeting_id: string
  session_id: string
  generation: number
  result_id?: string
}

interface AppEvent<T> {
  data: T
}

const emptySettings: ASRSettingsProjection = {
  api_key_configured: false,
  api_key_mask: '',
  requires_api_key_upgrade: false,
  updated_at: 0,
}

/** useASRStore 保存可由 SQLite 快照恢复的设置、Timeline 与瞬时 partial。 */
export const useASRStore = defineStore('asr', {
  state: () => ({
    settings: { ...emptySettings },
    timeline: [] as ASRTimelineEntry[],
    partials: {} as Record<string, ASRPartial>,
    clearedPartialSessions: {} as Record<string, number>,
    clearedPartialResults: {} as Record<string, number>,
    meetingID: '',
    realtimeState: 'idle',
    realtimeErrorCode: '',
    loading: false,
    saving: false,
    probing: false,
    retrying: false,
    errorMessage: '',
    notice: '',
    probeLatencyMS: 0,
    rawRecordState: 'idle',
    rawRecordErrorCode: '',
  }),
  getters: {
    /** apiKeyReady 表示已保存实时与补录共同使用的 APP Key。 */
    apiKeyReady: (state): boolean => state.settings.api_key_configured,
    /** latestSeq 返回当前已恢复的持久事件游标。 */
    latestSeq: (state): number =>
      state.timeline.reduce((latest, entry) => Math.max(latest, entry.seq), 0),
    /** orderedPartials 返回按样本位置排序的会中临时结果。 */
    orderedPartials: (state): ASRPartial[] =>
      Object.values(state.partials).sort(
        (left, right) => left.start_sample - right.start_sample,
      ),
  },
  actions: {
    /** loadSettings 从后端读取掩码投影，绝不接收凭证明文。 */
    async loadSettings(): Promise<void> {
      this.loading = true
      this.errorMessage = ''
      const result = await GetASRSettings()
      this.loading = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return
      }
      this.settings = result.data as ASRSettingsProjection
    },
    /** saveAPIKey 用非空草稿替换 APP Key，空草稿保留已保存值。 */
    async saveAPIKey(apiKey: string): Promise<boolean> {
      this.saving = true
      this.errorMessage = ''
      this.notice = ''
      const result = await SaveASRSettings(
        wails.SaveASRSettingsDTO.createFrom({
          api_key: credentialChange(apiKey),
        }),
      )
      this.saving = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return false
      }
      this.settings = result.data as ASRSettingsProjection
      this.notice = 'APP Key 已保存'
      return true
    },
    /** clearAPIKey 明确删除实时转写与补录共同使用的 APP Key。 */
    async clearAPIKey(): Promise<boolean> {
      this.saving = true
      this.errorMessage = ''
      this.notice = ''
      const result = await SaveASRSettings(
        wails.SaveASRSettingsDTO.createFrom({
          api_key: { action: 'clear', value: '' },
        }),
      )
      this.saving = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return false
      }
      this.settings = result.data as ASRSettingsProjection
      this.notice = '已清除 APP Key'
      return true
    },
    /** testAPIKeyConnection 使用未保存 APP Key 探测连接，不发送真实音频。 */
    async testAPIKeyConnection(apiKey: string): Promise<boolean> {
      this.probing = true
      this.errorMessage = ''
      this.notice = ''
      const result = await TestASRConnection({
        api_key: apiKey.trim(),
      })
      this.probing = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return false
      }
      this.probeLatencyMS = result.data.latency_ms
      this.notice = '连接已建立；本次未发送真实音频'
      return true
    },
    /** restoreTimeline 按 seq 增量补拉 final/gap，页面刷新后不依赖 Pinia 旧内存。 */
    async restoreTimeline(meetingID: string): Promise<void> {
      if (!meetingID) return
      if (this.meetingID !== meetingID) {
        this.meetingID = meetingID
        this.timeline = []
        this.partials = {}
        this.clearedPartialSessions = {}
        this.clearedPartialResults = {}
      }
      const result = await GetASRTimeline(meetingID, this.latestSeq, 200)
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return
      }
      const known = new Set(this.timeline.map((entry) => entry.seq))
      for (const entry of result.data as ASRTimelineEntry[]) {
        if (!known.has(entry.seq)) this.timeline.push(entry)
      }
      this.timeline.sort((left, right) => left.seq - right.seq)
      const lastFinalEnd = this.timeline
        .filter((entry) => entry.kind === 'utterance.final')
        .reduce((end, entry) => Math.max(end, entry.end_sample), 0)
      for (const [resultID, partial] of Object.entries(this.partials)) {
        if (partial.end_sample <= lastFinalEnd) delete this.partials[resultID]
      }
      await this.refreshRawRecordState()
    },
    /** refreshRawRecordState 读取文件投影状态，失败时持续显示而不伪装已刷新。 */
    async refreshRawRecordState(): Promise<void> {
      if (!this.meetingID) return
      const result = await GetRawRecordState(this.meetingID)
      if (result.code !== 200 || !result.data) return
      this.rawRecordState = result.data.state
      this.rawRecordErrorCode = result.data.error_code ?? ''
    },
    /** applyPartial 按物理 session 接受更高 revision，不写入持久 Timeline。 */
    applyPartial(event: AppEvent<ASRPartial>): void {
      const partial = event.data
      if (!partial || partial.meeting_id !== this.meetingID) return
      const key = partialKey(partial.session_id, partial.result_id)
      if (
        (this.clearedPartialSessions[partial.session_id] ?? -1) >=
        partial.generation
      )
        return
      if ((this.clearedPartialResults[key] ?? -1) >= partial.generation) return
      const previous = this.partials[key]
      if (previous && previous.revision >= partial.revision) return
      this.partials[key] = partial
    },
    /** applyPartialClear 清除 session/result，并阻止旧 generation 的迟到 partial 复活。 */
    applyPartialClear(event: AppEvent<ASRPartialClear>): void {
      const cleared = event.data
      if (!cleared || cleared.meeting_id !== this.meetingID) return
      if (cleared.result_id) {
        const key = partialKey(cleared.session_id, cleared.result_id)
        delete this.partials[key]
        this.clearedPartialResults[key] = Math.max(
          this.clearedPartialResults[key] ?? -1,
          cleared.generation,
        )
        return
      }
      for (const [key, partial] of Object.entries(this.partials)) {
        if (partial.session_id === cleared.session_id) delete this.partials[key]
      }
      this.clearedPartialSessions[cleared.session_id] = Math.max(
        this.clearedPartialSessions[cleared.session_id] ?? -1,
        cleared.generation,
      )
    },
    /** applyRealtimeState 更新独立实时转写状态，状态变化后由页面补拉持久事件。 */
    applyRealtimeState(
      event: AppEvent<{
        meeting_id: string
        state: string
        error_code?: string
      }>,
    ): void {
      if (!event.data || event.data.meeting_id !== this.meetingID) return
      this.realtimeState = event.data.state
      this.realtimeErrorCode = event.data.error_code ?? ''
    },
    /** retryRealtime 对不可用状态执行一次用户主动重试。 */
    async retryRealtime(): Promise<boolean> {
      if (!this.meetingID) return false
      this.retrying = true
      this.errorMessage = ''
      const result = await RetryRealtimeASR(this.meetingID)
      this.retrying = false
      if (result.code !== 200 || result.data !== true) {
        this.errorMessage = result.message
        return false
      }
      return true
    },
  },
})

/** credentialChange 将空草稿解释为保留，避免掩码被误写回数据库。 */
function credentialChange(value: string): { action: string; value: string } {
  const trimmed = value.trim()
  return trimmed
    ? { action: 'replace', value: trimmed }
    : { action: 'keep', value: '' }
}

/** partialKey 返回跨物理 session 唯一的临时转写键。 */
function partialKey(sessionID: string, resultID: string): string {
  return `${sessionID}:${resultID}`
}
