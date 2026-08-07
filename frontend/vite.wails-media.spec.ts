import { describe, expect, it, vi } from 'vitest'

import {
  isWailsMediaRequest,
  wailsMediaFallbackPlugin,
} from './vite.wails-media'

describe('isWailsMediaRequest', () => {
  it.each([
    '/media/audio-clips/clip-token',
    '/media/audio-clips/clip-token?range=0',
    '/media/gap-clips/clip-token',
  ])('识别需要回退给 Wails 的媒体请求：%s', (url) => {
    expect(isWailsMediaRequest(url)).toBe(true)
  })

  it('不把 Vite 的应用资源和普通路由识别为媒体请求', () => {
    expect(isWailsMediaRequest('/src/main.ts')).toBe(false)
    expect(isWailsMediaRequest('/meetings/meeting-1')).toBe(false)
  })

  it('让媒体请求返回 404，并继续处理普通请求', () => {
    const use = vi.fn()
    const plugin = wailsMediaFallbackPlugin()
    plugin.configureServer?.({ middlewares: { use } } as never)
    const middleware = use.mock.calls[0]?.[0] as (
      request: { url?: string },
      response: { statusCode: number; end: () => void },
      next: () => void,
    ) => void
    const mediaResponse = { statusCode: 200, end: vi.fn() }
    const next = vi.fn()

    middleware({ url: '/media/audio-clips/clip-token' }, mediaResponse, next)
    expect(mediaResponse.statusCode).toBe(404)
    expect(mediaResponse.end).toHaveBeenCalledOnce()
    expect(next).not.toHaveBeenCalled()

    const routeResponse = { statusCode: 200, end: vi.fn() }
    middleware({ url: '/meetings/meeting-1' }, routeResponse, next)
    expect(routeResponse.end).not.toHaveBeenCalled()
    expect(next).toHaveBeenCalledOnce()
  })
})
