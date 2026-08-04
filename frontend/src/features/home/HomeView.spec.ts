// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const queryBindings = vi.hoisted(() => ({ GetHome: vi.fn() }))
const peopleBindings = vi.hoisted(() => ({ GetMeetingPeopleOptions: vi.fn() }))
vi.mock('../../../wailsjs/go/wails/QueryBinding', () => queryBindings)
vi.mock('../../../wailsjs/go/wails/PeopleBinding', () => peopleBindings)

import HomeView from './HomeView.vue'

describe('HomeView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    queryBindings.GetHome.mockResolvedValue({
      code: 200,
      data: {
        continuation: {
          id: 'meeting-1',
          meeting_no: 'M-01',
          subject: 'AI 会议助手产品设计',
          started_at: 1000,
          participants: ['刘毅'],
          highest_status: 'gap_conflict',
          primary_action: {
            kind: 'resolve_gap',
            label: '处理缺口',
            target_id: 'gap-1',
            enabled: true,
          },
        },
        remaining: 2,
        recent_meetings: [],
      },
    })
    peopleBindings.GetMeetingPeopleOptions.mockResolvedValue({
      code: 200,
      data: {
        groups: [
          {
            id: 'group-1',
            name: '产品周会',
            members: [
              { id: 'm1', name: '刘毅' },
              { id: 'm2', name: '陈然' },
            ],
          },
        ],
        members: [],
      },
    })
  })

  it('使用稳定顺序第一小组快速开始且不加载创建页设备', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/home', component: HomeView },
        { path: '/meetings/new', component: { template: '<p>new</p>' } },
        {
          path: '/meetings/:id/gaps/:gap',
          component: { template: '<p>gap</p>' },
        },
        { path: '/meetings', component: { template: '<p>records</p>' } },
        { path: '/people', component: { template: '<p>people</p>' } },
      ],
    })
    await router.push('/home')
    await router.isReady()
    const wrapper = mount(HomeView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('产品周会')
    expect(wrapper.text()).toContain('2 位成员')
    expect(
      wrapper
        .findAll('a')
        .find((item) => item.text() === '选择此小组')!
        .attributes('href'),
    ).toBe('/meetings/new?group=group-1')
    expect(peopleBindings.GetMeetingPeopleOptions).toHaveBeenCalledTimes(1)
  })

  it('继续处理只使用后端 primary_action 导航', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/home', component: HomeView },
        { path: '/meetings/new', component: { template: '<p>new</p>' } },
        {
          path: '/meetings/:id/gaps/:gap',
          component: { template: '<p>gap</p>' },
        },
        { path: '/meetings', component: { template: '<p>records</p>' } },
        { path: '/people', component: { template: '<p>people</p>' } },
      ],
    })
    await router.push('/home')
    await router.isReady()
    const wrapper = mount(HomeView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    await wrapper.get('[data-home-primary-action]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe(
      '/meetings/meeting-1/gaps/gap-1',
    )
  })

  it('空小组引导使用按钮链接并与说明保持独立间距', async () => {
    peopleBindings.GetMeetingPeopleOptions.mockResolvedValue({
      code: 200,
      data: { groups: [], members: [] },
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/home', component: HomeView },
        { path: '/meetings/new', component: { template: '<p>new</p>' } },
        { path: '/meetings', component: { template: '<p>records</p>' } },
        { path: '/people', component: { template: '<p>people</p>' } },
      ],
    })
    await router.push('/home')
    await router.isReady()
    const wrapper = mount(HomeView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.get('.ms-home-empty-action').text()).toContain('创建小组')
    for (const label of ['创建会议', '创建小组', '查看全部']) {
      const link = wrapper.findAll('a').find((item) => item.text() === label)
      expect(link?.classes()).toContain('ms-button')
    }
  })
})
