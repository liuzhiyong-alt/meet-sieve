import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useVoiceStore } from './voice'

const bindings = vi.hoisted(() => ({
  CancelVoiceRecording: vi.fn(),
  ChooseVoiceSample: vi.fn(),
  DeleteAllVoiceSamples: vi.fn(),
  DeleteVoiceSample: vi.fn(),
  GetVoiceRecordingState: vi.fn(),
  ListInputDevices: vi.fn(),
  ListVoiceSamples: vi.fn(),
  ProcessVoiceSample: vi.fn(),
  RebuildVoiceEmbeddings: vi.fn(),
  StartVoiceRecording: vi.fn(),
  StopVoiceRecording: vi.fn(),
}))

vi.mock('../../wailsjs/go/wails/VoiceBinding', () => bindings)

describe('voice store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('uses the real recording snapshot returned by Go', async () => {
    bindings.GetVoiceRecordingState.mockResolvedValue({
      code: 200,
      data: { recording: true, level: 0.42, duration_ms: 1250 },
    })
    const store = useVoiceStore()
    store.recording = true

    await store.refreshRecordingState()

    expect(store.recordingLevel).toBe(0.42)
    expect(store.recordingDurationMS).toBe(1250)
  })

  it('clears the connecting state after the native device has started', async () => {
    bindings.StartVoiceRecording.mockResolvedValue({ code: 200, data: true })
    const store = useVoiceStore()
    store.memberId = 'member-1'

    expect(await store.startRecording('device-1', 'quiet')).toBe(true)
    expect(store.startingRecording).toBe(false)
    expect(store.recording).toBe(true)
  })

  it('consumes a failed file token instead of silently retrying a stale path', async () => {
    bindings.ProcessVoiceSample.mockResolvedValue({
      code: 422,
      message: 'WAV 文件不符合要求',
    })
    bindings.ListVoiceSamples.mockResolvedValue({ code: 200, data: [] })
    const store = useVoiceStore()
    store.memberId = 'member-1'
    store.selectedToken = 'one-time-token'
    store.selectedFileName = 'sample.wav'

    expect(await store.processWAV('quiet')).toBe(false)
    expect(store.selectedToken).toBe('')
    expect(store.selectedFileName).toBe('')
    expect(store.errorMessage).toBe('WAV 文件不符合要求')
  })
})
