<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import {
  createSession,
  getMeeting,
  GuestAPIError,
  listEvents,
  sendContent,
  type GuestMeeting,
  type TimelineEvent,
  uploadAttachment,
} from './api'

type Screen = 'loading' | 'identity' | 'active' | 'ended'

const screen = ref<Screen>('loading')
const displayName = ref(sessionStorage.getItem('meetsieve_guest_name') ?? '')
const meeting = ref<GuestMeeting | null>(null)
const events = ref<TimelineEvent[]>([])
const nextSeq = ref(0)
const kind = ref<'text' | 'link'>('text')
const content = ref('')
const errorMessage = ref('')
const sending = ref(false)
const upload = ref<{
  name: string
  sent: number
  total: number
  requestID: string
} | null>(null)
const uploadError = ref('')
let pendingContentRequestID = ''
let uploadController: AbortController | undefined
let pollTimer: number | undefined
let pollFailures = 0

const meetingToken = readMeetingToken()
const uploadPercent = computed(() => {
  if (!upload.value?.total) return 0
  return Math.min(
    100,
    Math.round((upload.value.sent / upload.value.total) * 100),
  )
})

onMounted(async () => {
  document.addEventListener('visibilitychange', handleVisibilityChange)
  if (meetingToken) {
    screen.value = 'identity'
    return
  }
  try {
    meeting.value = await getMeeting()
    screen.value = 'active'
    await refreshEvents()
    schedulePoll(1000)
  } catch (error) {
    endSession(publicMessage(error, '访客入口已停止'))
  }
})

onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', handleVisibilityChange)
  stopPoll()
  uploadController?.abort()
})

/** readMeetingToken 只从 fragment 读取入口令牌，不让它进入 HTTP 请求。 */
function readMeetingToken(): string {
  const parameters = new URLSearchParams(window.location.hash.slice(1))
  return parameters.get('k') ?? ''
}

/** joinMeeting 建立 Cookie session，成功后立即从地址栏移除 fragment。 */
async function joinMeeting(): Promise<void> {
  const name = displayName.value.trim()
  if (!name || !meetingToken) return
  errorMessage.value = ''
  sending.value = true
  try {
    const session = await createSession(meetingToken, name)
    meeting.value = session.meeting
    displayName.value = session.display_name
    sessionStorage.setItem('meetsieve_guest_name', session.display_name)
    history.replaceState(
      null,
      '',
      `${window.location.pathname}${window.location.search}`,
    )
    screen.value = 'active'
    await refreshEvents()
    schedulePoll(1000)
  } catch (error) {
    errorMessage.value = publicMessage(error, '无法加入本场会议')
  } finally {
    sending.value = false
  }
}

/** submitContent 复用同一 request ID 完成单次提交，避免重复点击生成重复消息。 */
async function submitContent(): Promise<void> {
  if (sending.value || !content.value.trim()) return
  if (!pendingContentRequestID) pendingContentRequestID = crypto.randomUUID()
  sending.value = true
  errorMessage.value = ''
  try {
    await sendContent(kind.value, content.value, pendingContentRequestID)
    pendingContentRequestID = ''
    content.value = ''
    await refreshEvents()
  } catch (error) {
    handleActiveError(error, '消息未发送')
  } finally {
    sending.value = false
  }
}

