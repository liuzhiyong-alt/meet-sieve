import { describe, expect, it } from 'vitest'

import { resolveBootstrapView } from './bootstrap'

describe('resolveBootstrapView', () => {
  it('将首次选择状态映射为工作目录引导页', () => {
    expect(resolveBootstrapView('needs_workspace')).toBe('onboarding')
  })

  it('将升级状态映射为阻断升级页', () => {
    expect(resolveBootstrapView('upgrading_database')).toBe('upgrade')
  })

  it('将就绪状态映射为真实 General 设置页', () => {
    expect(resolveBootstrapView('ready')).toBe('ready')
  })
})
