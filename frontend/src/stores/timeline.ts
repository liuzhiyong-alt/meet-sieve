import { defineStore } from 'pinia'

import {
  chooseAndSendMeetingAttachment,
  getLiveMeetingStatus,
  getMeetingTimeline,
  sendMeetingMessage,
  type LiveMeetingStatus,
  type TimelineEntry,
} from '../features/meeting/contentApi'

export interface TimelinePartial {
  meeting_id: string
  session_id: string
  generation: number
  result_id: string
  revision: number
  text: string
  start_sample: number
  end_sample: number
}

export interface TimelinePartialClear {
  meeting_id: string
  session_id: string
  generation: number
  result_id?: string
}

export interface AttachmentUploadState {
  meeting_id: string
  request_id: string
  state: 'uploading' | 'failed' | 'completed'
  name: string
  size_bytes: number
  error_code?: string
}

interface AppEvent<T> {
  data?: T
}

const emptyStatus = (): LiveMeetingStatus => ({
  recording_state: 'preparing',
  microphone_state: 'stopped',
  local_save_state: 'saving',
  realtime_asr_state: 'idle',
  agent_state: 'unchecked',
  lan_state: 'disabled',
  online_count: 0,
})

/** useTimelineStore 保存可按 SQLite seq 完整重建的会中统一时间线。 */
export const useTimelineStore = defineStore('timeline', {
  state: () => ({
    meetingID: '',
    entries: [] as TimelineEntry[],
    partials: {} as Record<string, TimelinePartial>,
    clearedPartialSessions: {} as Record<string, number>,
    clearedPartialResults: {} as Record<string, number>,
    uploads: {} as Record<string, AttachmentUploadState>,
    status: emptyStatus(),
    latestCursor: 0,
    oldestCursor: 0,
    hasOlder: false,
    loading: false,
    loadingOlder: false,
    sending: false,
    choosingAttachment: false,
    errorMessage: '',
  }),
  getters: {
    latestSeq: (state): number => state.latestCursor,
    oldestSeq: (state): number => state.oldestCursor,
    orderedPartials: (state): TimelinePartial[] =>
      Object.values(state.partials).sort(
        (left, right) => left.start_sample - right.start_sample,
      ),
    activeUploads: (state): AttachmentUploadState[] =>
      Object.values(state.uploads).filter((item) => item.state !== 'completed'),
  },
  actions: {
    /** resetMeeting 切换会议时丢弃所有可恢复内存态。 */
    resetMeeting(meetingID: string): void {
      this.meetingID = meetingID
      this.entries = []
      this.partials = {}
      this.clearedPartialSessions = {}
      this.clearedPartialResults = {}
      this.uploads = {}
      this.status = emptyStatus()
      this.latestCursor = 0
      this.oldestCursor = 0
      this.hasOlder = false
      this.errorMessage = ''
    },
    /** loadLatest 读取最新页并建立初始游标。 */
    async loadLatest(meetingID: string): Promise<void> {
      if (!meetingID) return
      if (this.meetingID !== meetingID) this.resetMeeting(meetingID)
      this.loading = true
      const result = await getMeetingTimeline(meetingID, 'latest', 0, 100)
      this.loading = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return
      }
      this.entries = [...result.data.entries].sort((a, b) => a.seq - b.seq)
      this.latestCursor = result.data.latest_seq
      this.oldestCursor = result.data.oldest_seq
      this.hasOlder = result.data.has_older
      this.clearCommittedPartials()
    },
    /** loadOlder 在用户请求历史时把旧页稳定插入顶部。 */
    async loadOlder(): Promise<void> {
      if (!this.meetingID || !this.oldestSeq || this.loadingOlder) return
      this.loadingOlder = true
      const result = await getMeetingTimeline(
        this.meetingID,
        'before',
        this.oldestSeq,
        100,
      )
      this.loadingOlder = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return
      }
      this.mergeEntries(result.data.entries)
      if (result.data.oldest_seq) this.oldestCursor = result.data.oldest_seq
      this.hasOlder = result.data.has_older
    },
    /** recoverAfter 按当前 latest seq 循环补拉，恢复通知合并或丢失。 */
    async recoverAfter(): Promise<void> {
      if (!this.meetingID) return
      let hasMore = true
      while (hasMore) {
        const result = await getMeetingTimeline(
          this.meetingID,
          'after',
          this.latestSeq,
          200,
        )
        if (result.code !== 200 || !result.data) {
          this.errorMessage = result.message
          return
        }
        this.mergeEntries(result.data.entries)
        this.latestCursor = Math.max(this.latestCursor, result.data.latest_seq)
        hasMore = result.data.has_more_after
      }
      this.clearCommittedPartials()
    },
    /** refreshLatestProjection 覆盖最新页的可变投影，同时保留已经加载的更早历史。 */
    async refreshLatestProjection(): Promise<void> {
      if (!this.meetingID) return
      const result = await getMeetingTimeline(this.meetingID, 'latest', 0, 100)
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return
      }
      this.mergeEntries(result.data.entries)
      this.latestCursor = Math.max(this.latestCursor, result.data.latest_seq)
      if (!this.oldestCursor && result.data.oldest_seq)
        this.oldestCursor = result.data.oldest_seq
      this.clearCommittedPartials()
    },
    /** refreshStatus 更新右侧独立状态轴。 */
    async refreshStatus(): Promise<void> {
      if (!this.meetingID) return
      const result = await getLiveMeetingStatus(this.meetingID)
      if (result.code === 200 && result.data) this.status = result.data
    },
    /** sendText 发送主持人 Markdown 消息，成功后立即合并真实 seq。 */
    async sendText(content: string): Promise<boolean> {
      const normalized = content.trim()
      if (!this.meetingID || !normalized || this.sending) return false
      this.sending = true
      this.errorMessage = ''
      const result = await sendMeetingMessage(
        this.meetingID,
        crypto.randomUUID(),
        normalized,
      )
      this.sending = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return false
      }
      this.mergeEntries([result.data])
      this.latestCursor = Math.max(this.latestCursor, result.data.seq)
      return true
    },
    /** chooseAttachment 只触发系统文件窗口；确认后由后端直接发送。 */
    async chooseAttachment(): Promise<void> {
      if (!this.meetingID || this.choosingAttachment) return
      this.choosingAttachment = true
      this.errorMessage = ''
      const result = await chooseAndSendMeetingAttachment(this.meetingID)
      this.choosingAttachment = false
      if (result.code !== 200) {
        this.errorMessage = result.message
        return
      }
      if (!result.data?.cancelled) await this.recoverAfter()
    },
    /** applyPartial 按物理 session 隔离 revision，并拒绝 clear 后迟到的旧事件。 */
    applyPartial(event: AppEvent<TimelinePartial>): void {
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
      if (!previous || previous.revision < partial.revision)
        this.partials[key] = partial
    },
    /** applyPartialClear 删除指定 session/result，并记录 tombstone 拒绝迟到事件。 */
    applyPartialClear(event: AppEvent<TimelinePartialClear>): void {
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
    /** applyAttachmentState 更新不含本地路径的临时上传行。 */
    applyAttachmentState(event: AppEvent<AttachmentUploadState>): void {
      const upload = event.data
      if (!upload || upload.meeting_id !== this.meetingID) return
      if (upload.state === 'completed') {
        delete this.uploads[upload.request_id]
        return
      }
      this.uploads[upload.request_id] = upload
    },
    /** mergeEntries 按 seq 去重；说话人投影只允许相同或更高 revision 覆盖。 */
    mergeEntries(incoming: TimelineEntry[]): void {
      const entries = new Map(this.entries.map((entry) => [entry.seq, entry]))
      for (const entry of incoming) {
        const previous = entries.get(entry.seq)
        if (
          previous?.kind === 'utterance' &&
          entry.kind === 'utterance' &&
          (entry.speaker_revision ?? 0) < (previous.speaker_revision ?? 0)
        )
          continue
        entries.set(entry.seq, entry)
      }
      this.entries = [...entries.values()].sort((a, b) => a.seq - b.seq)
    },
    /** clearCommittedPartials 用 final 样本范围替换已经落库的临时转写。 */
    clearCommittedPartials(): void {
      const finalEnd = this.entries
        .filter((entry) => entry.kind === 'utterance')
        .reduce((end, entry) => Math.max(end, entry.end_sample ?? 0), 0)
      for (const [resultID, partial] of Object.entries(this.partials)) {
        if (partial.end_sample <= finalEnd) delete this.partials[resultID]
      }
    },
  },
})

/** partialKey 返回跨物理 session 唯一的临时转写键。 */
function partialKey(sessionID: string, resultID: string): string {
  return `${sessionID}:${resultID}`
}
