import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

import { GetFinalizationState } from '../../wailsjs/go/wails/FinalizationBinding'
import { useFinalizationStore } from './finalization'

vi.mock('../../wailsjs/go/wails/FinalizationBinding', () => ({
  GetFinalizationState: vi.fn(),
  RetryFinalization: vi.fn(),
  RetryAgentFinalSync: vi.fn(),
}))

describe('finalization store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('先恢复事实，并按 meeting/revision 拒绝旧事件', async () => {
    vi.mocked(GetFinalizationState).mockResolvedValue({
      code: 200,
      message: 'ok',
      data: {
        meeting_id: 'meeting-1',
        state: 'running',
        stage: 'merge_recording',
        error_code: '',
        revision: 3,
      },
    } as unknown as Awaited<ReturnType<typeof GetFinalizationState>>)
    const store = useFinalizationStore()
    await store.refresh('meeting-1')
    expect(store.projection.stage).toBe('merge_recording')
    expect(
      store.applyEvent({
        version: 1,
        data: { meeting_id: 'meeting-1', state: 'failed', revision: 2 },
      }),
    ).toBe(false)
    expect(
      store.applyEvent({
        version: 1,
        data: { meeting_id: 'meeting-1', state: 'failed', revision: 4 },
      }),
    ).toBe(true)
    expect(store.projection.state).toBe('failed')
  })
})
