import { defineStore } from 'pinia'

import {
  DeleteMeeting,
  GetDeletionJob,
  PreviewMeetingDeletion,
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
    /** previewMeeting 请求后端扫描整个规范会议目录。 */
    async previewMeeting(meetingID: string): Promise<boolean> {
      return this.invoke(
        () => PreviewMeetingDeletion(meetingID),
        (data) => {
          this.preview = data
        },
      )
    },
    /** deleteMeeting 额外传递用户手工输入的会议号。 */
    async deleteMeeting(meetingNo: string): Promise<boolean> {
      const preview = this.preview
      if (!preview) return false
      return this.invoke(
        () =>
          DeleteMeeting({
            meeting_id: preview.meeting_id,
            meeting_no: meetingNo,
            revision: preview.revision,
            digest: preview.digest,
          }),
        (data) => {
          this.job = data
        },
      )
    },
    /** loadJob 从 SQLite 重建删除失败恢复状态。 */
    async loadJob(meetingID: string): Promise<boolean> {
      return this.invoke(
        () => GetDeletionJob(meetingID),
        (data) => {
          this.job = data
        },
      )
    },
    /** retry 只让后端继续原 manifest 剩余项。 */
    async retry(): Promise<boolean> {
      if (!this.job) return false
      return this.invoke(
        () => RetryDeletion(this.job!.job_id),
        (data) => {
          this.job = data
        },
      )
    },
    /** invoke 保证 Wails Promise 拒绝时也会恢复按钮状态。 */
    async invoke<T>(
      call: () => Promise<{ code: number; message: string; data?: T }>,
      apply: (data: T) => void,
    ): Promise<boolean> {
      this.begin()
      try {
        return this.consume(await call(), apply)
      } catch {
        this.loading = false
        this.errorMessage = '无法完成删除操作'
        return false
      }
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
