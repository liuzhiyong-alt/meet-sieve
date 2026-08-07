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

  it('reloads persisted availability after a failed Codex probe', async () => {
    bindings.ProbeAgent.mockResolvedValue({
      code: 502,
      message: 'Codex 运行环境不完整，请检查 Node.js 或重新安装 Codex',
    })
    bindings.GetAgentSettings.mockResolvedValue({
      code: 200,
      data: {
        wake_word: 'AI 助手',
        codex_executable_path: '/opt/homebrew/bin/codex',
        codex_proxy_port: 65400,
        probed_at: 1,
        updated_at: 1,
        availability: {
          state: 'unavailable',
          version: '',
          account_state: 'unknown',
          protocol_state: 'unchecked',
          message: 'Codex 运行环境不可用',
        },
      },
    })

    const store = useAgentStore()
    store.settings.availability = {
      state: 'available',
      version: 'codex-cli old',
      account_state: 'logged_in',
      protocol_state: 'compatible',
      message: 'Codex 可用',
    }

    expect(await store.probe()).toBe(false)
    expect(store.settings.availability.state).toBe('unavailable')
    expect(store.settings.availability.protocol_state).toBe('unchecked')
    expect(store.settings.codex_proxy_port).toBe(65400)
    expect(store.errorMessage).toBe(
      'Codex 运行环境不完整，请检查 Node.js 或重新安装 Codex',
    )
  })

  it('coalesces repeated retry actions while one request is running', async () => {
    let resolveRetry:
      ((value: { code: number; data: boolean }) => void) | undefined
    bindings.RetryAgent.mockReturnValue(
      new Promise((resolve) => {
        resolveRetry = resolve
      }),
    )
    bindings.GetAgentState.mockResolvedValue({
      code: 200,
      data: { state: 'available', meeting_id: 'meeting-1' },
    })
    const store = useAgentStore()
    store.meetingID = 'meeting-1'

    const first = store.retry()
    const repeated = store.retry()
    expect(store.retrying).toBe(true)
    expect(bindings.RetryAgent).toHaveBeenCalledTimes(1)

    resolveRetry?.({ code: 200, data: true })
    await Promise.all([first, repeated])
    expect(store.retrying).toBe(false)
    expect(store.runtime.state).toBe('available')
  })
})
