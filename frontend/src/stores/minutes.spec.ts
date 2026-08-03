import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

import {
  ConfirmMinute,
  GetMinutesState,
  SaveMinuteDraft,
} from '../../wailsjs/go/wails/MinutesBinding'
import { useMinutesStore } from './minutes'

vi.mock('../../wailsjs/go/wails/MinutesBinding', () => ({
  ConfirmMinute: vi.fn(),
  GenerateMinutes: vi.fn(),
  GetMinutesState: vi.fn(),
  ListMinuteVersions: vi.fn(),
  RestoreMinuteVersion: vi.fn(),
  SaveMinuteDraft: vi.fn(),
  StopMinutesGeneration: vi.fn(),
}))

const version = {
  id: 'version-1',
  version_no: 1,
  source: 'ai',
  content_markdown: '# 原稿',
  state: 'draft',
  is_current: true,
  created_at: 1,
}

describe('minutes store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    vi.mocked(GetMinutesState).mockResolvedValue({
      code: 200,
      message: 'ok',
      data: {
        meeting_id: 'meeting-1',
        state: 'draft',
        current: version,
        runtime_state: 'idle',
        projection_state: 'current',
        revision: 1,
      },
    } as unknown as Awaited<ReturnType<typeof GetMinutesState>>)
  })

  it('未保存修改会禁止确认，保存时从明确基线创建人工版本', async () => {
    const store = useMinutesStore()
    await store.refresh('meeting-1')
    store.setDraft('# 人工修改')
    expect(store.canConfirm).toBe(false)
    expect(await store.confirm()).toBe(false)
    expect(ConfirmMinute).not.toHaveBeenCalled()
    vi.mocked(SaveMinuteDraft).mockResolvedValue({
      code: 200,
      message: 'ok',
    } as unknown as Awaited<ReturnType<typeof SaveMinuteDraft>>)
    expect(await store.saveDraft()).toBe(true)
    expect(SaveMinuteDraft).toHaveBeenCalledWith(
      'meeting-1',
      'version-1',
      '# 人工修改',
      expect.any(String),
    )
  })
})
