import { defineStore } from 'pinia'

import {
  CancelGuestUpload,
  GetLANStatus,
  ListLANInterfaces,
  RetryLAN,
  StopLAN,
} from '../../wailsjs/go/wails/LANBinding'

export interface LANInterface {
  id: string
  name: string
  address: string
  default_route: boolean
}

export interface LANUpload {
  request_id: string
  name: string
  written: number
  total: number
}

export interface LANStatus {
  state: string
  meeting_id?: string
  interface_id?: string
  address?: string
  join_url?: string
  error_code?: string
  online_count: number
  active_uploads: LANUpload[]
}

const initialStatus = (): LANStatus => ({
  state: 'disabled',
  online_count: 0,
  active_uploads: [],
})

/** useLANStore 只保存可由 Wails Binding 重建的 LAN 选择和运行时投影。 */
export const useLANStore = defineStore('lan', {
  state: () => ({
    enabled: false,
    interfaces: [] as LANInterface[],
    selectedInterfaceID: '',
    recommendedID: '',
    warning: '',
    status: initialStatus(),
    loading: false,
    errorMessage: '',
  }),
  getters: {
    selectedInterface(state): LANInterface | undefined {
      return state.interfaces.find(
        (item) => item.id === state.selectedInterfaceID,
      )
    },
  },
  actions: {
    /** loadInterfaces 枚举安全私网并仅采用后端明确给出的默认路由推荐。 */
    async loadInterfaces(): Promise<void> {
      this.loading = true
      this.errorMessage = ''
      const result = await ListLANInterfaces()
      this.loading = false
      if (result.code !== 200 || !result.data) {
        this.interfaces = []
        this.selectedInterfaceID = ''
        this.errorMessage = result.message
        return
      }
      this.interfaces = result.data.interfaces ?? []
      this.recommendedID = result.data.recommended_id ?? ''
      this.warning = result.data.warning ?? ''
      if (
        !this.interfaces.some((item) => item.id === this.selectedInterfaceID)
      ) {
        this.selectedInterfaceID = this.recommendedID
      }
    },
    /** refreshStatus 每次页面恢复都以 Go Runtime 为事实源。 */
    async refreshStatus(): Promise<void> {
      const result = await GetLANStatus()
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return
      }
      this.status = {
        ...result.data,
        active_uploads: result.data.active_uploads ?? [],
      }
    },
    /** retry 使用当前显式选择生成新入口，旧入口立即失效。 */
    async retry(): Promise<boolean> {
      if (!this.selectedInterfaceID) return false
      this.loading = true
      this.errorMessage = ''
      const result = await RetryLAN(this.selectedInterfaceID)
      this.loading = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        await this.refreshStatus()
        return false
      }
      this.status = {
        ...result.data,
        active_uploads: result.data.active_uploads ?? [],
      }
      return true
    },
    /** stop 幂等关闭当前访客入口。 */
    async stop(): Promise<boolean> {
      const result = await StopLAN()
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return false
      }
      this.status = {
        ...result.data,
        active_uploads: result.data.active_uploads ?? [],
      }
      return true
    },
    /** cancelUpload 取消真实活动上传并刷新宿主投影。 */
    async cancelUpload(requestID: string): Promise<boolean> {
      const result = await CancelGuestUpload(requestID)
      if (result.code !== 200 || !result.data?.cancelled) return false
      await this.refreshStatus()
      return true
    },
    /** resetForCreate 清理上一会议仅存在内存中的入口信息。 */
    resetForCreate(): void {
      this.status = initialStatus()
      this.errorMessage = ''
    },
  },
})
