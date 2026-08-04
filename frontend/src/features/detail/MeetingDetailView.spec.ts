// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const bindings = vi.hoisted(() => ({
  GetDeletionJob: vi.fn(),
  GetMeetingDetail: vi.fn(),
  ListMeetingContent: vi.fn(),
  ListTranscript: vi.fn(),
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
  DeleteRecording: vi.fn(),
  GetDeletionJob: bindings.GetDeletionJob,
  PreviewMeetingDeletion: vi.fn(),
  PreviewRecordingDeletion: vi.fn(),
  RetryDeletion: vi.fn(),
}))
vi.mock('../../../wailsjs/go/wails/ResourceBinding', () => ({
  OpenExternalLink: vi.fn(),
  OpenResource: vi.fn(),
  RevealResource: vi.fn(),
}))

import MeetingDetailView from './MeetingDetailView.vue'

/** mountDetail 使用图 3 的状态组合挂载真实详情路由。 */
async function mountDetail() {
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
    ],
  })
  await router.push('/meetings/meeting-1')
  await router.isReady()
  const wrapper = mount(MeetingDetailView, {
    props: { id: 'meeting-1' },
    global: { plugins: [createPinia(), router] },
  })
  await flushPromises()
  return { router, wrapper }
}

describe('MeetingDetailView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
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
        can_delete_recording: true,
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
      data: { items: [], before_seq: 0, after_seq: 0, has_more: false },
    })
    bindings.ListMeetingContent.mockResolvedValue({
      code: 200,
      message: 'ok',
      data: { items: [], before_seq: 0, after_seq: 0, has_more: false },
    })
  })

  it('把顶部和概览状态统一映射为中文', async () => {
    const { wrapper } = await mountDetail()

    expect(wrapper.get('.ms-status-pill').text()).toBe('本地已保存')
    expect(
      wrapper.findAll('.ms-fact-grid dd').map((item) => item.text()),
    ).toEqual([
      '本地已保存',
      '已停止',
      '无缺口',
      '暂不可用',
      '生成失败',
      '已停止',
    ])
  })

  it('四个页签的内容容器共用同一间距类', async () => {
    const { router, wrapper } = await mountDetail()

    for (const tab of ['overview', 'transcript', 'messages', 'minutes']) {
      await router.push({
        path: '/meetings/meeting-1',
        query: tab === 'overview' ? {} : { tab },
      })
      await flushPromises()
      expect(wrapper.findAll('section.ms-detail-panel')).toHaveLength(1)
    }
  })
})
