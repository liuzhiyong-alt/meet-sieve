import { describe, expect, it } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import { createMeetSieveRouter } from './index'

describe('正式 Hash 路由', () => {
  it('规范化未知路由和无效设置分类', async () => {
    const router = createMeetSieveRouter(createMemoryHistory())
    await router.push('/does-not-exist')
    expect(router.currentRoute.value.path).toBe('/home')
    await router.push('/settings/future')
    expect(router.currentRoute.value.path).toBe('/settings/general')
  })

  it('保留会议详情 tab、筛选和游标 URL', async () => {
    const router = createMeetSieveRouter(createMemoryHistory())
    await router.push('/meetings/meeting-id?tab=messages&after=42')
    expect(router.currentRoute.value.query).toMatchObject({
      tab: 'messages',
      after: '42',
    })
    await router.push('/meetings?search=评审&status=saved&cursor=opaque')
    expect(router.currentRoute.value.query.cursor).toBe('opaque')
  })

  it('旧小组和成员详情地址重定向到可恢复的编辑弹窗', async () => {
    const router = createMeetSieveRouter(createMemoryHistory())

    await router.push('/people/groups/group-1')
    expect(router.currentRoute.value.fullPath).toBe(
      '/people?tab=groups&edit=group-1',
    )

    await router.push('/people/members/member-1')
    expect(router.currentRoute.value.fullPath).toBe(
      '/people?tab=members&edit=member-1',
    )
  })
})
