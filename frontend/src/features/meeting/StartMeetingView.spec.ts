// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const meetingBindings = vi.hoisted(() => ({
  GetCreateDraft: vi.fn(),
  StartMeeting: vi.fn(),
}))
const peopleBindings = vi.hoisted(() => ({ GetMeetingPeopleOptions: vi.fn() }))
const voiceBindings = vi.hoisted(() => ({ ListInputDevices: vi.fn() }))
const asrBindings = vi.hoisted(() => ({ GetASRSettings: vi.fn() }))
const lanBindings = vi.hoisted(() => ({ ListLANInterfaces: vi.fn() }))

vi.mock('../../../wailsjs/go/wails/MeetingBinding', () => meetingBindings)
vi.mock('../../../wailsjs/go/wails/PeopleBinding', () => peopleBindings)
vi.mock('../../../wailsjs/go/wails/VoiceBinding', () => voiceBindings)
vi.mock('../../../wailsjs/go/wails/ASRBinding', () => asrBindings)
vi.mock('../../../wailsjs/go/wails/LANBinding', () => lanBindings)

import StartMeetingView from './StartMeetingView.vue'

describe('StartMeetingView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    meetingBindings.GetCreateDraft.mockResolvedValue({
      code: 200,
      data: {
        suggested_meeting_no: '20260803-A7K2-03',
        default_subject: '未命名会议',
      },
    })
    peopleBindings.GetMeetingPeopleOptions.mockResolvedValue({
      code: 200,
      data: {
        groups: [
          {
            id: 'group-1',
            name: '产品周会',
            default_lan_enabled: false,
            members: [
              { id: 'member-active', name: '刘毅', voice_readiness: 'ready' },
            ],
          },
        ],
        members: [
          { id: 'member-active', name: '刘毅', voice_readiness: 'ready' },
          { id: 'member-other', name: '陈然', voice_readiness: 'ready' },
        ],
      },
    })
    voiceBindings.ListInputDevices.mockResolvedValue({
      code: 200,
      data: [{ id: 'mic-1', name: 'MacBook 麦克风', is_default: true }],
    })
    asrBindings.GetASRSettings.mockResolvedValue({
      code: 200,
      data: {
        api_key_configured: false,
        api_key_mask: '',
        requires_api_key_upgrade: false,
      },
    })
    lanBindings.ListLANInterfaces.mockResolvedValue({
      code: 200,
      data: { interfaces: [], recommended_id: '', reason: 'not_found' },
    })
  })

  it('按入口小组预填当前仍存在的成员并保留新草稿会议号', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/meetings/new', component: StartMeetingView }],
    })
    await router.push('/meetings/new?group=group-1')
    await router.isReady()

    const wrapper = mount(StartMeetingView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.get('select').element.value).toBe('group-1')
    expect(
      wrapper
        .findAll<HTMLInputElement>('input[type="checkbox"]')
        .filter((item) => item.element.checked)
        .map((item) => item.element.value),
    ).toEqual(['member-active'])
    await wrapper
      .findAll('button')
      .find((item) => item.text() === '展开高级设置')!
      .trigger('click')
    const advancedPanel = wrapper.get('.ms-advanced-panel')
    expect(
      advancedPanel.get('.ms-advanced-grid').findAll(':scope > *'),
    ).toHaveLength(4)
    expect(advancedPanel.findAll('.ms-advanced-field')).toHaveLength(3)
    expect(wrapper.get<HTMLInputElement>('.ms-input--mono').element.value).toBe(
      '20260803-A7K2-03',
    )
  })

  it('重新进入开始会议页时显示新增的活动成员', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/meetings/new', component: StartMeetingView }],
    })
    await router.push('/meetings/new')
    await router.isReady()
    const pinia = createPinia()
    const firstWrapper = mount(StartMeetingView, {
      global: { plugins: [pinia, router] },
    })
    await flushPromises()

    expect(firstWrapper.text()).not.toContain('测试成员')
    firstWrapper.unmount()
    peopleBindings.GetMeetingPeopleOptions.mockResolvedValue({
      code: 200,
      data: {
        groups: [],
        members: [
          {
            id: 'member-new',
            name: '测试成员',
            voice_readiness: 'unavailable',
          },
          { id: 'member-active', name: '刘毅', voice_readiness: 'ready' },
          { id: 'member-other', name: '陈然', voice_readiness: 'ready' },
        ],
      },
    })

    const secondWrapper = mount(StartMeetingView, {
      global: { plugins: [pinia, router] },
    })
    await flushPromises()

    expect(peopleBindings.GetMeetingPeopleOptions).toHaveBeenCalledTimes(2)
    expect(secondWrapper.text()).toContain('测试成员')
  })

  it('使用卡片标题区弹窗维护临时成员并在关闭后恢复焦点', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/meetings/new', component: StartMeetingView }],
    })
    await router.push('/meetings/new')
    await router.isReady()
    const wrapper = mount(StartMeetingView, {
      attachTo: document.body,
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const opener = wrapper.get('.ms-card-head button')
    await opener.trigger('click')
    const dialog = wrapper.get('[role="dialog"]')
    const input = dialog.get<HTMLInputElement>('input')
    expect(document.activeElement).toBe(input.element)
    expect(wrapper.get('.ms-meeting-split').attributes()).toHaveProperty(
      'inert',
    )

    await input.setValue(' 临时访客 ')
    await input.trigger('keydown.enter')
    expect(dialog.text()).toContain('临时访客')
    await dialog.get('[aria-label="移除 临时访客"]').trigger('click')
    expect(dialog.text()).not.toContain('临时访客')

    await dialog.trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper.get('.ms-meeting-split').attributes()).not.toHaveProperty(
      'inert',
    )
    expect(document.activeElement).toBe(opener.element)
    wrapper.unmount()
  })

  it('局域网访客页暂未开放时固定关闭且提交关闭状态', async () => {
    peopleBindings.GetMeetingPeopleOptions.mockResolvedValue({
      code: 200,
      data: {
        groups: [
          {
            id: 'group-lan',
            name: '访客评审',
            default_lan_enabled: true,
            members: [
              { id: 'member-active', name: '刘毅', voice_readiness: 'ready' },
            ],
          },
        ],
        members: [
          { id: 'member-active', name: '刘毅', voice_readiness: 'ready' },
        ],
      },
    })
    lanBindings.ListLANInterfaces.mockResolvedValue({
      code: 200,
      data: { interfaces: [], recommended_id: '', reason: 'not_found' },
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/meetings/new', component: StartMeetingView }],
    })
    await router.push('/meetings/new?group=group-lan')
    await router.isReady()
    const wrapper = mount(StartMeetingView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    await wrapper
      .findAll('button')
      .find((item) => item.text() === '展开高级设置')!
      .trigger('click')
    expect(wrapper.text()).not.toContain('使用的网络')
    expect(wrapper.text()).toContain(
      '开启后，同一局域网内的设备可以访问访客页。仅在可信的私有网络使用（暂未开放）。',
    )
    const lanSwitch = wrapper.get<HTMLInputElement>('[role="switch"]')
    expect(lanSwitch.attributes('aria-label')).toBe('允许同一私有网络访问')
    expect(lanSwitch.attributes('aria-describedby')).toBe('lan-create-help')
    expect(lanSwitch.element.disabled).toBe(true)
    expect(lanSwitch.element.checked).toBe(false)
    const switchTrack = wrapper.get('.ms-switch-track')
    expect(switchTrack.attributes('aria-hidden')).toBe('true')
    expect(lanSwitch.element.nextElementSibling).toBe(switchTrack.element)
    expect(
      wrapper.get<HTMLButtonElement>('.ms-start-button').element.disabled,
    ).toBe(false)

    await lanSwitch.trigger('click')
    await lanSwitch.trigger('keydown', { key: ' ' })
    expect(lanSwitch.element.checked).toBe(false)

    meetingBindings.StartMeeting.mockResolvedValue({
      code: 200,
      data: { id: 'meeting-1', lifecycle_state: 'recording' },
    })
    await wrapper.get('.ms-start-button').trigger('click')
    await flushPromises()
    expect(meetingBindings.StartMeeting).toHaveBeenCalledWith(
      expect.objectContaining({ lan_enabled: false, lan_interface_id: '' }),
    )
  })

  it('实时 ASR 启动失败时由用户选择用同一草稿仅录音重试', async () => {
    asrBindings.GetASRSettings.mockResolvedValue({
      code: 200,
      data: {
        api_key_configured: true,
        api_key_mask: '••••5678',
        requires_api_key_upgrade: false,
      },
    })
    meetingBindings.StartMeeting.mockResolvedValueOnce({
      code: 403,
      errorCode: 'ASR_AUTH_FAILED',
      message: '实时转写凭据无效或服务未开通',
    }).mockResolvedValueOnce({
      code: 200,
      data: { id: 'meeting-1', lifecycle_state: 'recording' },
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/meetings/new', component: StartMeetingView }],
    })
    await router.push('/meetings/new?group=group-1')
    await router.isReady()
    const wrapper = mount(StartMeetingView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    await wrapper.get('.ms-start-button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('仅录音继续')
    await wrapper
      .findAll('button')
      .find((item) => item.text() === '仅录音继续')!
      .trigger('click')
    await flushPromises()

    expect(meetingBindings.StartMeeting).toHaveBeenCalledTimes(2)
    const first = meetingBindings.StartMeeting.mock.calls[0]?.[0]
    const second = meetingBindings.StartMeeting.mock.calls[1]?.[0]
    expect(first).toMatchObject({ asr_mode: 'realtime' })
    expect(second).toMatchObject({
      meeting_no: first.meeting_no,
      suggested_meeting_no: first.suggested_meeting_no,
      subject: first.subject,
      member_ids: first.member_ids,
      temporary_participant_names: first.temporary_participant_names,
      microphone_id: first.microphone_id,
      asr_mode: 'record_only',
    })
  })

  it('非 ASR 启动错误不显示仅录音降级入口', async () => {
    meetingBindings.StartMeeting.mockResolvedValue({
      code: 500,
      errorCode: 'WORKSPACE_UNAVAILABLE',
      message: '工作目录不可用',
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/meetings/new', component: StartMeetingView }],
    })
    await router.push('/meetings/new?group=group-1')
    await router.isReady()
    const wrapper = mount(StartMeetingView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    await wrapper.get('.ms-start-button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('仅录音继续')
  })
})
