import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

import {
  GetGapConflict,
  GetGapState,
  ResolveGapConflict,
} from '../../wailsjs/go/wails/GapBinding'
import { useGapStore } from './gap'

vi.mock('../../wailsjs/go/wails/GapBinding', () => ({
  GetGapConflict: vi.fn(),
  GetGapState: vi.fn(),
  ResolveGapConflict: vi.fn(),
  RetryGapCompensation: vi.fn(),
  StartGapCompensation: vi.fn(),
  StopGapCompensation: vi.fn(),
}))

describe('gap store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('解决冲突逐条提交目标 revision，成功后重建状态', async () => {
    vi.mocked(GetGapConflict).mockResolvedValue({
      code: 200,
      message: 'ok',
      data: {
        gap_id: 'gap-1',
        revision: 7,
        core_start_sample: 0,
        core_end_sample: 10,
        audio_start_sample: 0,
        audio_end_sample: 10,
        audio_clip_url: '/media/gap-clips/token',
        audio_clip_expires_at: 100,
        candidates: [{ text: '文件文字', start_sample: 0, end_sample: 10 }],
        existing: [
          {
            id: 'utterance-1',
            seq: 1,
            original_text: '原始',
            current_text: '当前',
            start_sample: 0,
            end_sample: 10,
            text_revision: 3,
          },
        ],
        context: [],
      },
    } as unknown as Awaited<ReturnType<typeof GetGapConflict>>)
    vi.mocked(ResolveGapConflict).mockResolvedValue({
      code: 200,
      message: 'ok',
    } as unknown as Awaited<ReturnType<typeof ResolveGapConflict>>)
    vi.mocked(GetGapState).mockResolvedValue({
      code: 200,
      message: 'ok',
      data: {
        meeting_id: 'meeting-1',
        state: 'completed',
        gaps: [],
        active_attempt_id: '',
        revision: 8,
      },
    } as unknown as Awaited<ReturnType<typeof GetGapState>>)
    const store = useGapStore()
    await store.loadConflict('meeting-1', 'gap-1')
    expect(
      await store.resolveConflict('save_manual_text', {
        'utterance-1': '人工文字',
      }),
    ).toBe(true)
    expect(ResolveGapConflict).toHaveBeenCalledWith(
      'meeting-1',
      'gap-1',
      7,
      'save_manual_text',
      [{ target_id: 'utterance-1', expected_revision: 3, text: '人工文字' }],
      expect.any(String),
    )
    expect(store.state).toBe('completed')
  })
})
