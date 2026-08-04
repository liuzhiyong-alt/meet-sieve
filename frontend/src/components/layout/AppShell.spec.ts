// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it } from 'vitest'

import AppShell from './AppShell.vue'

describe('AppShell', () => {
  it('使用确认品牌标识并为当前主导航提供页面语义', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/home', component: { template: '<p>home</p>' } },
        { path: '/meetings/new', component: { template: '<p>new</p>' } },
        { path: '/meetings/live', component: { template: '<p>live</p>' } },
        { path: '/meetings', component: { template: '<p>records</p>' } },
        { path: '/people', component: { template: '<p>people</p>' } },
        {
          path: '/settings/general',
          component: { template: '<p>settings</p>' },
        },
      ],
    })
    await router.push('/home')
    await router.isReady()
    const wrapper = mount(AppShell, { global: { plugins: [router] } })

    expect(wrapper.get('.ms-brand-mark').text()).toBe('M')
    expect(wrapper.get('a[href="/home"]').attributes('aria-current')).toBe(
      'page',
    )
    expect(wrapper.get('a[href="/people"]').text()).toBe('小组与成员')
  })

  it('活动会议复用“开始会议”入口，不增加“会议进行中”菜单', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/home', component: { template: '<p>home</p>' } },
        { path: '/meetings/new', component: { template: '<p>new</p>' } },
        { path: '/meetings/live', component: { template: '<p>live</p>' } },
        { path: '/meetings', component: { template: '<p>records</p>' } },
        { path: '/people', component: { template: '<p>people</p>' } },
        {
          path: '/settings/general',
          component: { template: '<p>settings</p>' },
        },
      ],
    })
    await router.push('/meetings/live')
    await router.isReady()
    const wrapper = mount(AppShell, {
      props: { activeMeeting: true },
      global: { plugins: [router] },
    })

    expect(wrapper.text()).not.toContain('会议进行中')
    expect(wrapper.get('.ms-nav a[href="/meetings/live"]').text()).toBe(
      '开始会议',
    )
  })
})
