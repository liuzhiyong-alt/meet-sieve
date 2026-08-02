import { defineStore } from 'pinia'

import {
  GetBuildInfo,
  GetHealth,
  RunEventRoundTrip,
} from '../../wailsjs/go/wails/SystemBinding'

export type SystemViewState = 'starting' | 'setup_required' | 'failed'

/** 将后端 health 状态投影为 Step 0 可展示的最小页面状态。 */
export function resolveSystemViewState(status: string): SystemViewState {
  if (status === 'setup_required') {
    return 'setup_required'
  }
  if (status === 'failed') {
    return 'failed'
  }
  return 'starting'
}

/** isEventRoundTripValid 校验 smoke 模式下真实 binding/event 往返结果。 */
export function isEventRoundTripValid(result: {
  code: number
  data?: { name: string; version: number; data: string }
}): boolean {
  return (
    result.code === 200 &&
    result.data?.name === 'system.event.roundtrip' &&
    result.data.version === 1 &&
    result.data.data === 'step0-smoke'
  )
}

/** useSystemStore 保存前端 health 投影，后端仍是状态事实源。 */
export const useSystemStore = defineStore('system', {
  state: () => ({
    viewState: 'starting' as SystemViewState,
    message: '',
    errorCode: undefined as number | undefined,
  }),
  actions: {
    /** refresh 从 Wails binding 读取一次当前健康状态。 */
    async refresh(): Promise<void> {
      const buildInfo = await GetBuildInfo()
      const result = await GetHealth()
      if (result.code !== 200 || result.data == null) {
        this.viewState = 'failed'
        this.message = result.message
        this.errorCode = result.code
        return
      }

      this.viewState = resolveSystemViewState(result.data.status)
      this.message = result.data.message ?? ''
      this.errorCode = result.data.errorCode

      // 仅 smoke 构建执行真实 event 往返，production 不承担测试开销。
      if (buildInfo.code === 200 && buildInfo.data?.buildMode === 'smoke') {
        const roundTrip = await RunEventRoundTrip('step0-smoke')
        if (!isEventRoundTripValid(roundTrip)) {
          this.viewState = 'failed'
          this.message = roundTrip.message
          this.errorCode = roundTrip.code
        }
      }
    },
  },
})
