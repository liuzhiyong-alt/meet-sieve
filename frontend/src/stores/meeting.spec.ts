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
})
