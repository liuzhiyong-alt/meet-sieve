// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const bindings = vi.hoisted(() => ({
  GetDeletionJob: vi.fn(),
  GetAgentRecoveryCommands: vi.fn(),
  GetMeetingDetail: vi.fn(),
  ListMeetingContent: vi.fn(),
  ListTranscript: vi.fn(),
  GetMinutesState: vi.fn(),
}))

vi.mock('../../../wailsjs/go/wails/QueryBinding', () => ({
  GetHome: vi.fn(),
  GetInterruptedRecovery: vi.fn(),
  GetMeetingDetail: bindings.GetMeetingDetail,
  ListMeetingContent: bindings.ListMeetingContent,
  ListMeetings: vi.fn(),
  ListTranscript: bindings.ListTranscript,
}))
vi.mock('../../../wailsjs/go/wails/DeletionBinding', () => ({
  DeleteMeeting: vi.fn(),
  GetDeletionJob: bindings.GetDeletionJob,
  PreviewMeetingDeletion: vi.fn(),
  RetryDeletion: vi.fn(),
}))
vi.mock('../../../wailsjs/go/wails/ResourceBinding', () => ({
  OpenExternalLink: vi.fn(),
  OpenResource: vi.fn(),
  RevealResource: vi.fn(),
}))
vi.mock('../../../wailsjs/go/wails/MinutesBinding', () => ({
  GenerateMinutes: vi.fn(),
  GetMinutesSettings: vi.fn(),
  GetMinutesState: bindings.GetMinutesState,
  SaveMinuteDraft: vi.fn(),
  SaveMinutesSettings: vi.fn(),
  StopMinutesGeneration: vi.fn(),
}))
vi.mock('../../../wailsjs/go/wails/AgentBinding', () => ({
  GetAgentRecoveryCommands: bindings.GetAgentRecoveryCommands,
}))

import MeetingDetailView from './MeetingDetailView.vue'
import { useMinutesStore } from '../../stores/minutes'

/** mountDetail 使用图 3 的状态组合挂载真实详情路由。 */
async function mountDetail() {
  const pinia = createPinia()
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/meetings', component: { template: '<p>meetings</p>' } },
      {
        path: '/meetings/:id',
        component: MeetingDetailView,
        props: true,
      },
      {
        path: '/meetings/:id/minutes',
        component: { template: '<p>minutes</p>' },
      },
      {
        path: '/meetings/:id/transcript',
        name: 'meeting-transcript',
        component: { template: '<p>transcript</p>' },
      },
    ],
  })
  await router.push('/meetings/meeting-1')
  await router.isReady()
  const wrapper = mount(MeetingDetailView, {
    props: { id: 'meeting-1' },
    global: { plugins: [pinia, router] },
  })
  await flushPromises()
  return { pinia, router, wrapper }
}

