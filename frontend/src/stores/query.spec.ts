import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../../wailsjs/go/wails/QueryBinding', () => ({
  GetHome: vi.fn(),
  GetInterruptedRecovery: vi.fn(),
  GetMeetingDetail: vi.fn(),
  ListMeetingContent: vi.fn(),
  ListMeetings: vi.fn(),
  ListTranscript: vi.fn(),
}))

import { GetHome, ListMeetings } from '../../wailsjs/go/wails/QueryBinding'
import { wails } from '../../wailsjs/go/models'
import { useQueryStore } from './query'

describe('Query store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.resetAllMocks()
  })

  it('Binding 拒绝时结束加载并进入安全错误态', async () => {
    vi.mocked(GetHome).mockRejectedValue(new Error('底层路径不应展示'))
    const store = useQueryStore()

    expect(await store.loadHome()).toBe(false)
    expect(store.loading).toBe(false)
    expect(store.errorMessage).toBe('无法读取本地会议数据')
  })

  it('会议记录固定请求 10 场且不查询总数', async () => {
    vi.mocked(ListMeetings).mockResolvedValue(
      new wails.Result_meet_sieve_internal_transport_wails_MeetingPageDTO_({
        code: 200,
        message: 'ok',
        requestId: 'test',
        data: new wails.MeetingPageDTO({
          items: [],
          next_cursor: '',
          previous_cursor: '',
        }),
      }),
    )
    const store = useQueryStore()

    expect(await store.loadMeetings('产品', 'saved', '')).toBe(true)
    expect(ListMeetings).toHaveBeenCalledWith({
      search: '产品',
      status: 'saved',
      cursor: '',
      limit: 10,
    })
  })
})
