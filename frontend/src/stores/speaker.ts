import { defineStore } from 'pinia'

import { GetSpeakerStatus } from '../../wailsjs/go/wails/CorrectionBinding'

export type SpeakerAutomationState =
  | 'unknown'
  | 'ready'
  | 'model_unavailable'
  | 'profile_missing'
  | 'profile_mismatch'
  | 'voice_rebuild_required'

/** normalizeSpeakerState 拒绝未知后端状态，避免前端误把新增状态显示为可用。 */
function normalizeSpeakerState(value: string): SpeakerAutomationState {
  switch (value) {
    case 'ready':
    case 'model_unavailable':
    case 'profile_missing':
    case 'profile_mismatch':
    case 'voice_rebuild_required':
      return value
    default:
      return 'unknown'
  }
}

/** useSpeakerStore 保存可由后端随时重建的自动说话人识别门禁。 */
export const useSpeakerStore = defineStore('speaker', {
  state: () => ({
    meetingID: '',
    state: 'unknown' as SpeakerAutomationState,
    errorCode: '',
    errorMessage: '',
  }),
  actions: {
    /** refresh 从后端读取模型、正式校准档案和声纹向量的综合可用状态。 */
    async refresh(meetingID: string): Promise<void> {
      if (!meetingID) return
      const result = await GetSpeakerStatus(meetingID)
      this.meetingID = meetingID
      if (result.code !== 200 || !result.data) {
        this.state = 'unknown'
        this.errorCode = result.errorCode ?? ''
        this.errorMessage = result.message
        return
      }
      this.state = normalizeSpeakerState(result.data.state)
      this.errorCode = result.data.error_code ?? ''
      this.errorMessage = ''
    },
  },
})
