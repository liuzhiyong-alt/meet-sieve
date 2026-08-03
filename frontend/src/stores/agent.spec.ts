import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useAgentStore } from './agent'

const bindings = vi.hoisted(() => ({
  AskAgent: vi.fn(),
  GetAgentSettings: vi.fn(),
  GetAgentState: vi.fn(),
  GetAgentTimeline: vi.fn(),
  InterruptAgent: vi.fn(),
  ProbeAgent: vi.fn(),
  RespondAgentApproval: vi.fn(),
  RetryAgent: vi.fn(),
  SaveAgentSettings: vi.fn(),
  StartWakeWordTest: vi.fn(),
  StopWakeWordTest: vi.fn(),
}))

vi.mock('../../wailsjs/go/wails/AgentBinding', () => bindings)

describe('agent store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('restores persisted AI timeline without duplicates', async () => {
    bindings.GetAgentTimeline.mockResolvedValueOnce({
      code: 200,
      data: [{ seq: 4, kind: 'ai.answer', turn_id: 'turn-1', text: '结论' }],
    }).mockResolvedValueOnce({ code: 200, data: [] })
    const store = useAgentStore()
    await store.restoreTimeline('meeting-1')
    await store.restoreTimeline('meeting-1')
    expect(store.timeline).toHaveLength(1)
    expect(bindings.GetAgentTimeline).toHaveBeenLastCalledWith(
      'meeting-1',
      4,
      200,
    )
  })

  it('ignores stale delta revisions and clears partial on failure', () => {
    const store = useAgentStore()
    store.meetingID = 'meeting-1'
    store.applyEvent({
      data: {
        meeting_id: 'meeting-1',
        turn_id: 'turn-1',
        type: 'answer_delta',
        delta: '新',
        revision: 2,
      },
    })
    store.applyEvent({
      data: {
        meeting_id: 'meeting-1',
        turn_id: 'turn-1',
        type: 'answer_delta',
        delta: '旧',
        revision: 1,
      },
    })
    expect(store.runtime.partial).toBe('新')
    store.applyEvent({
      data: {
        meeting_id: 'meeting-1',
        turn_id: 'turn-1',
        type: 'failed',
        error_code: 'AGENT_OUTPUT_INVALID',
        revision: 3,
      },
    })
    expect(store.runtime.partial).toBe('')
    expect(store.runtime.state).toBe('unavailable')
  })

  it('keeps wake test connected distinct from three-pass success', () => {
    const store = useAgentStore()
    store.applyWakeTestEvent({
      data: {
        state: 'running',
        matched: 0,
        required: 3,
        asr_state: 'connected',
      },
    })
    expect(store.wakeTest.state).toBe('running')
    expect(store.wakeTest.matched).toBe(0)
  })
})
