import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { usePeopleStore } from './people'

const bindings = vi.hoisted(() => ({
  ListMembers: vi.fn(),
  ListGroups: vi.fn(),
  CreateMember: vi.fn(),
  CreateGroup: vi.fn(),
  DeleteMember: vi.fn(),
}))

vi.mock('../../wailsjs/go/wails/PeopleBinding', () => bindings)

describe('people store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('refreshes members and groups from the same backend projection', async () => {
    bindings.ListMembers.mockResolvedValue({
      code: 200,
      data: [{ id: 'm1', name: '张三' }],
    })
    bindings.ListGroups.mockResolvedValue({
      code: 200,
      data: [{ id: 'g1', name: '研发组', members: [] }],
    })

    const store = usePeopleStore()
    await store.refresh()

    expect(store.members).toHaveLength(1)
    expect(store.groups).toHaveLength(1)
    expect(store.errorMessage).toBe('')
  })

  it('keeps backend failure semantics instead of reporting success', async () => {
    bindings.ListMembers.mockResolvedValue({
      code: 500,
      message: '系统内部错误',
    })
    bindings.ListGroups.mockResolvedValue({ code: 200, data: [] })

    const store = usePeopleStore()
    await store.refresh()

    expect(store.errorMessage).toBe('系统内部错误')
  })

  it('refreshes members and groups only after member deletion succeeds', async () => {
    bindings.ListMembers.mockResolvedValueOnce({
      code: 200,
      data: [{ id: 'm1', name: '张三' }],
    })
    bindings.ListGroups.mockResolvedValueOnce({
      code: 200,
      data: [{ id: 'g1', name: '研发组', members: [{ member_id: 'm1' }] }],
    })
    bindings.DeleteMember.mockResolvedValue({ code: 200, data: true })
    const store = usePeopleStore()
    await store.refresh()
    bindings.ListMembers.mockResolvedValue({ code: 200, data: [] })
    bindings.ListGroups.mockResolvedValue({
      code: 200,
      data: [{ id: 'g1', name: '研发组', members: [] }],
    })

    expect(await store.deleteMember('m1')).toBe(true)
    expect(store.members).toEqual([])
    expect(store.groups[0]?.members).toEqual([])
  })

  it('keeps the current row visible when member deletion fails', async () => {
    bindings.ListMembers.mockResolvedValue({
      code: 200,
      data: [{ id: 'm1', name: '张三' }],
    })
    bindings.ListGroups.mockResolvedValue({ code: 200, data: [] })
    bindings.DeleteMember.mockResolvedValue({
      code: 500,
      message: '声纹样本删除未完成，请重试',
    })
    const store = usePeopleStore()
    await store.refresh()

    expect(await store.deleteMember('m1')).toBe(false)
    expect(store.members).toHaveLength(1)
    expect(store.errorMessage).toBe('声纹样本删除未完成，请重试')
  })
})
