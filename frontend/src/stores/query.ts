import { defineStore } from 'pinia'

import {
  GetHome,
  GetInterruptedRecovery,
  GetMeetingDetail,
  ListMeetingContent,
  ListMeetings,
  ListTranscript,
} from '../../wailsjs/go/wails/QueryBinding'
import type { wails } from '../../wailsjs/go/models'

export type MeetingSummary = wails.MeetingSummaryDTO

interface BindingResult<T> {
  code: number
  message: string
  errorCode?: string
  data?: T
}

/** useQueryStore 只缓存可由 Go/SQLite 重建的页面投影和游标。 */
export const useQueryStore = defineStore('query', {
  state: () => ({
    home: undefined as wails.HomeDTO | undefined,
    meetings: [] as MeetingSummary[],
    nextCursor: '',
    previousCursor: '',
    detail: undefined as wails.MeetingDetailDTO | undefined,
    transcript: undefined as wails.TranscriptPageDTO | undefined,
    content: undefined as wails.ContentPageDTO | undefined,
    recovery: undefined as wails.InterruptedRecoveryDTO | undefined,
    loading: false,
    errorMessage: '',
    errorCode: '',
  }),
  actions: {
    /** loadHome 从后端读取唯一继续处理项和最近会议。 */
    async loadHome(): Promise<boolean> {
      return this.invoke(GetHome, (data) => {
        this.home = data
      })
    },
    /** loadMeetings 使用 URL 提供的筛选和不透明游标读取一页。 */
    async loadMeetings(
      search: string,
      status: string,
      cursor: string,
    ): Promise<boolean> {
      return this.invoke(
        () => ListMeetings({ search, status, cursor, limit: 10 }),
        (data) => {
          this.meetings = data.items
          this.nextCursor = data.next_cursor ?? ''
          this.previousCursor = data.previous_cursor ?? ''
        },
      )
    },
    /** loadDetail 读取会议摘要和所有正交状态能力。 */
    async loadDetail(meetingID: string): Promise<boolean> {
      return this.invoke(
        () => GetMeetingDetail(meetingID),
        (data) => {
          this.detail = data
        },
      )
    },
    /** loadTranscript 按 seq 懒加载最多 200 条原始记录。 */
    async loadTranscript(
      meetingID: string,
      afterSeq = 0,
      beforeSeq = 0,
    ): Promise<boolean> {
      return this.invoke(
        () =>
          ListTranscript({
            meeting_id: meetingID,
            after_seq: afterSeq,
            before_seq: beforeSeq,
            limit: 200,
          }),
        (data) => {
          this.transcript = data
        },
      )
    },
    /** loadContent 按 seq 懒加载最多 100 条消息、资料与公开 AI。 */
    async loadContent(
      meetingID: string,
      afterSeq = 0,
      beforeSeq = 0,
    ): Promise<boolean> {
      return this.invoke(
        () =>
          ListMeetingContent({
            meeting_id: meetingID,
            after_seq: afterSeq,
            before_seq: beforeSeq,
            limit: 100,
          }),
        (data) => {
          this.content = data
        },
      )
    },
    /** loadRecovery 读取 interrupted/unsaved 的真实恢复摘要。 */
    async loadRecovery(meetingID: string): Promise<boolean> {
      return this.invoke(
        () => GetInterruptedRecovery(meetingID),
        (data) => {
          this.recovery = data
        },
      )
    },
    /** invoke 保证 Wails Promise 拒绝时也结束 loading，且不泄漏底层异常。 */
    async invoke<T>(
      call: () => Promise<BindingResult<T>>,
      apply: (data: T) => void,
    ): Promise<boolean> {
      this.begin()
      try {
        return await this.consume(await call(), apply)
      } catch {
        this.loading = false
        this.errorMessage = '无法读取本地会议数据'
        this.errorCode = ''
        return false
      }
    },
    /** consume 统一管理 Query Binding 的 loading/error 状态。 */
    async consume<T>(
      result: BindingResult<T>,
      apply: (data: T) => void,
    ): Promise<boolean> {
      this.loading = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message || '无法读取本地会议数据'
        this.errorCode = result.errorCode ?? ''
        return false
      }
      apply(result.data)
      return true
    },
    /** begin 在异步 Binding 调用前进入 loading，并清除旧错误。 */
    begin(): void {
      this.loading = true
      this.errorMessage = ''
      this.errorCode = ''
    },
  },
})