/** chooseAttachment 校验浏览器已知大小后启动不可恢复的单次流式上传。 */
async function chooseAttachment(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || upload.value) return
  if (
    file.size <= 0 ||
    file.size > (meeting.value?.attachment_max_bytes ?? 500 * 1024 * 1024)
  ) {
    uploadError.value = '单个附件必须大于 0 字节且不超过 500 MB。'
    return
  }
  uploadError.value = ''
  uploadController = new AbortController()
  const current = {
    name: file.name,
    sent: 0,
    total: file.size,
    requestID: crypto.randomUUID(),
  }
  upload.value = current
  try {
    await uploadAttachment(
      file,
      current.requestID,
      uploadController.signal,
      (sent, total) => {
        if (upload.value?.requestID === current.requestID)
          upload.value = { ...current, sent, total }
      },
    )
    upload.value = null
    await refreshEvents()
  } catch (error) {
    upload.value = null
    if (error instanceof DOMException && error.name === 'AbortError')
      uploadError.value = '上传已取消，没有留下正式附件。'
    else if (isTerminalError(error))
      endSession(publicMessage(error, '本场会议已结束'))
    else
      uploadError.value = publicMessage(
        error,
        '网络连接中断，没有留下正式附件。',
      )
  } finally {
    uploadController = undefined
  }
}

/** cancelUpload 中止浏览器请求；服务端 context 会清理同一暂存文件。 */
function cancelUpload(): void {
  uploadController?.abort()
}

/** refreshEvents 每次读取一页并按 seq 去重合并，避免轮询突发触发限流。 */
async function refreshEvents(): Promise<void> {
  if (screen.value !== 'active') return
  const page = await listEvents(nextSeq.value)
  const known = new Set(events.value.map((item) => item.seq))
  events.value.push(...page.events.filter((item) => !known.has(item.seq)))
  events.value.sort((left, right) => left.seq - right.seq)
  nextSeq.value = Math.max(nextSeq.value, page.next_seq)
  if (page.has_more) schedulePoll(1000)
}

/** poll 在页面可见时查询事实，短暂失败使用有界指数退避。 */
async function poll(): Promise<void> {
  if (screen.value !== 'active' || document.hidden) return
  try {
    await refreshEvents()
    pollFailures = 0
  } catch (error) {
    pollFailures++
    if (isTerminalError(error)) {
      endSession(publicMessage(error, '本场会议已结束'))
      return
    }
  }
  schedulePoll(Math.min(30_000, 1000 * 2 ** pollFailures))
}

/** schedulePoll 保证任意时刻只存在一个事件轮询计时器。 */
function schedulePoll(delay: number): void {
  stopPoll()
  if (screen.value === 'active' && !document.hidden)
    pollTimer = window.setTimeout(() => void poll(), delay)
}

/** stopPoll 释放事件轮询计时器。 */
function stopPoll(): void {
  if (pollTimer !== undefined) window.clearTimeout(pollTimer)
  pollTimer = undefined
}

/** handleVisibilityChange 隐藏时停止网络活动，恢复可见时立即同步。 */
function handleVisibilityChange(): void {
  if (document.hidden) stopPoll()
  else if (screen.value === 'active') schedulePoll(0)
}

/** handleActiveError 把终态错误切换到停止页，其余错误留在操作上下文。 */
function handleActiveError(error: unknown, fallback: string): void {
  if (isTerminalError(error)) endSession(publicMessage(error, '本场会议已结束'))
  else errorMessage.value = publicMessage(error, fallback)
}

/** endSession 清除可编辑内容并取消未完成上传。 */
function endSession(message: string): void {
  stopPoll()
  uploadController?.abort()
  content.value = ''
  pendingContentRequestID = ''
  errorMessage.value = message
  screen.value = 'ended'
}

/** isTerminalError 判断入口代际、会话或会议是否已经不可继续使用。 */
function isTerminalError(error: unknown): boolean {
  return (
    error instanceof GuestAPIError &&
    [
      'LAN_GENERATION_CHANGED',
      'LAN_SESSION_INVALID',
      'LAN_SESSION_EXPIRED',
      'LAN_MEETING_ENDED',
    ].includes(error.code)
  )
}

/** publicMessage 只显示服务端公开文案，不拼接浏览器或网络内部异常。 */
function publicMessage(error: unknown, fallback: string): string {
  return error instanceof GuestAPIError && error.message
    ? error.message
    : fallback
}

