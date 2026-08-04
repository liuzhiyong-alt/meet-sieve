// @vitest-environment happy-dom

import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useTimelineStore } from './timeline'

const binding = {
  GetMeetingTimeline: vi.fn(),
  SendMeetingMessage: vi.fn(),
  ChooseAndSendMeetingAttachment: vi.fn(),
  GetLiveMeetingStatus: vi.fn(),
}

describe('timeline store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    Object.assign(window, { go: { wails: { ContentBinding: binding } } })
  })

  it('按服务端游标恢复不可见事件之后的新消息', async () => {
    binding.GetMeetingTimeline.mockResolvedValueOnce({
      code: 200,
      data: {
        entries: [],
        oldest_seq: 1,
        latest_seq: 1,
        has_older: false,
        has_more_after: false,
      },
    }).mockResolvedValueOnce({
      code: 200,
      data: {
        entries: [
          {
            seq: 2,
            kind: 'message',
            occurred_at: 1,
            source: 'guest',
            text: '**确认**',
            content_format: 'markdown',
          },
        ],
        oldest_seq: 2,
        latest_seq: 2,
        has_older: false,
        has_more_after: false,
      },
    })

    const store = useTimelineStore()
    await store.loadLatest('meeting-1')
    await store.recoverAfter()

    expect(binding.GetMeetingTimeline).toHaveBeenLastCalledWith({
      meeting_id: 'meeting-1',
      direction: 'after',
      cursor_seq: 1,
      limit: 200,
    })
    expect(store.entries.map((entry) => entry.seq)).toEqual([2])
    expect(store.latestSeq).toBe(2)
  })

  it('只保留实时转写的最高 revision，并在 final 后移除临时结果', () => {
    const store = useTimelineStore()
    store.resetMeeting('meeting-1')
    store.applyPartial({
      data: {
        meeting_id: 'meeting-1',
        result_id: 'result-1',
        revision: 2,
        text: '较新内容',
        start_sample: 0,
        end_sample: 16000,
      },
    })
    store.applyPartial({
      data: {
        meeting_id: 'meeting-1',
        result_id: 'result-1',
        revision: 1,
        text: '旧内容',
        start_sample: 0,
        end_sample: 8000,
      },
    })
    store.entries = [
      {
        seq: 2,
        kind: 'utterance',
        occurred_at: 1,
        source: 'asr',
        text: '最终内容',
        end_sample: 16000,
      },
    ]
    store.clearCommittedPartials()

    expect(store.orderedPartials).toHaveLength(0)
  })
})
