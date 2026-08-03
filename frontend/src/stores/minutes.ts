import { defineStore } from 'pinia'

import {
  ConfirmMinute,
  GenerateMinutes,
  GetMinutesState,
  ListMinuteVersions,
  RestoreMinuteVersion,
  SaveMinuteDraft,
  StopMinutesGeneration,
} from '../../wailsjs/go/wails/MinutesBinding'

import type { Step8EventEnvelope } from './finalization'

export interface MinuteVersion {
  id: string
  version_no: number
  source: string
  content_markdown: string
  state: string
  is_current: boolean
  confirmed_at?: number
  created_at: number
}

export interface MinutesProjection {
  meeting_id: string
  state: string
  current?: MinuteVersion
  latest_candidate?: MinuteVersion
  recent_error_code?: string
  turn_id?: string
  runtime_state: string
  projection_state: string
  revision: number
}

const emptyState = (): MinutesProjection => ({
  meeting_id: '',
  state: 'none',
  runtime_state: 'idle',
  projection_state: 'idle',
  revision: 0,
})

/** useMinutesStore 管理不可变纪要版本、编辑草稿和生成运行态。 */
export const useMinutesStore = defineStore('minutes', {
  state: () => ({
    projection: emptyState(),
    draft: '',
    baseVersionID: '',
    dirty: false,
    history: [] as MinuteVersion[],
    nextCursor: 0,
    loading: false,
    generating: false,
    saving: false,
    errorMessage: '',
    notice: '',
  }),
  getters: {
    canConfirm: (state) =>
      Boolean(state.projection.current) &&
      !state.dirty &&
      state.projection.current?.state !== 'confirmed',
    processing: (state) =>
      state.generating ||
      ['generating', 'slow'].includes(state.projection.runtime_state),
  },
  actions: {
    /** refresh 从 SQLite/current runtime 重建页面，未编辑时同步草稿。 */
    async refresh(meetingID: string): Promise<void> {
      if (!meetingID) return
      this.loading = true
      const result = await GetMinutesState(meetingID)
      this.loading = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return
      }
      this.projection = result.data as MinutesProjection
      if (!this.dirty) this.resetDraft()
    },
    /** setDraft 记录本地未保存修改，不提前改写 current 版本。 */
    setDraft(content: string): void {
      this.draft = content
      this.dirty = content !== (this.projection.current?.content_markdown ?? '')
    },
    /** generate 生成 AI candidate；人工 current 由后端版本策略保持不变。 */
    async generate(
      meetingID: string,
      showGapNotice: boolean,
    ): Promise<boolean> {
      if (this.generating) return false
      this.generating = true
      this.errorMessage = ''
      this.notice = ''
      const result = await GenerateMinutes(
        meetingID,
        showGapNotice,
        crypto.randomUUID(),
      )
      this.generating = false
      if (result.code !== 200) {
        this.errorMessage = result.message
        await this.refresh(meetingID)
        return false
      }
      await this.refresh(meetingID)
      this.notice = 'AI 纪要已生成'
      return true
    },
    /** stop 停止当前 turn；停止或超时不会创建版本。 */
    async stop(): Promise<boolean> {
      const meetingID = this.projection.meeting_id
      const turnID = this.projection.turn_id
      if (!meetingID || !turnID) return false
      const result = await StopMinutesGeneration(meetingID, turnID)
      if (result.code !== 200) {
        this.errorMessage = result.message
        return false
      }
      await this.refresh(meetingID)
      return true
    },
    /** saveDraft 从明确 current 基线创建新的人工版本。 */
    async saveDraft(): Promise<boolean> {
      if (!this.baseVersionID || !this.dirty || this.saving) return false
      this.saving = true
      this.errorMessage = ''
      const result = await SaveMinuteDraft(
        this.projection.meeting_id,
        this.baseVersionID,
        this.draft,
        crypto.randomUUID(),
      )
      this.saving = false
      if (result.code !== 200) {
        this.errorMessage = result.message
        await this.refresh(this.projection.meeting_id)
        return false
      }
      this.dirty = false
      await this.refresh(this.projection.meeting_id)
      this.notice = '已创建新的人工版本'
      return true
    },
    /** confirm 确认未修改的 current，不新建版本。 */
    async confirm(): Promise<boolean> {
      const current = this.projection.current
      if (!current || !this.canConfirm) return false
      const result = await ConfirmMinute(
        this.projection.meeting_id,
        current.id,
        crypto.randomUUID(),
      )
      if (result.code !== 200) {
        this.errorMessage = result.message
        return false
      }
      await this.refresh(this.projection.meeting_id)
      this.notice = '当前纪要已确认'
      return true
    },
    /** loadHistory 从最新版本开始读取不可变历史。 */
    async loadHistory(meetingID: string, append = false): Promise<void> {
      const cursor = append ? this.nextCursor : 0
      const result = await ListMinuteVersions(meetingID, cursor, 50)
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return
      }
      this.history = append
        ? [...this.history, ...(result.data.items ?? [])]
        : (result.data.items ?? [])
      this.nextCursor = result.data.next_cursor ?? 0
    },
    /** restore 把历史正文复制为新版本，原历史保持不变。 */
    async restore(versionID: string): Promise<boolean> {
      const result = await RestoreMinuteVersion(
        this.projection.meeting_id,
        versionID,
        crypto.randomUUID(),
      )
      if (result.code !== 200) {
        this.errorMessage = result.message
        return false
      }
      this.dirty = false
      await Promise.all([
        this.refresh(this.projection.meeting_id),
        this.loadHistory(this.projection.meeting_id),
      ])
      this.notice = '已从历史创建新的当前版本'
      return true
    },
    /** applyEvent 按 meeting/revision 去重；version_changed 的零 revision 仍触发查询。 */
    applyEvent(event: Step8EventEnvelope): boolean {
      const data = event.data
      if (
        event.version !== 1 ||
        !data ||
        data.meeting_id !== this.projection.meeting_id ||
        (data.revision > 0 && data.revision <= this.projection.revision)
      )
        return false
      if (data.revision > 0) this.projection.revision = data.revision
      this.projection.runtime_state = data.state
      return true
    },
    /** resetDraft 从 current 版本恢复编辑器基线。 */
    resetDraft(): void {
      this.draft = this.projection.current?.content_markdown ?? ''
      this.baseVersionID = this.projection.current?.id ?? ''
      this.dirty = false
    },
  },
})
