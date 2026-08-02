import { describe, expect, it } from 'vitest'

import { isEventRoundTripValid, resolveSystemViewState } from './system'

describe('resolveSystemViewState', () => {
  it('将未初始化状态投影为设置引导页', () => {
    expect(resolveSystemViewState('setup_required')).toBe('setup_required')
  })

  it('将失败状态投影为错误页', () => {
    expect(resolveSystemViewState('failed')).toBe('failed')
  })
})

describe('isEventRoundTripValid', () => {
  it('只接受版本和 payload 均匹配的成功事件', () => {
    expect(
      isEventRoundTripValid({
        code: 200,
        data: {
          name: 'system.event.roundtrip',
          version: 1,
          data: 'step0-smoke',
        },
      }),
    ).toBe(true)
  })

  it('拒绝错误版本的事件', () => {
    expect(
      isEventRoundTripValid({
        code: 200,
        data: {
          name: 'system.event.roundtrip',
          version: 2,
          data: 'step0-smoke',
        },
      }),
    ).toBe(false)
  })
})