describe('MeetingDetailView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    document.body.innerHTML = '<div id="meeting-titlebar-actions"></div>'
    bindings.GetMeetingDetail.mockResolvedValue({
      code: 200,
      message: 'ok',
      data: {
        summary: {
          id: 'meeting-1',
          meeting_no: '20260804-HAQR-01',
          subject: '测试会议 1',
          participants: ['张三'],
          highest_status: 'saved',
          local_save_state: 'saved',
          realtime_asr_state: 'stopped',
          gap_state: 'none',
          agent_state: 'unavailable',
          minute_state: 'failed',
          lan_state: 'stopped',
        },
        can_delete_meeting: true,
      },
    })
    bindings.GetDeletionJob.mockResolvedValue({
      code: 404,
      message: '无删除任务',
    })
    bindings.ListTranscript.mockResolvedValue({
      code: 200,
      message: 'ok',
      data: {
        items: [],
        before_seq: 0,
        after_seq: 0,
        has_more: false,
        has_previous: false,
        has_next: false,
      },
    })
    bindings.ListMeetingContent.mockResolvedValue({
      code: 200,
      message: 'ok',
      data: {
        items: [],
        before_seq: 0,
        after_seq: 0,
        has_more: false,
        has_previous: false,
        has_next: false,
      },
    })
    bindings.GetMinutesState.mockResolvedValue({
      code: 200,
      message: 'ok',
      data: {
        meeting_id: 'meeting-1',
        state: 'not_generated',
        runtime_state: 'idle',
        projection_state: 'idle',
        revision: 1,
      },
    })
    bindings.GetAgentRecoveryCommands.mockResolvedValue({
      code: 200,
      message: 'ok',
      data: {
        thread_available: false,
        thread_command: '',
        directory_command: 'codex -C /tmp/meeting',
        recovery_prompt: '请读取会议原始记录.md',
      },
    })
  })

  it('不再展示无操作价值的页头状态和概览状态网格', async () => {
    const { wrapper } = await mountDetail()

    expect(wrapper.findAll('.ms-page-head .ms-status-pill')).toHaveLength(0)
    expect(wrapper.find('.ms-fact-grid').exists()).toBe(false)
  })

  it('会议详情不再提供重复的整场删除入口', async () => {
    const { wrapper } = await mountDetail()

    expect(wrapper.find('.ms-danger-zone').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('危险操作')
    expect(wrapper.text()).not.toContain('永久删除会议')
  })

  it('三个页签共用统一内容面板和头部层级', async () => {
    const { router, wrapper } = await mountDetail()

    const cases = [
      ['transcript', '原始记录', '按会议时间顺序'],
      ['minutes', '会议纪要', 'Markdown 预览'],
      ['messages', '消息与资料', '会议消息、AI 回答、链接与附件'],
    ]
    for (const [tab, title, description] of cases) {
      await router.push({
        path: '/meetings/meeting-1',
        query: tab === 'transcript' ? {} : { tab },
      })
      await flushPromises()
      const panel = wrapper.get('section.ms-detail-panel')
      expect(panel.classes()).toContain('ms-card')
      expect(panel.classes()).not.toContain('ms-settings-card')
      expect(panel.get('.ms-detail-panel__head h2').text()).toBe(title)
      expect(panel.get('.ms-detail-panel__head p').text()).toBe(description)
    }
  })

  it('会议详情页签按原始记录、会议纪要、消息与资料排列并保持查询参数行为', async () => {
    const { router, wrapper } = await mountDetail()

    expect(
      wrapper
        .findAll('nav[aria-label="会议详情页签"] button')
        .map((item) => item.text()),
    ).toEqual(['原始记录', '会议纪要', '消息与资料'])

    await wrapper
      .findAll('nav[aria-label="会议详情页签"] button')[0]
      ?.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({})
    expect(
      wrapper.get('nav[aria-label="会议详情页签"] .is-current').text(),
    ).toBe('原始记录')
    expect(bindings.ListTranscript).toHaveBeenCalledWith({
      meeting_id: 'meeting-1',
      after_seq: 0,
      before_seq: 0,
      limit: 200,
    })
  })

  it('在会议页头提供 Codex 接续入口', async () => {
    const { wrapper } = await mountDetail()

    expect(
      document.querySelector('#meeting-titlebar-actions button'),
    ).toBeNull()
    expect(
      wrapper.findAll('.ms-page-head__actions .ms-status-pill'),
    ).toHaveLength(0)
    const action = wrapper.get('.ms-page-head__actions button')
    expect(action.text()).toBe('用 Codex 继续')
    await action.trigger('click')
    await flushPromises()

    expect(bindings.GetAgentRecoveryCommands).toHaveBeenCalledWith('meeting-1')
    expect(document.body.textContent).toContain('从会议文件继续')
  })

  it('仅在原始记录内容区提供编辑入口', async () => {
    const { router, wrapper } = await mountDetail()

    expect(wrapper.text()).toContain('编辑原始记录')
    await router.push({
      path: '/meetings/meeting-1',
      query: { tab: 'minutes' },
    })
    await flushPromises()

    expect(wrapper.text()).not.toContain('编辑原始记录')
  })

  it('会议纪要正文可感知时不额外展示已生成状态', async () => {
    const { pinia, router, wrapper } = await mountDetail()
    useMinutesStore(pinia).notice = '会议纪要已生成'

    await router.push({
      path: '/meetings/meeting-1',
      query: { tab: 'minutes' },
    })
    await flushPromises()

    expect(wrapper.text()).not.toContain('会议纪要已生成')
  })

  it('没有纪要时展示生成按钮，有纪要时直接渲染 Markdown 并提供编辑入口', async () => {
    const { router, wrapper } = await mountDetail()
    await router.push({
      path: '/meetings/meeting-1',
      query: { tab: 'minutes' },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('生成会议纪要')
    expect(wrapper.text()).not.toContain('打开纪要')

    bindings.GetMinutesState.mockResolvedValue({
      code: 200,
      message: 'ok',
      data: {
        meeting_id: 'meeting-1',
        state: 'draft',
        current: {
          id: 'minute-1',
          version_no: 1,
          source: 'ai',
          content_markdown: '# 决策\n\n- 下周发布',
          state: 'draft',
          is_current: true,
          created_at: 1,
        },
        runtime_state: 'idle',
        projection_state: 'current',
        revision: 2,
      },
    })
    await router.push('/meetings/meeting-1')
    await router.push({
      path: '/meetings/meeting-1',
      query: { tab: 'minutes' },
    })
    await flushPromises()

    expect(
      wrapper.get('.ms-markdown [role="heading"][aria-level="2"]').text(),
    ).toBe('决策')
    expect(wrapper.get('.ms-detail-panel__head h2').text()).toBe('会议纪要')
    expect(wrapper.get('.ms-detail-panel__head p').text()).toBe('Markdown 预览')
    expect(wrapper.get('.ms-markdown li').text()).toBe('下周发布')
    const editAction = wrapper.get('a.ms-button')
    expect(editAction.text()).toBe('编辑')
    expect(editAction.classes()).toContain('ms-button--quiet')
  })

  it('原始记录显示安全说话人名称并按真实方向翻页', async () => {
    bindings.ListTranscript.mockResolvedValue({
      code: 200,
      message: 'ok',
      data: {
        items: [
          {
            seq: 1,
            kind: 'utterance.final',
            occurred_at: 1000,
            speaker_display: '未知说话人 2',
            text: '第一段',
          },
          {
            seq: 2,
            kind: 'utterance.final',
            occurred_at: 2000,
            speaker_display: '未识别说话人',
            text: '第二段',
          },
        ],
        before_seq: 1,
        after_seq: 2,
        has_more: true,
        has_previous: false,
        has_next: true,
      },
    })
    const { router, wrapper } = await mountDetail()

    await router.push({
      path: '/meetings/meeting-1',
      query: { tab: 'transcript' },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('未知说话人 2')
    expect(wrapper.text()).toContain('未识别说话人')
    expect(wrapper.text()).not.toContain('utterance.final')
    expect(wrapper.get('.ms-detail-panel__head h2').text()).toBe('原始记录')
    expect(wrapper.get('.ms-transcript-list').classes()).toContain(
      'ms-detail-list',
    )
    expect(wrapper.get('.ms-transcript-list').classes()).not.toContain(
      'ms-people-list',
    )
    expect(wrapper.get('.ms-detail-panel__head a').classes()).toContain(
      'ms-button--quiet',
    )
    expect(wrapper.find('.ms-transcript-list .ms-list-item > p').exists()).toBe(
      false,
    )
    const pagination = wrapper.get('nav[aria-label="列表分页"]')
    const previous = pagination.get('button:first-child')
    const next = pagination.get('button:last-child')
    expect(pagination.classes()).toContain('ms-cursor-pagination')
    expect(pagination.get('.ms-meta').text()).toBe('第 1 页 · 当前 2 条')
    expect(previous.attributes('disabled')).toBeDefined()
    expect(next.attributes('disabled')).toBeUndefined()

    await next.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toMatchObject({
      tab: 'transcript',
      after: '2',
      page: '2',
    })
  })

  it('消息与资料把公开 AI 和附件映射为中文业务标签', async () => {
    bindings.ListMeetingContent.mockResolvedValue({
      code: 200,
      message: 'ok',
      data: {
        items: [
          {
            seq: 3,
            kind: 'ai.answer',
            occurred_at: 3000,
            entity_id: 'answer-1',
            text: '这是公开回答',
          },
          {
            seq: 4,
            kind: 'resource.created',
            occurred_at: 4000,
            entity_id: 'resource-1',
            resource_kind: 'attachment',
            resource_name: '验收清单.docx',
            resource_state: 'verified',
          },
        ],
        before_seq: 3,
        after_seq: 4,
        has_more: false,
        has_previous: false,
        has_next: false,
      },
    })
    const { router, wrapper } = await mountDetail()

    await router.push({
      path: '/meetings/meeting-1',
      query: { tab: 'messages' },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('AI 回答')
    expect(wrapper.text()).toContain('验收清单.docx')
    expect(wrapper.text()).toContain('完整性已验证')
    expect(wrapper.text()).not.toContain('ai.answer')
    expect(wrapper.text()).not.toContain('resource.created')
    const pagination = wrapper.get('nav[aria-label="列表分页"]')
    expect(pagination.classes()).toContain('ms-cursor-pagination')
    expect(pagination.get('.ms-meta').text()).toBe('第 1 页 · 当前 2 条')
  })
})
