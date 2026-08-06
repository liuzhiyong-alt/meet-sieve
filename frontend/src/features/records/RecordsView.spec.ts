// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../../../wailsjs/go/wails/QueryBinding', () => ({
  GetHome: vi.fn(),
  GetInterruptedRecovery: vi.fn(),
  GetMeetingDetail: vi.fn(),
  ListMeetingContent: vi.fn(),
  ListMeetings: vi.fn(),
  ListTranscript: vi.fn(),
}))
vi.mock('../../../wailsjs/go/wails/DeletionBinding', () => ({
  DeleteMeeting: vi.fn(),
  GetDeletionJob: vi.fn(),
  PreviewMeetingDeletion: vi.fn(),
  RetryDeletion: vi.fn(),
}))

import { ListMeetings } from '../../../wailsjs/go/wails/QueryBinding'
import { wails } from '../../../wailsjs/go/models'
import RecordsView from './RecordsView.vue'

/** makeMeeting 构造不含假业务成功语义的组件契约夹具。 */
function makeMeeting(
  primaryAction: wails.MeetingPrimaryActionDTO,
  index = 1,
): wails.MeetingSummaryDTO {
  return new wails.MeetingSummaryDTO({
    id: `11111111-1111-4111-8111-${String(index).padStart(12, '0')}`,
    meeting_no: `M-${String(index).padStart(2, '0')}`,
    subject: `缺口会议 ${index}`,
    started_at: 1000 + index,
    lifecycle_state: 'ended',
    local_save_state: 'saved',
    realtime_asr_state: 'stopped',
    gap_state: 'conflict',
    agent_state: 'available',
    minute_state: 'not_generated',
    lan_state: 'stopped',
    participants: ['刘毅'],
    participant_member_ids: [],
    highest_status: 'gap_conflict',
    primary_action: primaryAction,
    can_delete_meeting: true,
  })
}

describe('RecordsView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(ListMeetings).mockResolvedValue(
      new wails.Result_meet_sieve_internal_transport_wails_MeetingPageDTO_({
        code: 200,
        message: 'ok',
        requestId: 'test',
        data: new wails.MeetingPageDTO({
          items: [],
          next_cursor: '',
          previous_cursor: '',
        }),
      }),
    )
  })

  it('筛选只在提交时查询并清除旧 cursor', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/meetings', component: RecordsView },
        { path: '/meetings/new', component: { template: '<p>new</p>' } },
      ],
    })
    await router.push('/meetings?cursor=old')
    await router.isReady()
    const wrapper = mount(RecordsView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()
    vi.mocked(ListMeetings).mockClear()

    await wrapper.get('select').setValue('saved')
    await wrapper.get('input').setValue(' 产品 ')
    await flushPromises()
    expect(ListMeetings).not.toHaveBeenCalled()

    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({
      search: '产品',
      status: 'saved',
    })
    expect(ListMeetings).toHaveBeenCalledTimes(1)
  })

  it('筛选栏使用两个专用字段和独立查询动作', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/meetings', component: RecordsView },
        { path: '/meetings/new', component: { template: '<p>new</p>' } },
      ],
    })
    await router.push('/meetings')
    await router.isReady()
    const wrapper = mount(RecordsView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(wrapper.findAll('.ms-records-filter-field')).toHaveLength(2)
    expect(wrapper.get('.ms-records-filter-submit').text()).toBe('查询')
  })

  it('每条记录固定展示打开和删除，不暴露后端状态动作', async () => {
    vi.mocked(ListMeetings).mockResolvedValue(
      new wails.Result_meet_sieve_internal_transport_wails_MeetingPageDTO_({
        code: 200,
        message: 'ok',
        requestId: 'test',
        data: new wails.MeetingPageDTO({
          items: [
            makeMeeting(
              new wails.MeetingPrimaryActionDTO({
                kind: 'resolve_gap',
                label: '处理缺口',
                target_id: '22222222-2222-4222-8222-222222222222',
                enabled: true,
              }),
            ),
          ],
          next_cursor: '',
          previous_cursor: '',
        }),
      }),
    )
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/meetings', component: RecordsView },
        { path: '/meetings/new', component: { template: '<p>new</p>' } },
        { path: '/meetings/:id', component: { template: '<p>detail</p>' } },
      ],
    })
    await router.push('/meetings')
    await router.isReady()
    const wrapper = mount(RecordsView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const actions = wrapper.findAll('.ms-record-actions button')
    expect(actions.map((item) => item.text())).toEqual(['打开', '删除'])
    expect(wrapper.text()).not.toContain('处理缺口')

    await actions[0]?.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe(
      '/meetings/11111111-1111-4111-8111-000000000001',
    )
  })

  it('十行记录在固定视口内滚动且分页保持在视口外', async () => {
    const action = new wails.MeetingPrimaryActionDTO({
      kind: 'open_meeting',
      label: '打开',
      enabled: true,
    })
    vi.mocked(ListMeetings).mockResolvedValue(
      new wails.Result_meet_sieve_internal_transport_wails_MeetingPageDTO_({
        code: 200,
        message: 'ok',
        requestId: 'test',
        data: new wails.MeetingPageDTO({
          items: Array.from({ length: 10 }, (_, index) =>
            makeMeeting(action, index + 1),
          ),
          next_cursor: 'next',
          previous_cursor: '',
        }),
      }),
    )
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/meetings', component: RecordsView },
        { path: '/meetings/new', component: { template: '<p>new</p>' } },
      ],
    })
    await router.push('/meetings')
    await router.isReady()
    const wrapper = mount(RecordsView, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    const viewport = wrapper.get('.ms-records-viewport')
    const list = viewport.get('.ms-records-list')
    const pager = wrapper.get('.ms-records-pagination')
    expect(list.findAll('.ms-record-row')).toHaveLength(10)
    expect(list.attributes('tabindex')).toBe('0')
    expect(viewport.element.parentElement).toBe(pager.element.parentElement)
    expect(list.element.parentElement).not.toBe(pager.element.parentElement)
    expect(
      viewport.element.compareDocumentPosition(pager.element) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    expect(list.findAll('.ms-record-status')).toHaveLength(10)
    expect(list.findAll('.ms-record-actions')).toHaveLength(10)
    expect(list.findAll('.ms-record-actions button')).toHaveLength(20)
  })
})
