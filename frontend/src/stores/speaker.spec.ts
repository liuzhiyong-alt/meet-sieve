import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { GetSpeakerStatus } from '../../wailsjs/go/wails/CorrectionBinding'
import { useSpeakerStore } from './speaker'

vi.mock('../../wailsjs/go/wails/CorrectionBinding', () => ({
  GetSpeakerStatus: vi.fn(),
}))

const mockedStatus = vi.mocked(GetSpeakerStatus)

describe('speaker store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('向会中页面暴露正式校准档案缺失状态', async () => {
    mockedStatus.mockResolvedValue({
      code: 200,
      message: 'ok',
      data: {
        meeting_id: 'meeting-1',
        state: 'profile_missing',
        error_code: 'SPEAKER_PROFILE_MISSING',
      },
    } as never)
    const store = useSpeakerStore()

    await store.refresh('meeting-1')

    expect(store.state).toBe('profile_missing')
    expect(store.errorCode).toBe('SPEAKER_PROFILE_MISSING')
  })

  it('后端查询失败时不沿用旧的可用状态', async () => {
    mockedStatus.mockResolvedValue({
      code: 500,
      message: '读取说话人识别状态失败',
      errorCode: 'INTERNAL_ERROR',
    } as never)
    const store = useSpeakerStore()
    store.state = 'ready'

    await store.refresh('meeting-1')

    expect(store.state).toBe('unknown')
    expect(store.errorMessage).toBe('读取说话人识别状态失败')
  })
})
