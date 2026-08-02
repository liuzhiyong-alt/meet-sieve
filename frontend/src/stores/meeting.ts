import { defineStore } from 'pinia'

import {
  EndMeeting,
  GetActiveMeeting,
  GetCreateDraft,
  GetLatestInterruptedMeeting,
  RetryMeetingRecovery,
  StartMeeting,
} from '../../wailsjs/go/wails/MeetingBinding'
import { GetMeetingPeopleOptions } from '../../wailsjs/go/wails/PeopleBinding'
import { ListInputDevices } from '../../wailsjs/go/wails/VoiceBinding'

export interface MeetingProjection {
  id: string
  meeting_no: string
  subject: string
  lifecycle_state: string
  local_save_state: string
  realtime_asr_state: string
  started_at?: number
  ended_at?: number
  updated_at: number
}

export interface MeetingMemberOption {
  id: string
  name: string
  sort_order: number
  voice_readiness: string
}

export interface MeetingGroupOption {
  id: string
  name: string
  default_lan_enabled: boolean
  members: MeetingMemberOption[]
}

export interface MicrophoneOption {
  id: string
  name: string
  is_default: boolean
}

export interface StartMeetingRequest {
  meetingNo: string
  suggestedMeetingNo: string
  subject: string
  memberIds: string[]
  temporaryNames: string[]
  microphoneId: string
  asrMode: 'realtime' | 'record_only'
}

/** useMeetingStore 只保存可由 Binding 查询重建的会议 UI 投影。 */
export const useMeetingStore = defineStore('meeting', {
  state: () => ({
    screen: 'start' as 'start' | 'live' | 'interrupted',
    current: null as MeetingProjection | null,
    draft: { meetingNo: '', subject: '' },
    members: [] as MeetingMemberOption[],
    groups: [] as MeetingGroupOption[],
    microphones: [] as MicrophoneOption[],
    loading: false,
    saving: false,
    errorMessage: '',
  }),
  actions: {
    /** refreshCurrentMeeting 在刷新或重启后优先恢复活动录音，其次恢复中断结果页。 */
    async refreshCurrentMeeting(): Promise<void> {
      if (this.current?.lifecycle_state === 'ended') return
      this.errorMessage = ''
      const active = await GetActiveMeeting()
      if (active.code !== 200) {
        this.errorMessage = active.message
        return
      }
      if (active.data?.active && active.data.meeting) {
        this.current = active.data.meeting
        this.screen = 'live'
        return
      }
      const interrupted = await GetLatestInterruptedMeeting()
      if (interrupted.code !== 200) {
        this.errorMessage = interrupted.message
        return
      }
      if (interrupted.data?.meeting) {
        this.current = interrupted.data.meeting
        this.screen = 'interrupted'
        return
      }
      this.current = null
      this.screen = 'start'
    },
    /** loadCreateScreen 并行读取草稿、参会候选和系统真实麦克风。 */
    async loadCreateScreen(): Promise<void> {
      this.loading = true
      this.errorMessage = ''
      const [draft, people, microphones] = await Promise.all([
        GetCreateDraft(),
        GetMeetingPeopleOptions(),
        ListInputDevices(),
      ])
      this.loading = false
      const failure = [draft, people, microphones].find(
        (result) => result.code !== 200 || !result.data,
      )
      if (failure) {
        this.errorMessage = failure.message
        return
      }
      this.draft = {
        meetingNo: draft.data!.suggested_meeting_no,
        subject: draft.data!.default_subject,
      }
      this.groups = people.data!.groups ?? []
      this.members = people.data!.members ?? []
      this.microphones = microphones.data ?? []
    },
    /** startMeeting 仅在后端首帧与状态事务均提交后切换会议中页面。 */
    async startMeeting(request: StartMeetingRequest): Promise<boolean> {
      this.saving = true
      this.errorMessage = ''
      const result = await StartMeeting({
        meeting_no: request.meetingNo,
        suggested_meeting_no: request.suggestedMeetingNo,
        subject: request.subject,
        member_ids: request.memberIds,
        temporary_participant_names: request.temporaryNames,
        microphone_id: request.microphoneId,
        local_timezone:
          Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai',
        asr_mode: request.asrMode,
      })
      this.saving = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return false
      }
      this.current = result.data
      this.screen = 'live'
      return true
    },
    /** endMeeting 等待后端唯一安全收尾完成后才更新终态。 */
    async endMeeting(): Promise<boolean> {
      if (!this.current) return false
      this.saving = true
      this.errorMessage = ''
      const result = await EndMeeting(this.current.id)
      this.saving = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        await this.refreshCurrentMeeting()
        return false
      }
      this.current = result.data
      return true
    },
    /** retryRecovery 只重试中断会议的文件对账，不请求恢复原录音。 */
    async retryRecovery(): Promise<boolean> {
      if (!this.current) return false
      this.saving = true
      this.errorMessage = ''
      const result = await RetryMeetingRecovery(this.current.id)
      this.saving = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return false
      }
      this.current = result.data
      return true
    },
    /** startNewMeeting 离开恢复结果页，新会议仍会创建新的 UUID 和录音流。 */
    startNewMeeting(): void {
      this.current = null
      this.screen = 'start'
    },
  },
})
