// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { describe, expect, it } from 'vitest'

import { createMeetSieveRouter } from '../../router'
import PageBreadcrumb from './PageBreadcrumb.vue'

describe('PageBreadcrumb', () => {
  it('一级页面只显示当前页，不统一添加首页', async () => {
    const router = createMeetSieveRouter(createMemoryHistory())
    await router.push('/meetings/new')
    await router.isReady()
    const wrapper = mount(PageBreadcrumb, { global: { plugins: [router] } })

    expect(wrapper.findAll('a')).toHaveLength(0)
    expect(wrapper.text()).toBe('开始会议')
  })

  it('旧成员详情地址回到成员列表并由查询参数打开编辑弹窗', async () => {
    const router = createMeetSieveRouter(createMemoryHistory())
    await router.push('/people/members/member-uuid?from=members')
    await router.isReady()
    const wrapper = mount(PageBreadcrumb, { global: { plugins: [router] } })

    expect(wrapper.text()).toBe('小组与成员')
    expect(wrapper.findAll('a')).toHaveLength(0)
    expect(router.currentRoute.value.path).toBe('/people')
    expect(router.currentRoute.value.query).toEqual({
      tab: 'members',
      edit: 'member-uuid',
    })
  })
})
