// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const stores = vi.hoisted(() => ({
  meeting: {
    current: null as null | {
      id: string
      subject: string
      lifecycle_state: string
      local_save_state: string
      realtime_asr_state: string
      agent_state: string
      started_at: number
      ended_at: number
      updated_at: number
      meeting_no: string
    },
    errorMessage: '',
    saving: false,
    startNewMeeting: vi.fn(),
    endMeeting: vi.fn(),
    retryRecovery: vi.fn(),
    refreshCurrentMeeting: vi.fn(),
  },
  asr: {
    realtimeState: 'stopped',
    rawRecordState: 'ready',
    retrying: false,
    restoreTimeline: vi.fn(),
    applyPartial: vi.fn(),
    applyRealtimeState: vi.fn(),
    retryRealtime: vi.fn(),
  },
  lan: {
    status: { state: 'stopped', active_uploads: [] },
    loadInterfaces: vi.fn(),
    refreshStatus: vi.fn(),
    cancelUpload: vi.fn(),
  },
  agent: {
    runtime: { state: 'unchecked', partial: '', approval: undefined },
    wakeCommand: { state: 'idle', error_code: '' },
    refreshState: vi.fn(),
    restoreTimeline: vi.fn(),
    applyEvent: vi.fn(),
    applyWakeCommandEvent: vi.fn(),
    ask: vi.fn(),
    interrupt: vi.fn(),
  },
  gap: {
    gaps: [],
    state: 'none',
    submitting: false,
    conflictGap: null,
    refresh: vi.fn(),
    applyEvent: vi.fn(() => false),
    retryFailed: vi.fn(),
    stop: vi.fn(),
  },
  speaker: {
    meetingID: '',
    state: 'ready',
    refresh: vi.fn(),
  },
  timeline: {
    meetingID: '',
    entries: [],
    status: {
      microphone_state: 'stopped',
      latest_final_at: 0,
      recording_state: 'ended',
      local_save_state: 'saved',
      realtime_asr_state: 'stopped',
      agent_state: 'unchecked',
      lan_state: 'stopped',
      online_count: 0,
    },
    loadLatest: vi.fn(),
    refreshStatus: vi.fn(),
    recoverAfter: vi.fn(),
    applyPartial: vi.fn(),
    applyAttachmentState: vi.fn(),
  },
}))

vi.mock('../../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn(() => () => undefined),
}))
vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn() } }))
vi.mock('../../stores/meeting', () => ({
  useMeetingStore: () => stores.meeting,
}))
vi.mock('../../stores/asr', () => ({ useASRStore: () => stores.asr }))
vi.mock('../../stores/lan', () => ({ useLANStore: () => stores.lan }))
vi.mock('../../stores/agent', () => ({ useAgentStore: () => stores.agent }))
vi.mock('../../stores/gap', () => ({ useGapStore: () => stores.gap }))
vi.mock('../../stores/speaker', () => ({
  useSpeakerStore: () => stores.speaker,
}))
vi.mock('../../stores/timeline', () => ({
  useTimelineStore: () => stores.timeline,
}))

import LiveMeetingView from './LiveMeetingView.vue'

describe('LiveMeetingView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    stores.meeting.current = {
      id: 'meeting-ended',
      subject: '已结束会议',
      lifecycle_state: 'ended',
      local_save_state: 'saved',
      realtime_asr_state: 'stopped',
      agent_state: 'unchecked',
      started_at: 1,
      ended_at: 2,
      updated_at: 2,
      meeting_no: '20260807-ABCD-01',
    }
    stores.meeting.errorMessage = ''
    stores.meeting.saving = false
    stores.meeting.startNewMeeting.mockImplementation(() => {
      stores.meeting.current = null
    })
    stores.lan.status = { state: 'stopped', active_uploads: [] }
    stores.timeline.entries = []
  })

  it('已结束会议点击开始新会议后进入会前创建页', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/meetings/live', component: LiveMeetingView },
        { path: '/meetings/new', component: { template: '<p>创建会议</p>' } },
      ],
    })
    await router.push('/meetings/live')
    await router.isReady()

    const wrapper = mount(LiveMeetingView, {
      global: {
        plugins: [router],
        stubs: {
          FinalizingView: true,
          MeetingTimelinePanel: {
            template: '<section><slot name="followup" /></section>',
          },
        },
      },
    })

    await flushPromises()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === '开始新会议')!
      .trigger('click')
    await flushPromises()

    expect(stores.meeting.startNewMeeting).toHaveBeenCalledOnce()
    expect(router.currentRoute.value.path).toBe('/meetings/new')
    wrapper.unmount()
  })
})
