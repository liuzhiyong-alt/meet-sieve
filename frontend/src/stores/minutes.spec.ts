import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

import {
  GetMinutesSettings,
  GetMinutesState,
  SaveMinuteDraft,
  SaveMinutesSettings,
} from '../../wailsjs/go/wails/MinutesBinding'
import { useMinutesStore } from './minutes'

vi.mock('../../wailsjs/go/wails/MinutesBinding', () => ({
  GenerateMinutes: vi.fn(),
  GetMinutesSettings: vi.fn(),
  GetMinutesState: vi.fn(),
  ListMinuteVersions: vi.fn(),
  RestoreMinuteVersion: vi.fn(),
  SaveMinuteDraft: vi.fn(),
  SaveMinutesSettings: vi.fn(),
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

  it('以原始 Markdown 为编辑基线并保存当前内容', async () => {
    const store = useMinutesStore()
    await store.refresh('meeting-1')
    store.setDraft('# 人工修改')
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

  it('读取默认要求并保存用户修改后的提示词', async () => {
    vi.mocked(GetMinutesSettings).mockResolvedValue({
      code: 200,
      message: 'ok',
      data: { prompt: '默认要求', is_default: true, updated_at: 1 },
    } as unknown as Awaited<ReturnType<typeof GetMinutesSettings>>)
    vi.mocked(SaveMinutesSettings).mockResolvedValue({
      code: 200,
      message: 'ok',
      data: { prompt: '突出待办', is_default: false, updated_at: 2 },
    } as unknown as Awaited<ReturnType<typeof SaveMinutesSettings>>)
    const store = useMinutesStore()

    await store.loadSettings()
    expect(store.settings.prompt).toBe('默认要求')
    expect(await store.saveSettings('突出待办')).toBe(true)
    expect(SaveMinutesSettings).toHaveBeenCalledWith('突出待办')
  })

  it('恢复默认要求时向后端保存空内容并使用返回的有效默认值', async () => {
    vi.mocked(SaveMinutesSettings).mockResolvedValue({
      code: 200,
      message: 'ok',
      data: { prompt: '默认要求', is_default: true, updated_at: 3 },
    } as unknown as Awaited<ReturnType<typeof SaveMinutesSettings>>)
    const store = useMinutesStore()

    expect(await store.restoreDefault()).toBe(true)
    expect(SaveMinutesSettings).toHaveBeenCalledWith('')
    expect(store.settings).toEqual({
      prompt: '默认要求',
      is_default: true,
      updated_at: 3,
    })
    expect(store.settingsNotice).toBe('已恢复默认会议纪要要求')
  })
})
