import { describe, expect, it } from 'vitest'

import { DirtyEditRegistry } from './dirty'

describe('DirtyEditRegistry', () => {
  it('不把后台任务当作 dirty，保存失败时停留', async () => {
    const registry = new DirtyEditRegistry()
    let dirty = true
    registry.register({
      id: 'minutes',
      label: '会议纪要',
      isDirty: () => dirty,
      canSave: () => true,
      save: async () => false,
      discard: () => {
        dirty = false
      },
    })
    registry.setPrompt(async () => 'save')
    expect(await registry.confirmNavigation()).toBe(false)
    registry.setPrompt(async () => 'discard')
    expect(await registry.confirmNavigation()).toBe(true)
    expect(dirty).toBe(false)
  })
})
