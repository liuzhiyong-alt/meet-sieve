import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useVoiceModelStore } from './voiceModel'

const bindings = vi.hoisted(() => ({
  DownloadOfficialVoiceModel: vi.fn(),
  GetVoiceModelState: vi.fn(),
  ImportOfflineVoiceModel: vi.fn(),
}))

vi.mock('../../wailsjs/go/wails/VoiceModelBinding', () => bindings)

describe('voice model store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('keeps the verified backend model identity after installation', async () => {
    bindings.DownloadOfficialVoiceModel.mockResolvedValue({
      code: 200,
      data: {
        state: 'ready',
        usable: true,
        modelId: 'campplus',
        modelName: 'CAM++ 中文通用',
        modelVersion: '1.0.0-ms1',
        modelSize: 28243826,
        location: '本机应用数据目录',
      },
    })
    const store = useVoiceModelStore()

    await store.download()

    expect(store.model.usable).toBe(true)
    expect(store.model.modelVersion).toBe('1.0.0-ms1')
    expect(store.errorMessage).toBe('')
  })
})