/** formatTime 把事件时间格式化为访客设备本地时分。 */
function formatTime(milliseconds: number): string {
  return new Intl.DateTimeFormat('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(milliseconds)
}

/** formatBytes 以易读二进制单位展示真实字节数。 */
function formatBytes(bytes = 0): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 ** 2) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 ** 2).toFixed(1)} MB`
}

/** eventLabel 返回公开时间线事件的统一中文类型。 */
function eventLabel(event: TimelineEvent): string {
  return {
    message: '会议消息',
    link: '链接',
    attachment: '附件',
    ai_answer: 'AI 回答',
  }[event.kind]
}

/** reloadPage 由用户显式重新检查当前入口状态。 */
function reloadPage(): void {
  window.location.reload()
}
</script>

<template>
  <main v-if="screen === 'loading'" class="guest-centered" aria-busy="true">
    <p>正在检查会议状态…</p>
  </main>

  <main v-else-if="screen === 'identity'" class="join-shell">
    <div class="brand">
      <span class="brand-mark">M</span><span>会议消息</span>
    </div>
    <section class="page-head">
      <p class="eyebrow">局域网访客</p>
      <h1>加入本场会议</h1>
      <p>填写临时显示名称。这里不展示录音、实时转写、正式成员库或会后纪要。</p>
    </section>
    <form class="card" @submit.prevent="joinMeeting">
      <label class="label" for="display-name">你的临时显示名称</label>
      <input
        id="display-name"
        v-model="displayName"
        class="field"
        maxlength="40"
        autocomplete="name"
        placeholder="例如：林舟"
        autofocus
      />
      <p class="help">最多 40 个字符，仅用于本场消息和资料署名。</p>
      <p v-if="errorMessage" class="field-error" role="alert">
        {{ errorMessage }}
      </p>
      <button
        class="btn btn-primary full"
        type="submit"
        :disabled="sending || !displayName.trim()"
      >
        {{ sending ? '正在加入…' : '进入会议消息' }}
      </button>
    </form>
    <div class="notice warn">
      <div>
        <strong>只在可信的私有网络使用</strong>
        <p>本页面使用 HTTP。请不要在公共 Wi-Fi 中发送敏感资料。</p>
      </div>
    </div>
  </main>

  <template v-else-if="screen === 'active' && meeting">
    <header class="guest-header">
      <div class="brand">
        <span class="brand-mark">M</span><span>会议消息</span>
      </div>
      <div class="guest-identity">
        <span class="meta">临时显示名称</span
        ><span class="avatar" aria-hidden="true">{{
          displayName.slice(0, 1)
        }}</span
        ><strong>{{ displayName }}</strong>
      </div>
    </header>
    <main class="guest-main">
      <section class="page-head active-head">
        <div>
          <p class="eyebrow">局域网访客</p>
          <h1>{{ meeting.subject || '未命名会议' }}</h1>
          <p>你只能查看本场会议消息、链接、公开 AI 回答和已上传资料。</p>
        </div>
        <span class="status ok">会议进行中</span>
      </section>
      <p v-if="errorMessage" class="notice bad" role="alert">
        {{ errorMessage }}
      </p>
      <section class="guest-layout">
        <article class="card">
          <div class="card-head">
            <h2>会议消息</h2>
            <span class="meta">{{
              events.length
                ? `最近更新 ${formatTime(events.at(-1)?.occurred_at ?? 0)}`
                : '暂无公开消息'
            }}</span>
          </div>
          <ol class="guest-message-list" aria-label="会议消息时间线">
            <li v-for="item in events" :key="item.seq" class="guest-message">
              <span class="meta"
                >{{ formatTime(item.occurred_at) }} ·
                {{ eventLabel(item) }}</span
              >
              <p v-if="item.kind === 'message' || item.kind === 'ai_answer'">
                <strong>{{
                  item.display_name ||
                  (item.kind === 'ai_answer' ? 'AI 助手' : '访客')
                }}</strong
                >：{{ item.text }}
              </p>
              <p v-else-if="item.kind === 'link'">
                <strong>{{ item.display_name || '访客' }}</strong
                >：<a
                  :href="item.url"
                  target="_blank"
                  rel="noopener noreferrer"
                  >{{ item.url }}</a
                >
              </p>
              <p v-else>
                <strong>{{ item.display_name || '访客' }}</strong
                >：<a
                  :href="`/api/v1/guest/attachments/${encodeURIComponent(item.entity_id)}`"
                  >{{ item.original_name }}</a
                >
                · {{ formatBytes(item.size_bytes) }}
              </p>
            </li>
          </ol>
          <form class="guest-composer" @submit.prevent="submitContent">
            <label class="label" for="guest-message">发送会议消息</label>
            <textarea
              id="guest-message"
              v-model="content"
              class="textarea"
              maxlength="10000"
              :placeholder="
                kind === 'link'
                  ? '输入完整的 http:// 或 https:// 链接'
                  : '输入文字；发送链接时请选择“链接”类型'
              "
            />
            <div class="composer-actions">
              <label class="label compact" for="message-kind"
                >内容类型<select
                  id="message-kind"
                  v-model="kind"
                  class="select"
                >
                  <option value="text">文字</option>
                  <option value="link">链接</option>
                </select></label
              ><button
                class="btn btn-primary"
                type="submit"
                :disabled="sending || !content.trim()"
              >
                {{ sending ? '正在发送…' : '发送消息' }}
              </button>
            </div>
          </form>
        </article>
        <aside class="stack">
          <article class="card">
            <div class="card-head">
              <h3>发送资料</h3>
              <span v-if="upload" class="status warn">1 个上传中</span>
            </div>
            <div v-if="upload" class="upload-item">
              <div class="row-between">
                <div class="upload-copy">
                  <strong>{{ upload.name }}</strong>
                  <p class="meta">
                    {{ formatBytes(upload.sent) }} /
                    {{ formatBytes(upload.total) }}
                  </p>
                </div>
                <button
                  class="btn btn-quiet"
                  type="button"
                  @click="cancelUpload"
                >
                  取消上传
                </button>
              </div>
              <div
                class="progress"
                role="progressbar"
                aria-label="附件上传进度"
                aria-valuemin="0"
                aria-valuemax="100"
                :aria-valuenow="uploadPercent"
              >
                <span :style="{ width: `${uploadPercent}%` }" />
              </div>
              <span class="help"
                >正在上传。关闭页面或会议结束会取消本次上传。</span
              >
            </div>
            <label
              class="btn btn-primary full file-button"
              :class="{ disabled: Boolean(upload) }"
              >选择附件<input
                type="file"
                :disabled="Boolean(upload)"
                @change="chooseAttachment"
            /></label>
            <p class="help">
              单个附件最大 500
              MB。可执行文件、安装包和动态库不能上传；压缩包不会自动展开检查。
            </p>
          </article>
          <div v-if="uploadError" class="notice bad" role="alert">
            <div>
              <strong>附件未上传</strong>
              <p>{{ uploadError }}</p>
            </div>
          </div>
        </aside>
      </section>
    </main>
  </template>

  <main v-else class="ended-shell">
    <div class="brand">
      <span class="brand-mark">M</span><span>会议消息</span>
    </div>
    <section class="card">
      <span class="status">访客入口已停止</span>
      <h1>本场会议已结束</h1>
      <p class="muted">
        MeetSieve 已停止局域网访问。此页面不能继续查看消息、下载资料或上传附件。
      </p>
      <div class="notice">
        <div>
          <strong>已完成的内容保存在主持人的会议工作目录中</strong>
          <p>未完成的上传已取消，不会留下正式附件。</p>
        </div>
      </div>
      <p v-if="errorMessage" class="help">{{ errorMessage }}</p>
      <button class="btn" type="button" @click="reloadPage">
        检查会议状态
      </button>
    </section>
  </main>
</template>
