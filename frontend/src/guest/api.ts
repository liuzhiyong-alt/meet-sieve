export interface GuestEnvelope<T> {
  success: boolean
  code: string
  message: string
  data?: T
  request_id: string
}

export interface GuestMeeting {
  id: string
  subject: string
  started_at: number
  lan_state: string
  attachment_max_bytes: number
}

export interface GuestSession {
  session_id: string
  display_name: string
  expires_at: number
  meeting: GuestMeeting
}

export interface TimelineEvent {
  seq: number
  kind: 'message' | 'link' | 'attachment' | 'ai_answer'
  occurred_at: number
  entity_id: string
  display_name: string
  text?: string
  content_format?: 'plain' | 'markdown'
  url?: string
  original_name?: string
  media_type?: string
  size_bytes?: number
  description?: string
}

export interface TimelineNotification {
  type: 'timeline.changed'
  meeting_id: string
  latest_seq: number
  reason: string
}

export interface TimelinePage {
  events: TimelineEvent[]
  next_seq: number
  has_more: boolean
}

export class GuestAPIError extends Error {
  /** GuestAPIError 保留公开错误码，不保存请求正文或凭据。 */
  constructor(
    public readonly code: string,
    message: string,
  ) {
    super(message)
  }
}

/** request 调用同源 Guest API，并把公开错误响应转换为稳定异常。 */
async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', ...init?.headers },
  })
  const envelope = (await response.json()) as GuestEnvelope<T>
  if (!response.ok || !envelope.success || !envelope.data) {
    throw new GuestAPIError(envelope.code || 'INTERNAL_ERROR', envelope.message)
  }
  return envelope.data
}

/** createSession 使用 fragment 中的会议令牌换取 HttpOnly Cookie。 */
export function createSession(
  meetingToken: string,
  displayName: string,
): Promise<GuestSession> {
  return request('/api/v1/guest/sessions', {
    method: 'POST',
    body: JSON.stringify({
      meeting_token: meetingToken,
      display_name: displayName,
    }),
  })
}

/** getMeeting 以 Cookie 恢复当前访客会话。 */
export function getMeeting(): Promise<GuestMeeting> {
  return request('/api/v1/guest/meeting')
}

/** listEvents 按服务端 seq 增量读取公开事件。 */
export function listEvents(afterSeq: number): Promise<TimelinePage> {
  return request(`/api/v1/guest/events?after_seq=${afterSeq}&limit=100`)
}

/** sendContent 发送显式文字或链接，request ID 使重复点击保持幂等。 */
export function sendContent(
  kind: 'text' | 'link',
  content: string,
  requestID: string,
): Promise<{ seq: number }> {
  return request('/api/v1/guest/messages', {
    method: 'POST',
    body: JSON.stringify({ request_id: requestID, kind, content }),
  })
}

/** uploadAttachment 通过 XHR 暴露真实已发送字节并支持 AbortSignal。 */
export function uploadAttachment(
  file: File,
  requestID: string,
  signal: AbortSignal,
  onProgress: (sent: number, total: number) => void,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    const form = new FormData()
    form.append('file', file, file.name)
    xhr.open('POST', '/api/v1/guest/attachments')
    xhr.withCredentials = true
    xhr.setRequestHeader('Idempotency-Key', requestID)
    xhr.setRequestHeader('X-File-Size', String(file.size))
    xhr.upload.addEventListener('progress', (event) =>
      onProgress(event.loaded, event.total || file.size),
    )
    xhr.addEventListener('load', () => {
      let envelope: GuestEnvelope<unknown> | undefined
      try {
        envelope = JSON.parse(xhr.responseText) as GuestEnvelope<unknown>
      } catch {
        reject(new GuestAPIError('ATTACHMENT_UPLOAD_FAILED', '附件上传失败'))
        return
      }
      if (xhr.status >= 200 && xhr.status < 300 && envelope.success) resolve()
      else reject(new GuestAPIError(envelope.code, envelope.message))
    })
    xhr.addEventListener('error', () =>
      reject(
        new GuestAPIError(
          'ATTACHMENT_UPLOAD_FAILED',
          '网络连接中断，附件未上传',
        ),
      ),
    )
    xhr.addEventListener('abort', () =>
      reject(new DOMException('上传已取消', 'AbortError')),
    )
    signal.addEventListener('abort', () => xhr.abort(), { once: true })
    xhr.send(form)
  })
}
