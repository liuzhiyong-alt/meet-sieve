import { defineStore } from 'pinia'

import {
  AddUtteranceToVoiceSamples,
  CorrectSpeakerCluster,
  CorrectUtteranceSpeaker,
  CorrectUtteranceText,
  CreateUtteranceAudioClip,
  GetSpeakerStatus,
  ListCorrectionEntries,
  RetryRawRecordProjection,
  RetrySpeakerProcessing,
  RevokeAudioClip,
} from '../../wailsjs/go/wails/CorrectionBinding'

export interface CorrectionEntry {
  seq: number
  utterance_id: string
  start_sample: number
  end_sample: number
  original_text: string
  current_text: string
  speaker_display: string
  current_participant_id?: string
  speaker_cluster_id?: string
  assignment_source: string
  text_revision: number
  speaker_revision: number
  cluster_revision?: number
  cluster_count?: number
  can_play: boolean
  playback_disabled_reason?: string
  can_enroll: boolean
  enrollment_disabled_reason?: string
}

export interface CorrectionParticipant {
  id: string
  display_name: string
  kind: string
  is_member: boolean
}

/** useCorrectionStore 只缓存可从 SQLite 分页重建的校对工作台状态。 */
export const useCorrectionStore = defineStore('correction', {
  state: () => ({
    meetingID: '',
    entries: [] as CorrectionEntry[],
    participants: [] as CorrectionParticipant[],
    selectedID: '',
    loading: false,
    saving: false,
    enrolling: false,
    errorMessage: '',
    notice: '',
    projectionWarning: '',
    audioURL: '',
    speakerState: 'unknown',
    speakerErrorCode: '',
  }),
  getters: {
    /** selected 返回当前选中片段。 */
    selected: (state): CorrectionEntry | undefined =>
      state.entries.find((entry) => entry.utterance_id === state.selectedID),
  },
  actions: {
    /** load 从 seq=0 完整恢复，避免刷新或 event 缺口依赖旧 Pinia 内存。 */
    async load(meetingID: string): Promise<void> {
      if (!meetingID) return
      this.loading = true
      this.errorMessage = ''
      const entries: CorrectionEntry[] = []
      let afterSeq = 0
      for (;;) {
        const result = await ListCorrectionEntries(meetingID, afterSeq, 200)
        if (result.code !== 200 || !result.data) {
          this.errorMessage = result.message
          break
        }
        const page = result.data
        entries.push(...(page.entries as CorrectionEntry[]))
        this.participants = page.participants as CorrectionParticipant[]
        if (page.entries.length < 200 || page.next_seq <= afterSeq) break
        afterSeq = page.next_seq
      }
      this.meetingID = meetingID
      this.entries = entries
      if (!entries.some((entry) => entry.utterance_id === this.selectedID)) {
        this.selectedID = entries[0]?.utterance_id ?? ''
      }
      this.loading = false
      const status = await GetSpeakerStatus(meetingID)
      if (status.code === 200 && status.data) {
        this.speakerState = status.data.state
        this.speakerErrorCode = status.data.error_code ?? ''
      }
    },
    /** saveText 保存 current text；revision 冲突时保留组件草稿并要求刷新。 */
    async saveText(entry: CorrectionEntry, text: string): Promise<boolean> {
      return this.runCorrection(
        () =>
          CorrectUtteranceText({
            request_id: crypto.randomUUID(),
            meeting_id: this.meetingID,
            utterance_id: entry.utterance_id,
            expected_revision: entry.text_revision,
            value: text,
            reason: '',
          }),
        '文字校对已保存',
      )
    },
    /** saveSpeaker 只校对当前片段说话人。 */
    async saveSpeaker(
      entry: CorrectionEntry,
      participantID: string,
    ): Promise<boolean> {
      return this.runCorrection(
        () =>
          CorrectUtteranceSpeaker({
            request_id: crypto.randomUUID(),
            meeting_id: this.meetingID,
            utterance_id: entry.utterance_id,
            expected_revision: entry.speaker_revision,
            value: participantID,
            reason: '',
          }),
        '说话人校对已保存',
      )
    },
    /** saveCluster 按确认时 revision/count 批量校对当前 cluster。 */
    async saveCluster(
      entry: CorrectionEntry,
      participantID: string,
    ): Promise<boolean> {
      if (!entry.speaker_cluster_id) return false
      return this.runCorrection(
        () =>
          CorrectSpeakerCluster({
            request_id: crypto.randomUUID(),
            meeting_id: this.meetingID,
            cluster_id: entry.speaker_cluster_id ?? '',
            participant_id: participantID,
            expected_revision: entry.cluster_revision ?? 0,
            expected_count: entry.cluster_count ?? 0,
            reason: '',
          }),
        `本场 ${entry.cluster_count ?? 0} 个片段已修改`,
      )
    },
    /** createClip 切换片段前先撤销旧 token，再创建新 URL。 */
    async createClip(entry: CorrectionEntry): Promise<string> {
      await this.revokeClip()
      const result = await CreateUtteranceAudioClip(entry.utterance_id)
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return ''
      }
      this.audioURL = result.data.url
      return this.audioURL
    },
    /** revokeClip 主动回收当前页面 token。 */
    async revokeClip(): Promise<void> {
      if (!this.audioURL) return
      const current = this.audioURL
      this.audioURL = ''
      await RevokeAudioClip(current)
    },
    /** enrollSelected 独立二次确认加入永久声纹。 */
    async enrollSelected(entry: CorrectionEntry): Promise<boolean> {
      this.enrolling = true
      this.errorMessage = ''
      const result = await AddUtteranceToVoiceSamples({
        request_id: crypto.randomUUID(),
        meeting_id: this.meetingID,
        utterance_id: entry.utterance_id,
        environment_kind: 'meeting_room',
        confirmed: true,
      })
      this.enrolling = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return false
      }
      this.notice =
        result.data.quality_state === 'accepted'
          ? '声纹样本已加入'
          : `片段质量未通过：${result.data.quality_code || '请更换片段'}`
      return result.data.quality_state === 'accepted'
    },
    /** retryProjection 重试从 SQLite 刷新 Markdown。 */
    async retryProjection(): Promise<void> {
      const result = await RetryRawRecordProjection(this.meetingID)
      if (result.code === 200) this.projectionWarning = ''
      else this.errorMessage = result.message
    },
    /** retrySpeaker 在正式 profile 缺失时保留明确 Warning。 */
    async retrySpeaker(): Promise<void> {
      const result = await RetrySpeakerProcessing(this.meetingID)
      if (result.code !== 200) this.errorMessage = result.message
      else await this.load(this.meetingID)
    },
    /** runCorrection 统一 busy、部分成功和 SQLite 重载。 */
    async runCorrection(
      action: () => Promise<{
        code: number
        message: string
        data?: {
          saved: boolean
          no_op: boolean
          projection_state: string
          projection_error_code?: string
        }
      }>,
      successNotice: string,
    ): Promise<boolean> {
      if (this.saving) return false
      this.saving = true
      this.errorMessage = ''
      this.notice = ''
      const result = await action()
      this.saving = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return false
      }
      this.notice = result.data.no_op ? '没有需要保存的变化' : successNotice
      this.projectionWarning =
        result.data.projection_state === 'failed'
          ? '校对已保存，但会议原始记录尚未刷新。'
          : ''
      await this.load(this.meetingID)
      return true
    },
  },
})
