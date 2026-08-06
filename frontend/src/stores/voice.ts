import { defineStore } from 'pinia'

import {
  CancelVoiceRecording,
  ChooseVoiceSample,
  DeleteAllVoiceSamples,
  DeleteVoiceSample,
  GetVoiceRecordingState,
  ListInputDevices,
  ListVoiceSamples,
  ProcessVoiceSample,
  RebuildVoiceEmbeddings,
  StartVoiceRecording,
  StopVoiceRecording,
} from '../../wailsjs/go/wails/VoiceBinding'

export interface VoiceSampleProjection {
  id: string
  member_id: string
  duration_ms: number
  source_kind: string
  source_name: string
  environment_kind: string
  processing_state: string
  quality_state: string
  quality_code: string
  created_at: number
}

export interface InputDeviceProjection {
  id: string
  name: string
  is_default: boolean
  channel_count: number
}

/** useVoiceStore 只保存当前成员声纹页面的后端投影和短暂交互状态。 */
export const useVoiceStore = defineStore('voice', {
  state: () => ({
    memberId: '',
    samples: [] as VoiceSampleProjection[],
    devices: [] as InputDeviceProjection[],
    selectedToken: '',
    selectedFileName: '',
    recording: false,
    startingRecording: false,
    recordingLevel: 0,
    recordingDurationMS: 0,
    loading: false,
    processing: false,
    errorMessage: '',
    notice: '',
  }),
  actions: {
    /** open 从 Go/SQLite 恢复指定成员样本，并读取当前输入设备。 */
    async open(memberId: string): Promise<void> {
      this.memberId = memberId
      this.loading = true
      this.errorMessage = ''
      const [samples, devices] = await Promise.all([
        ListVoiceSamples(memberId),
        ListInputDevices(),
      ])
      this.loading = false
      if (samples.code !== 200 || !samples.data) {
        this.errorMessage = samples.message
        return
      }
      this.samples = samples.data
      if (devices.code === 200 && devices.data) this.devices = devices.data
    },
    /** chooseWAV 使用后端系统对话框取得一次性文件令牌。 */
    async chooseWAV(): Promise<void> {
      this.errorMessage = ''
      const result = await ChooseVoiceSample()
      if (result.code !== 200 || !result.data) {
        if (result.message) this.errorMessage = result.message
        return
      }
      this.selectedToken = result.data.token
      this.selectedFileName = result.data.file_name
    },
    /** processWAV 消费选择令牌，完成真实规范化、质量检测与 embedding。 */
    async processWAV(environment: string): Promise<boolean> {
      if (!this.selectedToken) return false
      this.processing = true
      this.errorMessage = ''
      this.notice = ''
      const result = await ProcessVoiceSample(
        this.memberId,
        environment,
        this.selectedToken,
      )
      this.processing = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        this.selectedToken = ''
        this.selectedFileName = ''
        await this.refreshSamples()
        return false
      }
      this.selectedToken = ''
      this.selectedFileName = ''
      await this.refreshSamples()
      this.notice = '声纹样本已保存到本机。'
      return true
    },
    /** startRecording 打开选定麦克风并冻结成员、环境归属。 */
    async startRecording(
      deviceId: string,
      environment: string,
    ): Promise<boolean> {
      this.errorMessage = ''
      this.notice = ''
      this.startingRecording = true
      const result = await StartVoiceRecording(
        this.memberId,
        deviceId,
        environment,
      )
      this.startingRecording = false
      this.recording = result.code === 200 && result.data === true
      this.recordingLevel = 0
      this.recordingDurationMS = 0
      this.errorMessage = this.recording ? '' : result.message
      return this.recording
    },
    /** refreshRecordingState 读取真实 PCM 峰值和采集时长，不在前端伪造音量。 */
    async refreshRecordingState(): Promise<void> {
      if (!this.recording) return
      const result = await GetVoiceRecordingState()
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return
      }
      this.recording = result.data.recording
      this.recordingLevel = Math.max(0, Math.min(1, result.data.level))
      this.recordingDurationMS = result.data.duration_ms
    },
    /** stopRecording 停止并处理录音，完成前保持 processing。 */
    async stopRecording(): Promise<boolean> {
      this.processing = true
      const result = await StopVoiceRecording()
      this.recording = false
      this.processing = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        await this.refreshSamples()
        return false
      }
      await this.refreshSamples()
      this.recordingLevel = 0
      this.recordingDurationMS = 0
      this.notice = '声纹样本已保存到本机。'
      return true
    },
    /** cancelRecording 丢弃当前录音，不创建样本。 */
    async cancelRecording(): Promise<void> {
      const result = await CancelVoiceRecording()
      this.recording = false
      this.recordingLevel = 0
      this.recordingDurationMS = 0
      this.errorMessage = result.code === 200 ? '' : result.message
    },
    /** deleteSample 永久删除单个样本并刷新后端投影。 */
    async deleteSample(sampleId: string): Promise<void> {
      const result = await DeleteVoiceSample(this.memberId, sampleId)
      if (result.code !== 200) this.errorMessage = result.message
      await this.refreshSamples()
    },
    /** deleteAll 永久删除当前成员全部样本；任何部分失败均呈现错误。 */
    async deleteAll(): Promise<void> {
      const result = await DeleteAllVoiceSamples(this.memberId)
      if (result.code !== 200) this.errorMessage = result.message
      await this.refreshSamples()
    },
    /** rebuild 继续当前模型的历史向量重建。 */
    async rebuild(): Promise<void> {
      this.processing = true
      const result = await RebuildVoiceEmbeddings()
      this.processing = false
      if (result.code !== 200) this.errorMessage = result.message
      await this.refreshSamples()
    },
    /** refreshSamples 仅从 SQLite 查询样本，不依赖历史事件。 */
    async refreshSamples(): Promise<void> {
      if (!this.memberId) return
      const result = await ListVoiceSamples(this.memberId)
      if (result.code === 200 && result.data) this.samples = result.data
    },
    /** reset 清除关闭弹窗后不应跨成员保留的短暂状态。 */
    reset(): void {
      this.memberId = ''
      this.samples = []
      this.selectedToken = ''
      this.selectedFileName = ''
      this.errorMessage = ''
      this.notice = ''
      this.recording = false
      this.startingRecording = false
      this.recordingLevel = 0
      this.recordingDurationMS = 0
      this.processing = false
    },
  },
})
