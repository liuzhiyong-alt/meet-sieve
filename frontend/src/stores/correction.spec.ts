import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  CorrectSpeakerCluster,
  CorrectUtteranceSpeaker,
  CorrectUtteranceText,
  ListCorrectionEntries,
} from '../../wailsjs/go/wails/CorrectionBinding'
import { useCorrectionStore } from './correction'

vi.mock('../../wailsjs/go/wails/CorrectionBinding', () => ({
  CorrectSpeakerCluster: vi.fn(),
  CorrectUtteranceSpeaker: vi.fn(),
  CorrectUtteranceText: vi.fn(),
  CreateUtteranceAudioClip: vi.fn(),
  ListCorrectionEntries: vi.fn(),
  RetryRawRecordProjection: vi.fn(),
  RevokeAudioClip: vi.fn(),
}))

const mockedList = vi.mocked(ListCorrectionEntries)
const mockedCluster = vi.mocked(CorrectSpeakerCluster)
const mockedSpeaker = vi.mocked(CorrectUtteranceSpeaker)
const mockedText = vi.mocked(CorrectUtteranceText)

function entry(overrides: Record<string, unknown> = {}) {
  return {
    seq: 1,
    utterance_id: 'utterance-1',
    start_sample: 0,
    end_sample: 16000,
    original_text: '原文',
    current_text: '当前文字',
    speaker_display: '未知说话人 1',
    speaker_cluster_id: 'cluster-1',
    cluster_display_no: 1,
    cluster_count: 2,
    cluster_revision: 1,
    assignment_source: 'automatic_cluster',
    text_revision: 1,
    speaker_revision: 1,
    can_play: false,
    ...overrides,
  }
}

function page(entries: Array<{ seq: number }>) {
  return {
    code: 200,
    message: 'ok',
    data: {
      entries,
      participants: [
        {
          id: 'participant-1',
          display_name: '张三',
          kind: 'member',
          is_member: true,
        },
        {
          id: 'participant-2',
          display_name: '李四',
          kind: 'member',
          is_member: true,
        },
      ],
      next_seq: entries.at(-1)?.seq ?? 0,
    },
  } as never
}

function correctionResult() {
  return {
    code: 200,
    message: 'ok',
    data: { saved: true, no_op: false, projection_state: 'completed' },
  } as never
}

describe('correction store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('从 seq=0 分页恢复且工作区刷新不依赖旧内存', async () => {
    const firstEntries = Array.from({ length: 200 }, (_, index) =>
      entry({
        seq: index + 1,
        utterance_id: `utterance-${index + 1}`,
        start_sample: index * 16,
        end_sample: index * 16 + 16,
      }),
    )
    mockedList
      .mockResolvedValueOnce(page(firstEntries))
      .mockResolvedValueOnce(page([entry({ seq: 201, utterance_id: 'last' })]))
    const store = useCorrectionStore()
    store.entries = [entry({ seq: 999 })]

    await store.load('meeting-id')

    expect(mockedList).toHaveBeenNthCalledWith(1, 'meeting-id', 0, 200)
    expect(mockedList).toHaveBeenNthCalledWith(2, 'meeting-id', 200, 200)
    expect(store.entries).toHaveLength(201)
    expect(store.entries[0]?.utterance_id).toBe('utterance-1')
  })

  it('cluster 草稿会投影到未单独修改的片段，单段选择会覆盖该草稿', async () => {
    const store = useCorrectionStore()
    const first = entry({ current_participant_id: '' })
    const second = entry({
      seq: 2,
      utterance_id: 'utterance-2',
      current_participant_id: '',
    })
    store.entries = [first, second]

    store.setClusterDraft(first, 'participant-2')
    expect(store.speakerValue(second)).toBe('participant-2')

    store.setSpeakerDraft(second, 'participant-1')
    expect(store.speakerValue(second)).toBe('participant-1')
    expect(store.speakerDrafts['utterance-2']?.value).toBe('participant-1')
  })

  it('统一保存先提交 cluster，再提交单段说话人和文字', async () => {
    const initial = [
      entry({ current_participant_id: '' }),
      entry({
        seq: 2,
        utterance_id: 'utterance-2',
        current_participant_id: '',
      }),
    ]
    const afterCluster = initial.map((item) => ({
      ...item,
      speaker_revision: 2,
    }))
    const afterSpeaker = afterCluster.map((item) =>
      item.utterance_id === 'utterance-2'
        ? {
            ...item,
            speaker_revision: 3,
            current_participant_id: 'participant-1',
          }
        : item,
    )
    mockedList
      .mockResolvedValueOnce(page(initial))
      .mockResolvedValueOnce(page(afterCluster))
      .mockResolvedValueOnce(page(afterSpeaker))
      .mockResolvedValueOnce(page(afterSpeaker))
    mockedCluster.mockResolvedValue(correctionResult())
    mockedSpeaker.mockResolvedValue(correctionResult())
    mockedText.mockResolvedValue(correctionResult())
    const store = useCorrectionStore()
    await store.load('meeting-id')
    const [first, second] = store.entries
    store.setClusterDraft(first, 'participant-2')
    store.setSpeakerDraft(second, 'participant-1')
    store.setTextDraft(first, '修改后的文字')

    await expect(store.saveAll()).resolves.toBe(true)

    expect(mockedCluster).toHaveBeenCalledWith(
      expect.objectContaining({
        expected_revision: 1,
        participant_id: 'participant-2',
      }),
    )
    expect(mockedSpeaker).toHaveBeenCalledWith(
      expect.objectContaining({
        utterance_id: 'utterance-2',
        expected_revision: 2,
      }),
    )
    expect(mockedText).toHaveBeenCalledWith(
      expect.objectContaining({
        utterance_id: 'utterance-1',
        value: '修改后的文字',
      }),
    )
    expect(mockedCluster.mock.invocationCallOrder[0]).toBeLessThan(
      mockedSpeaker.mock.invocationCallOrder[0] ?? 0,
    )
    expect(mockedSpeaker.mock.invocationCallOrder[0]).toBeLessThan(
      mockedText.mock.invocationCallOrder[0] ?? 0,
    )
    expect(store.isDirty).toBe(false)
  })

  it('部分失败时保留未成功草稿，仅清除已经保存的文字', async () => {
    const entries = [entry({ current_participant_id: '' })]
    mockedList
      .mockResolvedValueOnce(page(entries))
      .mockResolvedValueOnce(page(entries))
      .mockResolvedValueOnce(page(entries))
      .mockResolvedValueOnce(page(entries))
    mockedCluster.mockResolvedValue({
      code: 409,
      message: '内容已变化，请检查后再次保存',
    } as never)
    mockedText.mockResolvedValue(correctionResult())
    const store = useCorrectionStore()
    await store.load('meeting-id')
    const first = store.entries[0]
    store.setClusterDraft(first, 'participant-2')
    store.setTextDraft(first, '修改后的文字')

    await expect(store.saveAll()).resolves.toBe(false)

    expect(store.clusterDrafts['cluster-1']).toBeDefined()
    expect(store.textDrafts['utterance-1']).toBeUndefined()
    expect(store.notice).toBe('部分修改未保存，请检查后再次保存。')
  })

  it('后端失败时不把旧内存伪装成新会议事实', async () => {
    mockedList.mockResolvedValue({
      code: 500,
      errorCode: 'INTERNAL_ERROR',
      message: '加载失败',
      requestId: 'request',
    } as never)
    const store = useCorrectionStore()
    await store.load('meeting-id')
    expect(store.entries).toEqual([])
    expect(store.errorMessage).toBe('加载失败')
  })
})
