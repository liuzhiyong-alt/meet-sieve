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
        session_id: 'session-1',
        generation: 1,
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
        session_id: 'session-1',
        generation: 1,
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

  it('清除旧 session 后接受新 session 的低 revision，并拒绝旧事件复活', () => {
    const store = useTimelineStore()
    store.resetMeeting('meeting-1')
    store.applyPartial({
      data: {
        meeting_id: 'meeting-1',
        session_id: 'session-old',
        generation: 1,
        result_id: 'stream',
        revision: 9,
        text: '旧内容',
        start_sample: 0,
        end_sample: 100,
      },
    })
    store.applyPartialClear({
      data: {
        meeting_id: 'meeting-1',
        session_id: 'session-old',
        generation: 1,
      },
    })
    store.applyPartial({
      data: {
        meeting_id: 'meeting-1',
        session_id: 'session-old',
        generation: 1,
        result_id: 'stream',
        revision: 10,
        text: '迟到旧内容',
        start_sample: 0,
        end_sample: 120,
      },
    })
    store.applyPartial({
      data: {
        meeting_id: 'meeting-1',
        session_id: 'session-new',
        generation: 2,
        result_id: 'stream',
        revision: 1,
        text: '恢复后的内容',
        start_sample: 200,
        end_sample: 300,
      },
    })

    expect(store.orderedPartials.map((partial) => partial.text)).toEqual([
      '恢复后的内容',
    ])
  })

  it('刷新同一 seq 的更高说话人 revision，并保留已加载历史', async () => {
    binding.GetMeetingTimeline.mockResolvedValueOnce({
      code: 200,
      data: {
        entries: [
          {
            seq: 2,
            kind: 'utterance',
            occurred_at: 1,
            source: 'asr',
            text: '氢氧化铝抗酸剂不影响其吸收效率。',
            speaker_label: '刘志勇',
            speaker_revision: 2,
          },
        ],
        oldest_seq: 2,
        latest_seq: 2,
        has_older: true,
        has_more_after: false,
      },
    })
    const store = useTimelineStore()
    store.resetMeeting('meeting-1')
    store.entries = [
      {
        seq: 1,
        kind: 'message',
        occurred_at: 0,
        source: 'host',
        text: '更早内容',
      },
      {
        seq: 2,
        kind: 'utterance',
        occurred_at: 1,
        source: 'asr',
        text: '氢氧化铝抗酸剂不影响其吸收效率。',
        speaker_label: '未知说话人 1',
        speaker_revision: 1,
      },
    ]
    store.latestCursor = 2
    store.oldestCursor = 1

    await store.refreshLatestProjection()

    expect(store.entries.map((entry) => entry.seq)).toEqual([1, 2])
    expect(store.entries[1].speaker_label).toBe('刘志勇')
    expect(store.entries[1].speaker_revision).toBe(2)
  })

  it('拒绝较旧的说话人 revision 覆盖当前投影', () => {
    const store = useTimelineStore()
    store.entries = [
      {
        seq: 2,
        kind: 'utterance',
        occurred_at: 1,
        source: 'asr',
        speaker_label: '刘志勇',
        speaker_revision: 3,
      },
    ]

    store.mergeEntries([
      {
        seq: 2,
        kind: 'utterance',
        occurred_at: 1,
        source: 'asr',
        speaker_label: '未知说话人 1',
        speaker_revision: 2,
      },
    ])

    expect(store.entries[0].speaker_label).toBe('刘志勇')
    expect(store.entries[0].speaker_revision).toBe(3)
  })
})
