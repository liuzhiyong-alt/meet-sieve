import { defineStore } from 'pinia'

import {
  DownloadOfficialVoiceModel,
  GetVoiceModelState,
  ImportOfflineVoiceModel,
} from '../../wailsjs/go/wails/VoiceModelBinding'

export interface VoiceModelProjection {
  state: string
  usable: boolean
  modelId: string
  modelName: string
  modelVersion: string
  modelSize: number
  location: string
}

/** useVoiceModelStore 保存设置页读取到的官方模型状态。 */
export const useVoiceModelStore = defineStore('voice-model', {
  state: () => ({
    model: {
      state: 'missing',
      usable: false,
      modelId: 'campplus',
      modelName: 'CAM++ 中文通用',
      modelVersion: '',
      modelSize: 0,
      location: '本机应用数据目录',
    } as VoiceModelProjection,
    loading: false,
    errorMessage: '',
  }),
  actions: {
    /** refresh 从 Go 重新校验模型文件与运行时状态。 */
    async refresh(): Promise<void> {
      const result = await GetVoiceModelState()
      if (result.code === 200 && result.data) this.model = result.data
      else this.errorMessage = result.message
    },
    /** download 下载并校验固定 GitHub Release 官方包。 */
    async download(): Promise<void> {
      await this.run(DownloadOfficialVoiceModel)
    },
    /** importOffline 通过系统选择器导入与下载完全相同的官方包。 */
    async importOffline(): Promise<void> {
      await this.run(ImportOfflineVoiceModel)
    },
    /** run 统一处理模型安装动作并保留后端真实状态。 */
    async run(action: typeof DownloadOfficialVoiceModel): Promise<void> {
      this.loading = true
      this.errorMessage = ''
      const result = await action()
      this.loading = false
      if (result.code === 200 && result.data) this.model = result.data
      else this.errorMessage = result.message
    },
  },
})
