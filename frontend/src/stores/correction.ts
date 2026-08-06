import { defineStore } from 'pinia'

import {
  CorrectSpeakerCluster,
  CorrectUtteranceSpeaker,
  CorrectUtteranceText,
  CreateUtteranceAudioClip,
  ListCorrectionEntries,
  RetryRawRecordProjection,
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
  cluster_display_no?: number
  cluster_participant_id?: string
  assignment_source: string
  text_revision: number
  speaker_revision: number
  cluster_revision?: number
  cluster_count?: number
  can_play: boolean
  playback_disabled_reason?: string
}

export interface CorrectionParticipant {
  id: string
  display_name: string
  kind: string
  is_member: boolean
}

export interface CorrectionDraft {
  value: string
  requestID: string
}

interface CorrectionResult {
  code: number
  message: string
  data?: {
    saved: boolean
    no_op: boolean
    projection_state: string
  }
}

interface DraftSnapshot {
  textDrafts: Record<string, CorrectionDraft>
  speakerDrafts: Record<string, CorrectionDraft>
  clusterDrafts: Record<string, CorrectionDraft>
}

/** useCorrectionStore 维护可重建快照与尚未提交的本地编辑草稿。 */
export const useCorrectionStore = defineStore('correction', {
  state: () => ({
    meetingID: '',
    entries: [] as CorrectionEntry[],
    participants: [] as CorrectionParticipant[],
    textDrafts: {} as Record<string, CorrectionDraft>,
    speakerDrafts: {} as Record<string, CorrectionDraft>,
    clusterDrafts: {} as Record<string, CorrectionDraft>,
    loading: false,
    saving: false,
    errorMessage: '',
    notice: '',
    projectionWarning: '',
    audioURL: '',
  }),
  getters: {
    /** isDirty 返回页面是否存在未保存的文字或说话人草稿。 */
    isDirty: (state): boolean =>
      Boolean(
        Object.keys(state.textDrafts).length ||
        Object.keys(state.speakerDrafts).length ||
        Object.keys(state.clusterDrafts).length,
      ),
  },
  actions: {
    /** load 从 SQLite 完整恢复 entries，绝不以旧 Pinia 内存充当会议事实。 */
    async load(meetingID: string): Promise<boolean> {
      if (!meetingID) return false
      this.loading = true
      const entries: CorrectionEntry[] = []
      let afterSeq = 0
      for (;;) {
        const result = await ListCorrectionEntries(meetingID, afterSeq, 200)
        if (result.code !== 200 || !result.data) {
          this.errorMessage = result.message
          this.loading = false
          return false
        }
        const page = result.data
        entries.push(...(page.entries as CorrectionEntry[]))
        this.participants = page.participants as CorrectionParticipant[]
        if (page.entries.length < 200 || page.next_seq <= afterSeq) break
        afterSeq = page.next_seq
      }
      this.meetingID = meetingID
      this.entries = entries
      this.loading = false
      return true
    },

    /** textValue 返回 entry 在本地草稿覆盖后的可编辑文字。 */
    textValue(entry: CorrectionEntry): string {
      return this.textDrafts[entry.utterance_id]?.value ?? entry.current_text
    },

    /** speakerValue 返回单段例外、cluster 草稿和后端快照合成后的说话人。 */
    speakerValue(entry: CorrectionEntry): string {
      const single = this.speakerDrafts[entry.utterance_id]
      if (single) return single.value
      if (!entry.speaker_cluster_id) return entry.current_participant_id ?? ''
      return (
        this.clusterDrafts[entry.speaker_cluster_id]?.value ??
        entry.cluster_participant_id ??
        entry.current_participant_id ??
        ''
      )
    },

    /** setTextDraft 只记录本地输入；失焦不会产生 correction。 */
    setTextDraft(entry: CorrectionEntry, value: string): void {
      if (value === entry.current_text) {
        delete this.textDrafts[entry.utterance_id]
        return
      }
      this.textDrafts[entry.utterance_id] = this.createDraft(
        value,
        this.textDrafts[entry.utterance_id],
      )
    },

    /** setClusterDraft 设置本场同一未知说话人的统一对应关系。 */
    setClusterDraft(entry: CorrectionEntry, participantID: string): void {
      if (!entry.speaker_cluster_id || !participantID) return
      const clusterID = entry.speaker_cluster_id
      if (participantID === (entry.cluster_participant_id ?? '')) {
        delete this.clusterDrafts[clusterID]
        return
      }
      this.clusterDrafts[clusterID] = this.createDraft(
        participantID,
        this.clusterDrafts[clusterID],
      )
    },

    /** setSpeakerDraft 将单段选择记录为覆盖 cluster 草稿的例外。 */
    setSpeakerDraft(entry: CorrectionEntry, participantID: string): void {
      const clusterDraft = entry.speaker_cluster_id
        ? this.clusterDrafts[entry.speaker_cluster_id]
        : undefined
      const clusterValue =
        clusterDraft?.value ?? entry.cluster_participant_id ?? ''
      const persistedValue = entry.current_participant_id ?? ''
      const shouldClear = clusterDraft
        ? participantID === clusterValue
        : participantID === persistedValue
      if (shouldClear) {
        delete this.speakerDrafts[entry.utterance_id]
        return
      }
      this.speakerDrafts[entry.utterance_id] = this.createDraft(
        participantID,
        this.speakerDrafts[entry.utterance_id],
      )
    },

    /** discardDrafts 放弃页面全部未保存输入，不影响 SQLite 中已保存的 correction。 */
    discardDrafts(): void {
      this.textDrafts = {}
      this.speakerDrafts = {}
      this.clusterDrafts = {}
      this.errorMessage = ''
      this.notice = ''
    },

    /** saveAll 按 cluster、单段说话人、文字的顺序提交并保留部分失败草稿。 */
    async saveAll(): Promise<boolean> {
      if (this.saving || !this.isDirty) return true
      this.saving = true
      this.errorMessage = ''
      this.notice = ''
      const snapshot = this.copyDrafts()
      const failedClusters = await this.saveClusterDrafts(
        snapshot.clusterDrafts,
      )
      await this.load(this.meetingID)
      const failedSpeakers = await this.saveSpeakerDrafts(
        snapshot.speakerDrafts,
        failedClusters,
      )
      await this.load(this.meetingID)
      const failedTexts = await this.saveTextDrafts(snapshot.textDrafts)
      await this.load(this.meetingID)
      this.clearSavedDrafts(snapshot, {
        clusters: failedClusters,
        speakers: failedSpeakers,
        texts: failedTexts,
      })
      this.saving = false
      if (failedClusters.size || failedSpeakers.size || failedTexts.size) {
        this.notice = '部分修改未保存，请检查后再次保存。'
        return false
      }
      this.notice = '修改已保存'
      return true
    },

    /** createClip 切换片段前回收旧 token，只保留一个可播放 URL。 */
    async createClip(entry: CorrectionEntry): Promise<string> {
      this.errorMessage = ''
      await this.revokeClip()
      const result = await CreateUtteranceAudioClip(entry.utterance_id)
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return ''
      }
      this.audioURL = result.data.url
      return this.audioURL
    },

    /** revokeClip 主动回收当前页面持有的短期回放 token。 */
    async revokeClip(): Promise<void> {
      if (!this.audioURL) return
      const current = this.audioURL
      this.audioURL = ''
      await RevokeAudioClip(current)
    },

    /** retryProjection 只重试 Markdown 投影，不重复提交已经保存的 correction。 */
    async retryProjection(): Promise<void> {
      const result = await RetryRawRecordProjection(this.meetingID)
      if (result.code === 200) this.projectionWarning = ''
      else this.errorMessage = result.message
    },

    /** createDraft 为一次未完成编辑保留稳定 request ID，重试时不会重复写入。 */
    createDraft(value: string, draft?: CorrectionDraft): CorrectionDraft {
      return { value, requestID: draft?.requestID ?? crypto.randomUUID() }
    },

    /** copyDrafts 冻结本次保存输入，允许用户在保存期间继续看到原草稿。 */
    copyDrafts(): DraftSnapshot {
      return {
        textDrafts: { ...this.textDrafts },
        speakerDrafts: { ...this.speakerDrafts },
        clusterDrafts: { ...this.clusterDrafts },
      }
    },

    /** saveClusterDrafts 先提交 cluster，避免随后单段例外被批量更新覆盖。 */
    async saveClusterDrafts(
      drafts: Record<string, CorrectionDraft>,
    ): Promise<Set<string>> {
      const failed = new Set<string>()
      const clusters = this.clusterEntries()
      for (const [clusterID, draft] of Object.entries(drafts)) {
        const entry = clusters.get(clusterID)
        if (!entry) {
          failed.add(clusterID)
          continue
        }
        const result = await this.submit(() =>
          CorrectSpeakerCluster({
            request_id: draft.requestID,
            meeting_id: this.meetingID,
            cluster_id: clusterID,
            participant_id: draft.value,
            expected_revision: entry.cluster_revision ?? 0,
            expected_count: entry.cluster_count ?? 0,
            reason: '',
          }),
        )
        if (!result) failed.add(clusterID)
      }
      return failed
    },

    /** saveSpeakerDrafts 在刷新 cluster revision 后按 seq 提交单段例外。 */
    async saveSpeakerDrafts(
      drafts: Record<string, CorrectionDraft>,
      failedClusters: Set<string>,
    ): Promise<Set<string>> {
      const failed = new Set<string>()
      for (const [utteranceID, draft] of this.entriesInSeqOrder(drafts)) {
        const entry = this.entryByID(utteranceID)
        if (
          !entry ||
          (entry.speaker_cluster_id &&
            failedClusters.has(entry.speaker_cluster_id))
        ) {
          failed.add(utteranceID)
          continue
        }
        const result = await this.submit(() =>
          CorrectUtteranceSpeaker({
            request_id: draft.requestID,
            meeting_id: this.meetingID,
            utterance_id: utteranceID,
            expected_revision: entry.speaker_revision,
            value: draft.value,
            reason: '',
          }),
        )
        if (!result) failed.add(utteranceID)
      }
      return failed
    },

    /** saveTextDrafts 使用最后刷新到的 text revision 逐条提交文字。 */
    async saveTextDrafts(
      drafts: Record<string, CorrectionDraft>,
    ): Promise<Set<string>> {
      const failed = new Set<string>()
      for (const [utteranceID, draft] of this.entriesInSeqOrder(drafts)) {
        const entry = this.entryByID(utteranceID)
        if (!entry) {
          failed.add(utteranceID)
          continue
        }
        const result = await this.submit(() =>
          CorrectUtteranceText({
            request_id: draft.requestID,
            meeting_id: this.meetingID,
            utterance_id: utteranceID,
            expected_revision: entry.text_revision,
            value: draft.value,
            reason: '',
          }),
        )
        if (!result) failed.add(utteranceID)
      }
      return failed
    },

    /** submit 统一记录真实失败与 Markdown 投影部分成功，不覆盖未保存草稿。 */
    async submit(action: () => Promise<CorrectionResult>): Promise<boolean> {
      const result = await action()
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return false
      }
      if (result.data.projection_state === 'failed') {
        this.projectionWarning = '修改已保存，原始记录文件待刷新。'
      }
      return true
    },

    /** clusterEntries 选取每个 cluster 的稳定首条 entry，供统一映射保存。 */
    clusterEntries(): Map<string, CorrectionEntry> {
      const entries = new Map<string, CorrectionEntry>()
      for (const entry of this.entries) {
        if (!entry.speaker_cluster_id) continue
        const current = entries.get(entry.speaker_cluster_id)
        if (!current || entry.seq < current.seq)
          entries.set(entry.speaker_cluster_id, entry)
      }
      return entries
    },

    /** entriesInSeqOrder 按会议记录顺序返回当前要提交的草稿。 */
    entriesInSeqOrder(
      drafts: Record<string, CorrectionDraft>,
    ): Array<[string, CorrectionDraft]> {
      return Object.entries(drafts).sort(([leftID], [rightID]) => {
        return (
          (this.entryByID(leftID)?.seq ?? Number.MAX_SAFE_INTEGER) -
          (this.entryByID(rightID)?.seq ?? Number.MAX_SAFE_INTEGER)
        )
      })
    },

    /** entryByID 按 utterance ID 读取最新 SQLite 快照。 */
    entryByID(utteranceID: string): CorrectionEntry | undefined {
      return this.entries.find((entry) => entry.utterance_id === utteranceID)
    },

    /** clearSavedDrafts 仅清除与本次快照 request ID 一致的成功草稿。 */
    clearSavedDrafts(
      snapshot: DraftSnapshot,
      failed: {
        clusters: Set<string>
        speakers: Set<string>
        texts: Set<string>
      },
    ): void {
      this.clearDraftMap(
        this.clusterDrafts,
        snapshot.clusterDrafts,
        failed.clusters,
      )
      this.clearDraftMap(
        this.speakerDrafts,
        snapshot.speakerDrafts,
        failed.speakers,
      )
      this.clearDraftMap(this.textDrafts, snapshot.textDrafts, failed.texts)
    },

    /** clearDraftMap 不覆盖用户在保存期间产生的新输入。 */
    clearDraftMap(
      current: Record<string, CorrectionDraft>,
      snapshot: Record<string, CorrectionDraft>,
      failed: Set<string>,
    ): void {
      for (const [id, draft] of Object.entries(snapshot)) {
        if (!failed.has(id) && current[id]?.requestID === draft.requestID)
          delete current[id]
      }
    },
  },
})
