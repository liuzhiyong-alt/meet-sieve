import type { Plugin } from 'vite'

const wailsMediaPrefixes = ['/media/audio-clips/', '/media/gap-clips/']

/** isWailsMediaRequest 判断请求是否必须回退给 Wails 的受控媒体 Handler。 */
export function isWailsMediaRequest(url: string | undefined): boolean {
  const pathname = url?.split('?', 1)[0] ?? ''
  return wailsMediaPrefixes.some((prefix) => pathname.startsWith(prefix))
}

/** wailsMediaFallbackPlugin 让 Vite 对受控媒体返回 404，触发 Wails 的 AssetServer 回退。 */
export function wailsMediaFallbackPlugin(): Plugin {
  return {
    name: 'meetsieve:wails-media-fallback',
    configureServer(server) {
      server.middlewares.use((request, response, next) => {
        if (!isWailsMediaRequest(request.url)) {
          next()
          return
        }
        response.statusCode = 404
        response.end()
      })
    },
  }
}
