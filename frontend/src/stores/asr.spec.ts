import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useASRStore } from './asr'

const bindings = vi.hoisted(() => ({
  GetASRSettings: vi.fn(),
  GetASRTimeline: vi.fn(),
  GetRawRecordState: vi.fn(),
  RetryRealtimeASR: vi.fn(),
  SaveASRSettings: vi.fn(),
  TestASRConnection: vi.fn(),
}))

vi.mock('../../wailsjs/go/wails/ASRBinding', () => bindings)

describe('asr store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    bindings.GetRawRecordState.mockResolvedValue({
      code: 200,
      data: { state: 'current' },
    })
  })

  it('restores persisted finals by sequence without duplicates', async () => {
    bindings.GetASRTimeline.mockResolvedValueOnce({
      code: 200,
      data: [
        {
          seq: 1,
          kind: 'utterance.final',
          text: '会议开始',
          start_sample: 0,
          end_sample: 16000,
        },
      ],
    }).mockResolvedValueOnce({ code: 200, data: [] })

    const store = useASRStore()
    await store.restoreTimeline('meeting-1')
    await store.restoreTimeline('meeting-1')

    expect(store.timeline).toHaveLength(1)
    expect(bindings.GetASRTimeline).toHaveBeenLastCalledWith(
      'meeting-1',
      1,
      200,
    )
  })

  it('keeps only the highest partial revision and never persists it', () => {
    const store = useASRStore()
    store.meetingID = 'meeting-1'
    store.applyPartial({
      data: {
        meeting_id: 'meeting-1',
        result_id: 'result-1',
        revision: 2,
        text: '较新文本',
        start_sample: 0,
        end_sample: 100,
      },
    })
    store.applyPartial({
      data: {
        meeting_id: 'meeting-1',
        result_id: 'result-1',
        revision: 1,
        text: '旧文本',
        start_sample: 0,
        end_sample: 80,
      },
    })

    expect(store.orderedPartials[0]?.text).toBe('较新文本')
    expect(store.timeline).toHaveLength(0)
  })

  it('saves only APP Key changes and never sends the saved mask', async () => {
    bindings.SaveASRSettings.mockResolvedValue({
      code: 200,
      data: {
        api_key_configured: true,
        api_key_mask: '••••5678',
        requires_api_key_upgrade: false,
      },
    })
    const store = useASRStore()
    await store.saveAPIKey('')

    expect(bindings.SaveASRSettings).toHaveBeenCalledWith(
      expect.objectContaining({ api_key: { action: 'keep', value: '' } }),
    )
    expect(bindings.SaveASRSettings.mock.calls[0]?.[0]).not.toHaveProperty(
      'mode',
    )
    expect(bindings.SaveASRSettings.mock.calls[0]?.[0]).not.toHaveProperty(
      'app_id',
    )
  })
})
