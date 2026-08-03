import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useLANStore } from './lan'

const bindings = vi.hoisted(() => ({
  CancelGuestUpload: vi.fn(),
  GetLANStatus: vi.fn(),
  ListLANInterfaces: vi.fn(),
  RetryLAN: vi.fn(),
  StopLAN: vi.fn(),
}))

vi.mock('../../wailsjs/go/wails/LANBinding', () => bindings)

describe('lan store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('selects only the backend recommended private interface', async () => {
    bindings.ListLANInterfaces.mockResolvedValue({
      code: 200,
      data: {
        interfaces: [
          {
            id: 'wifi',
            name: 'Wi-Fi',
            address: '192.168.1.8',
            default_route: true,
          },
          {
            id: 'usb',
            name: 'USB LAN',
            address: '10.0.0.2',
            default_route: false,
          },
        ],
        recommended_id: 'wifi',
        reason: 'default_route',
      },
    })
    const store = useLANStore()
    await store.loadInterfaces()
    expect(store.selectedInterfaceID).toBe('wifi')
  })

  it('restores active uploads from backend status instead of frontend memory', async () => {
    bindings.GetLANStatus.mockResolvedValue({
      code: 200,
      data: {
        state: 'serving',
        online_count: 2,
        active_uploads: [
          { request_id: 'request', name: '资料.pdf', written: 40, total: 100 },
        ],
      },
    })
    const store = useLANStore()
    await store.refreshStatus()
    expect(store.status.active_uploads[0]).toMatchObject({
      written: 40,
      total: 100,
    })
  })
})
