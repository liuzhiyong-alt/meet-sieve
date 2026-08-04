import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useMeetingStore } from './meeting'

const meetingBindings = vi.hoisted(() => ({
  GetActiveMeeting: vi.fn(),
  GetLatestInterruptedMeeting: vi.fn(),
  GetCreateDraft: vi.fn(),
  StartMeeting: vi.fn(),
  EndMeeting: vi.fn(),
  RetryMeetingRecovery: vi.fn(),
}))
const peopleBindings = vi.hoisted(() => ({ GetMeetingPeopleOptions: vi.fn() }))
const voiceBindings = vi.hoisted(() => ({ ListInputDevices: vi.fn() }))

vi.mock('../../wailsjs/go/wails/MeetingBinding', () => meetingBindings)
vi.mock('../../wailsjs/go/wails/PeopleBinding', () => peopleBindings)
vi.mock('../../wailsjs/go/wails/VoiceBinding', () => voiceBindings)

describe('meeting store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('restores active meeting from backend after page reload', async () => {
    meetingBindings.GetActiveMeeting.mockResolvedValue({
      code: 200,
      data: {
        active: true,
        meeting: {
          id: 'meeting-1',
          lifecycle_state: 'recording',
          local_save_state: 'saving',
        },
      },
    })

    const store = useMeetingStore()
    await store.refreshCurrentMeeting()

    expect(store.current?.id).toBe('meeting-1')
    expect(store.screen).toBe('live')
  })

  it('loads draft, people and real microphones together', async () => {
    meetingBindings.GetCreateDraft.mockResolvedValue({
      code: 200,
      data: {
        suggested_meeting_no: '20260801-ABCD-01',
        default_subject: '未命名会议',
      },
    })
    peopleBindings.GetMeetingPeopleOptions.mockResolvedValue({
      code: 200,
      data: { groups: [], members: [{ id: 'member-1', name: '张三' }] },
    })
    voiceBindings.ListInputDevices.mockResolvedValue({
      code: 200,
      data: [{ id: 'device-1', name: 'MacBook 麦克风', is_default: true }],
    })

    const store = useMeetingStore()
    await store.loadCreateScreen()

    expect(store.draft.meetingNo).toBe('20260801-ABCD-01')
    expect(store.members).toHaveLength(1)
    expect(store.microphones[0]?.id).toBe('device-1')
  })

  it('reloads the create screen projection with the latest member options', async () => {
    meetingBindings.GetCreateDraft.mockResolvedValue({
      code: 200,
      data: {
        suggested_meeting_no: '20260801-ABCD-02',
        default_subject: '未命名会议',
      },
    })
    peopleBindings.GetMeetingPeopleOptions.mockResolvedValueOnce({
      code: 200,
      data: { groups: [], members: [{ id: 'member-1', name: '张三' }] },
    }).mockResolvedValueOnce({
      code: 200,
      data: {
        groups: [],
        members: [
          { id: 'member-2', name: '测试成员' },
          { id: 'member-1', name: '张三' },
        ],
      },
    })
    voiceBindings.ListInputDevices.mockResolvedValue({
      code: 200,
      data: [],
    })

    const store = useMeetingStore()
    await store.loadCreateScreen()
    await store.loadCreateScreen()

    expect(meetingBindings.GetCreateDraft).toHaveBeenCalledTimes(2)
    expect(peopleBindings.GetMeetingPeopleOptions).toHaveBeenCalledTimes(2)
    expect(voiceBindings.ListInputDevices).toHaveBeenCalledTimes(2)
    expect(store.members.map((member) => member.name)).toEqual([
      '测试成员',
      '张三',
    ])
  })

  it('only exposes record-only retry for registered realtime ASR start errors', async () => {
    meetingBindings.StartMeeting.mockResolvedValue({
      code: 403,
      errorCode: 'ASR_AUTH_FAILED',
      message: '实时转写凭据无效或服务未开通',
    })
    const store = useMeetingStore()

    await store.startMeeting({
      meetingNo: 'M-01',
      suggestedMeetingNo: 'M-01',
      subject: '产品评审',
      memberIds: ['member-1'],
      temporaryNames: [],
      microphoneId: 'mic-1',
      asrMode: 'realtime',
      lanEnabled: false,
      lanInterfaceId: '',
    })

    expect(store.errorCode).toBe('ASR_AUTH_FAILED')
    expect(store.canRetryRecordOnly).toBe(true)

    meetingBindings.StartMeeting.mockResolvedValue({
      code: 500,
      errorCode: 'WORKSPACE_UNAVAILABLE',
      message: '工作目录不可用',
    })
    await store.startMeeting({
      meetingNo: 'M-01',
      suggestedMeetingNo: 'M-01',
      subject: '产品评审',
      memberIds: ['member-1'],
      temporaryNames: [],
      microphoneId: 'mic-1',
      asrMode: 'realtime',
      lanEnabled: false,
      lanInterfaceId: '',
    })

    expect(store.canRetryRecordOnly).toBe(false)
  })
})
