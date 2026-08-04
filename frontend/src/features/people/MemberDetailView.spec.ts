// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const bindings = vi.hoisted(() => ({
  DeleteAllMemberVoiceSamples: vi.fn(),
  GetMemberDetail: vi.fn(),
  UpdateMember: vi.fn(),
}))
vi.mock('../../../wailsjs/go/wails/PeopleBinding', () => bindings)

import MemberDetailView from './MemberDetailView.vue'

describe('MemberDetailView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    bindings.GetMemberDetail.mockResolvedValue({
      code: 200,
      data: {
        member: {
          id: 'member-1',
          name: '刘毅',
          notes: '产品负责人',
          accepted_sample_count: 2,
        },
        revision: 10,
        group_count: 2,
        historical_meetings: 3,
      },
    })
    bindings.UpdateMember.mockResolvedValue({ code: 200, data: {} })
  })

  it('详情页承担资料编辑和声纹管理但不提供成员删除或归档', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/people', component: { template: '<p>people</p>' } },
        {
          path: '/people/members/:id',
          component: MemberDetailView,
          props: true,
        },
      ],
    })
    await router.push('/people/members/member-1?from=members')
    await router.isReady()
    const wrapper = mount(MemberDetailView, {
      props: { id: 'member-1' },
      global: { plugins: [router] },
    })
    await flushPromises()

    expect(wrapper.text()).not.toContain('活动成员')
    expect(wrapper.text()).not.toContain('归档成员')
    expect(wrapper.text()).not.toContain('删除成员')
    expect(wrapper.text()).toContain('保存更改')
    expect(wrapper.text()).toContain('删除全部声纹')

    await wrapper.get('input').setValue('刘毅（产品）')
    await wrapper
      .findAll('button')
      .find((item) => item.text() === '保存更改')!
      .trigger('click')
    await flushPromises()
    expect(bindings.UpdateMember).toHaveBeenCalledWith(
      'member-1',
      expect.objectContaining({ name: '刘毅（产品）', revision: 10 }),
    )
  })
})
