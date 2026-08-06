import { defineStore } from 'pinia'

import {
  GenerateMinutes,
  GetMinutesSettings,
  GetMinutesState,
  SaveMinuteDraft,
  SaveMinutesSettings,
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
  recent_error_code?: string
  turn_id?: string
  runtime_state: string
  projection_state: string
  revision: number
}

export interface MinutesSettings {
  prompt: string
  is_default: boolean
  updated_at: number
}

const emptyState = (): MinutesProjection => ({
  meeting_id: '',
  state: 'none',
  runtime_state: 'idle',
  projection_state: 'idle',
  revision: 0,
})

/** useMinutesStore 管理单份会议纪要、Markdown 源码草稿和生成要求。 */
export const useMinutesStore = defineStore('minutes', {
  state: () => ({
    projection: emptyState(),
    draft: '',
    baseVersionID: '',
    dirty: false,
    settings: {
      prompt: '',
      is_default: true,
      updated_at: 0,
    } as MinutesSettings,
    loading: false,
    generating: false,
    saving: false,
    settingsLoading: false,
    settingsSaving: false,
    errorMessage: '',
    settingsError: '',
    notice: '',
    settingsNotice: '',
  }),
  getters: {
    processing: (state) =>
      state.generating ||
      ['generating', 'slow'].includes(state.projection.runtime_state),
  },
  actions: {
    /** refresh 从 SQLite/current runtime 重建页面，未编辑时同步 Markdown 源码。 */
    async refresh(meetingID: string): Promise<void> {
      if (!meetingID) return
      this.loading = true
      const result = await GetMinutesState(meetingID)
      this.loading = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return
      }
      this.errorMessage = ''
      this.projection = result.data as MinutesProjection
      if (!this.dirty) this.resetDraft()
    },
    /** setDraft 记录本地未保存的 Markdown 源码。 */
    setDraft(content: string): void {
      this.draft = content
      this.dirty = content !== (this.projection.current?.content_markdown ?? '')
    },
    /** generate 主动生成首份会议纪要。 */
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
      this.notice = '会议纪要已生成'
      return true
    },
    /** stop 停止当前生成任务；停止或超时不会改写已有内容。 */
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
    /** saveDraft 保存当前 Markdown 源码并刷新单份纪要投影。 */
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
      this.notice = '会议纪要已保存'
      return true
    },
    /** loadSettings 读取会议纪要要求，未配置时由后端回填当前默认内容。 */
    async loadSettings(): Promise<void> {
      this.settingsLoading = true
      const result = await GetMinutesSettings()
      this.settingsLoading = false
      if (result.code !== 200 || !result.data) {
        this.settingsError = result.message
        return
      }
      this.settingsError = ''
      this.settings = result.data as MinutesSettings
    },
    /** saveSettings 保存业务要求；空内容由后端解释为恢复默认要求。 */
    async saveSettings(prompt: string): Promise<boolean> {
      if (this.settingsSaving) return false
      this.settingsSaving = true
      this.settingsError = ''
      this.settingsNotice = ''
      const result = await SaveMinutesSettings(prompt)
      this.settingsSaving = false
      if (result.code !== 200 || !result.data) {
        this.settingsError = result.message
        return false
      }
      this.settings = result.data as MinutesSettings
      this.settingsNotice = prompt.trim()
        ? '会议纪要要求已保存'
        : '已恢复默认会议纪要要求'
      return true
    },
    /** restoreDefault 清除自定义要求并回填当前内置默认内容。 */
    async restoreDefault(): Promise<boolean> {
      return this.saveSettings('')
    },
    /** applyEvent 按 meeting/revision 去重并触发调用方重新查询。 */
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
    /** resetDraft 从当前纪要恢复编辑器基线。 */
    resetDraft(): void {
      this.draft = this.projection.current?.content_markdown ?? ''
      this.baseVersionID = this.projection.current?.id ?? ''
      this.dirty = false
    },
  },
})
