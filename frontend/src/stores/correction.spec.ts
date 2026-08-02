import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ListCorrectionEntries } from '../../wailsjs/go/wails/CorrectionBinding'
import { useCorrectionStore } from './correction'

vi.mock('../../wailsjs/go/wails/CorrectionBinding', () => ({
  AddUtteranceToVoiceSamples: vi.fn(),
  CorrectSpeakerCluster: vi.fn(),
  CorrectUtteranceSpeaker: vi.fn(),
  CorrectUtteranceText: vi.fn(),
  CreateUtteranceAudioClip: vi.fn(),
  GetSpeakerStatus: vi.fn().mockResolvedValue({
    code: 200,
    message: 'ok',
    data: {
      meeting_id: 'meeting-id',
      state: 'profile_missing',
      error_code: 'SPEAKER_PROFILE_MISSING',
    },
  }),
  ListCorrectionEntries: vi.fn(),
  RetryRawRecordProjection: vi.fn(),
  RetrySpeakerProcessing: vi.fn(),
  RevokeAudioClip: vi.fn(),
}))

const mockedList = vi.mocked(ListCorrectionEntries)

describe('correction store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('从 seq=0 分页恢复且工作区刷新不依赖旧内存', async () => {
    const firstEntries = Array.from({ length: 200 }, (_, index) => ({
      seq: index + 1,
      utterance_id: `utterance-${index + 1}`,
      start_sample: index * 16,
      end_sample: index * 16 + 16,
      original_text: '原文',
      current_text: '当前文字',
      speaker_display: '未知说话人 1',
      assignment_source: 'automatic_cluster',
      text_revision: 1,
      speaker_revision: 1,
      can_play: false,
      can_enroll: false,
    }))
    mockedList
      .mockResolvedValueOnce({
        code: 200,
        message: 'ok',
        requestId: 'request-1',
        data: { entries: firstEntries, participants: [], next_seq: 200 },
      } as never)
      .mockResolvedValueOnce({
        code: 200,
        message: 'ok',
        requestId: 'request-2',
        data: {
          entries: [{ ...firstEntries[0], seq: 201, utterance_id: 'last' }],
          participants: [],
          next_seq: 201,
        },
      } as never)
    const store = useCorrectionStore()
    store.entries = [{ ...firstEntries[0], seq: 999 }]

    await store.load('meeting-id')

    expect(mockedList).toHaveBeenNthCalledWith(1, 'meeting-id', 0, 200)
    expect(mockedList).toHaveBeenNthCalledWith(2, 'meeting-id', 200, 200)
    expect(store.entries).toHaveLength(201)
    expect(store.selectedID).toBe('utterance-1')
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
