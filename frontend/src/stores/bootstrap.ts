import { defineStore } from 'pinia'

import {
  GetBootstrapState,
  RetryDatabaseUpgrade,
  UseWorkspace,
} from '../../wailsjs/go/wails/BootstrapBinding'

export type BootstrapView =
  'loading' | 'onboarding' | 'unavailable' | 'upgrade' | 'ready' | 'fatal'

export interface BootstrapState {
  phase: string
  reason: string
  message: string
  retryable: boolean
  availableActions: string[]
}

/** resolveBootstrapView 将真实后端 phase 映射为唯一的页面状态，而非前端事实。 */
export function resolveBootstrapView(phase: string): BootstrapView {
  switch (phase) {
    case 'needs_workspace':
    case 'initializing_workspace':
      return 'onboarding'
    case 'upgrading_database':
      return 'upgrade'
    case 'workspace_unavailable':
      return 'unavailable'
    case 'ready':
      return 'ready'
    case 'fatal':
      return 'fatal'
    default:
      return 'loading'
  }
}

/** useBootstrapStore 读取和推进启动状态，所有持久事实仍由 Go coordinator 重建。 */
export const useBootstrapStore = defineStore('bootstrap', {
  state: () => ({
    state: {
      phase: 'checking_workspace',
      reason: 'none',
      message: '',
      retryable: false,
      availableActions: [],
    } as BootstrapState,
    loading: false,
    errorMessage: '',
  }),
  getters: {
    view: (store): BootstrapView => resolveBootstrapView(store.state.phase),
  },
  actions: {
    /** refresh 只读取当前状态，不触发 workspace retry。 */
    async refresh(): Promise<void> {
      this.loading = true
      this.errorMessage = ''
      const result = await GetBootstrapState()
      this.loading = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        this.state = { ...this.state, phase: 'fatal', message: result.message }
        return
      }
      this.applyState(result.data)
    },
    /** useWorkspace 通过 Wails 初始化或接入用户明确选择的目录。 */
    async useWorkspace(path: string): Promise<boolean> {
      this.loading = true
      this.errorMessage = ''
      const result = await UseWorkspace(path)
      this.loading = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return false
      }
      this.applyState(result.data)
      return true
    },
    /** retryDatabaseUpgrade 从后端 locator/SQLite 状态重新构建升级流程。 */
    async retryDatabaseUpgrade(): Promise<void> {
      this.loading = true
      this.errorMessage = ''
      const result = await RetryDatabaseUpgrade()
      this.loading = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return
      }
      this.applyState(result.data)
    },
    /** applyState 将 Go DTO 显式转为前端 camelCase 状态。 */
    applyState(state: {
      phase: string
      reason: string
      message: string
      retryable: boolean
      available_actions: string[]
    }): void {
      this.state = {
        phase: state.phase,
        reason: state.reason,
        message: state.message,
        retryable: state.retryable,
        availableActions: state.available_actions ?? [],
      }
    },
  },
})
