// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const peopleBindings = vi.hoisted(() => ({
  CreateGroup: vi.fn(),
  CreateMember: vi.fn(),
  DeleteGroup: vi.fn(),
  DeleteMember: vi.fn(),
  GetGroup: vi.fn(),
  GetMemberDetail: vi.fn(),
  ListGroups: vi.fn(),
  ListMembers: vi.fn(),
  UpdateGroup: vi.fn(),
  UpdateMember: vi.fn(),
}))

vi.mock('../../../wailsjs/go/wails/PeopleBinding', () => peopleBindings)
vi.mock('../../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn(() => vi.fn()),
}))
vi.mock('../../stores/voice', () => ({
  useVoiceStore: () => ({
    samples: [],
    devices: [],
    recording: false,
    startingRecording: false,
    processing: false,
    loading: false,
    errorMessage: '',
    selectedFileName: '',
    selectedToken: '',
    recordingDurationMS: 0,
    recordingLevel: 0,
    open: vi.fn(),
    reset: vi.fn(),
    deleteAll: vi.fn(),
    cancelRecording: vi.fn(),
  }),
}))

import { dirtyEditRegistry } from '../../router/dirty'
import PeopleView from './PeopleView.vue'

describe('PeopleView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    peopleBindings.ListMembers.mockResolvedValue({
      code: 200,
      data: [
        {
          id: 'member-1',
          name: '刘毅',
          notes: '产品负责人',
          accepted_sample_count: 2,
          rejected_sample_count: 0,
          voice_readiness: 'ready',
        },
      ],
    })
    peopleBindings.ListGroups.mockResolvedValue({ code: 200, data: [] })
    peopleBindings.GetMemberDetail.mockResolvedValue({
      code: 200,
      data: {
        member: {
          id: 'member-1',
          name: '刘毅',
          notes: '产品负责人',
          accepted_sample_count: 2,
        },
        revision: 7,
      },
    })
  })

  it('成员行只保留编辑、管理声纹和删除三个职责动作', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/people', component: PeopleView },
        {
          path: '/people/members/:id',
          component: { template: '<p>member</p>' },
        },
      ],
    })
    await router.push('/people?tab=members')
    await router.isReady()
    const wrapper = mount(PeopleView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()
    expect(
      wrapper
        .findAll('[role="tab"]')
        .find((item) => item.text() === '成员')!
        .attributes('aria-selected'),
    ).toBe('true')

    const row = wrapper.get('.ms-list-item')
    const actions = row.findAll('.ms-actions-inline > *')
    expect(actions.map((item) => item.text())).toEqual([
      '编辑',
      '管理声纹',
      '删除',
    ])
    expect(actions[0]?.attributes('href')).toBeUndefined()
    expect(row.text()).not.toContain('详情')
    expect(row.text()).not.toContain('归档')

    await actions[0]!.trigger('click')
    await flushPromises()
    const editDialog = wrapper.get('[data-edit-member-dialog]')
    expect(editDialog.text()).toContain('编辑成员')
    expect(editDialog.text()).toContain('姓名')
    expect(editDialog.text()).toContain('备注')
    expect(editDialog.text()).not.toContain('管理声纹')
    await editDialog.get('input').setValue('刘毅（更新）')
    expect(
      dirtyEditRegistry
        .dirtyEditors()
        .some((editor) => editor.label === '成员资料'),
    ).toBe(true)

    await editDialog
      .findAll('button')
      .find((item) => item.text() === '取消')!
      .trigger('click')
    await flushPromises()
    await actions[1]!.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toMatchObject({
      tab: 'members',
      voice: 'member-1',
    })

    await actions[2]!.trigger('click')
    const confirmation = wrapper.get('[role="alertdialog"]')
    expect(confirmation.text()).toContain('当前小组')
    expect(confirmation.text()).toContain('声纹')
    expect(confirmation.text()).toContain('历史会议')
  })

  it('小组卡只提供编辑和删除动作', async () => {
    peopleBindings.ListGroups.mockResolvedValue({
      code: 200,
      data: [
        {
          id: 'group-1',
          name: '产品周会',
          default_lan_enabled: true,
          members: [],
          updated_at: 100,
        },
      ],
    })
    peopleBindings.GetGroup.mockResolvedValue({
      code: 200,
      data: {
        id: 'group-1',
        name: '产品周会',
        default_lan_enabled: true,
        members: [],
        updated_at: 100,
      },
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/people', component: PeopleView }],
    })
    await router.push('/people?tab=groups')
    await router.isReady()
    const wrapper = mount(PeopleView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const card = wrapper.get('.ms-people-card')
    expect(
      card.findAll('.ms-people-card-actions > *').map((item) => item.text()),
    ).toEqual(['编辑', '删除'])
    expect(card.text()).not.toContain('详情')

    await card
      .findAll('.ms-people-card-actions > *')
      .find((item) => item.text() === '编辑')!
      .trigger('click')
    await flushPromises()
    const editDialog = wrapper.get('[data-edit-group-dialog]')
    expect(editDialog.text()).toContain('编辑小组')
    expect(editDialog.find('[aria-label="新建类型"]').exists()).toBe(false)
  })

  it('成员编辑结束后新建小组仍显示小组字段', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/people', component: PeopleView }],
    })
    await router.push('/people?tab=members&edit=member-1')
    await router.isReady()
    const wrapper = mount(PeopleView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()
    expect(wrapper.get('[data-edit-member-dialog]').text()).toContain('备注')

    await router.push('/people?tab=groups')
    await flushPromises()
    await wrapper
      .findAll('button')
      .find((item) => item.text() === '新建')!
      .trigger('click')
    await flushPromises()

    const createDialog = wrapper.get('[role="dialog"]')
    expect(createDialog.text()).toContain('默认开启访客页')
    expect(createDialog.text()).not.toContain('备注')
  })
})
