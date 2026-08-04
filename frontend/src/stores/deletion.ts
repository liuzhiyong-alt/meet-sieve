import { defineStore } from 'pinia'

import {
  DeleteMeeting,
  DeleteRecording,
  GetDeletionJob,
  PreviewMeetingDeletion,
  PreviewRecordingDeletion,
  RetryDeletion,
} from '../../wailsjs/go/wails/DeletionBinding'
import type { wails } from '../../wailsjs/go/models'

/** useDeletionStore 保存后端 manifest 任务投影，不在前端构造路径清单。 */
export const useDeletionStore = defineStore('deletion', {
  state: () => ({
    preview: undefined as wails.DeletionPreviewDTO | undefined,
    job: undefined as wails.DeletionJobDTO | undefined,
    loading: false,
    errorMessage: '',
  }),
  actions: {
    /** previewRecording 请求后端扫描录音范围。 */
    async previewRecording(meetingID: string): Promise<boolean> {
      this.begin()
      return this.consume(await PreviewRecordingDeletion(meetingID), (data) => {
        this.preview = data
      })
    },
    /** previewMeeting 请求后端扫描整个规范会议目录。 */
    async previewMeeting(meetingID: string): Promise<boolean> {
      this.begin()
      return this.consume(await PreviewMeetingDeletion(meetingID), (data) => {
        this.preview = data
      })
    },
    /** deleteRecording 只回传预览 revision/digest。 */
    async deleteRecording(): Promise<boolean> {
      if (!this.preview) return false
      this.begin()
      return this.consume(
        await DeleteRecording({
          meeting_id: this.preview.meeting_id,
          revision: this.preview.revision,
          digest: this.preview.digest,
        }),
        (data) => {
          this.job = data
        },
      )
    },
    /** deleteMeeting 额外传递用户手工输入的会议号。 */
    async deleteMeeting(meetingNo: string): Promise<boolean> {
      if (!this.preview) return false
      this.begin()
      return this.consume(
        await DeleteMeeting({
          meeting_id: this.preview.meeting_id,
          meeting_no: meetingNo,
          revision: this.preview.revision,
          digest: this.preview.digest,
        }),
        (data) => {
          this.job = data
        },
      )
    },
    /** loadJob 从 SQLite 重建删除失败恢复状态。 */
    async loadJob(meetingID: string): Promise<boolean> {
      this.begin()
      return this.consume(await GetDeletionJob(meetingID), (data) => {
        this.job = data
      })
    },
    /** retry 只让后端继续原 manifest 剩余项。 */
    async retry(): Promise<boolean> {
      if (!this.job) return false
      this.begin()
      return this.consume(await RetryDeletion(this.job.job_id), (data) => {
        this.job = data
      })
    },
    /** consume 统一处理删除命令状态。 */
    async consume<T>(
      result: { code: number; message: string; data?: T },
      apply: (data: T) => void,
    ): Promise<boolean> {
      this.loading = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message || '删除操作未完成'
        return false
      }
      apply(result.data)
      return true
    },
    /** begin 在异步删除命令开始前更新界面状态。 */
    begin(): void {
      this.loading = true
      this.errorMessage = ''
    },
  },
})
