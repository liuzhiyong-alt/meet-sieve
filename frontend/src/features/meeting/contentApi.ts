export interface TimelineEntry {
  seq: number
  kind: string
  occurred_at: number
  source: string
  entity_id?: string
  display_name?: string
  text?: string
  content_format?: 'plain' | 'markdown'
  speaker_key?: string
  speaker_label?: string
  start_sample?: number
  end_sample?: number
  state?: string
  reason?: string
  resource_kind?: string
  original_name?: string
  media_type?: string
  size_bytes?: number
  sha256?: string
  url?: string
  description?: string
}

export interface TimelinePage {
  entries: TimelineEntry[]
  oldest_seq: number
  latest_seq: number
  has_older: boolean
  has_more_after: boolean
}

export interface LiveMeetingStatus {
  started_at?: number
  ended_at?: number
  recording_state: string
  microphone_state: string
  local_save_state: string
  realtime_asr_state: string
  latest_final_at?: number
  agent_state: string
  lan_state: string
  online_count: number
}

export interface BindingResult<T> {
  code: number
  message: string
  data?: T
}

interface ContentBindingRuntime {
  GetMeetingTimeline(query: {
    meeting_id: string
    direction: string
    cursor_seq: number
    limit: number
  }): Promise<BindingResult<TimelinePage>>
  SendMeetingMessage(input: {
    meeting_id: string
    request_id: string
    content: string
  }): Promise<BindingResult<TimelineEntry>>
  ChooseAndSendMeetingAttachment(input: {
    meeting_id: string
  }): Promise<BindingResult<{ cancelled: boolean; seq?: number }>>
  GetLiveMeetingStatus(
    meetingID: string,
  ): Promise<BindingResult<LiveMeetingStatus>>
}

/** contentBinding 读取 Wails 运行时注册的统一内容 Binding。 */
function contentBinding(): ContentBindingRuntime {
  const runtimeWindow = window as typeof window & {
    go?: { wails?: { ContentBinding?: ContentBindingRuntime } }
  }
  const binding = runtimeWindow.go?.wails?.ContentBinding
  if (!binding) throw new Error('会中内容服务尚未准备')
  return binding
}

/** getMeetingTimeline 按统一 seq 游标读取持久事件。 */
export function getMeetingTimeline(
  meetingID: string,
  direction: 'latest' | 'before' | 'after',
  cursorSeq = 0,
  limit = 100,
): Promise<BindingResult<TimelinePage>> {
  return contentBinding().GetMeetingTimeline({
    meeting_id: meetingID,
    direction,
    cursor_seq: cursorSeq,
    limit,
  })
}

/** sendMeetingMessage 幂等提交主持人 Markdown 消息。 */
export function sendMeetingMessage(
  meetingID: string,
  requestID: string,
  content: string,
): Promise<BindingResult<TimelineEntry>> {
  return contentBinding().SendMeetingMessage({
    meeting_id: meetingID,
    request_id: requestID,
    content,
  })
}

/** chooseAndSendMeetingAttachment 打开系统文件窗口，确认后直接发送。 */
export function chooseAndSendMeetingAttachment(
  meetingID: string,
): Promise<BindingResult<{ cancelled: boolean; seq?: number }>> {
  return contentBinding().ChooseAndSendMeetingAttachment({
    meeting_id: meetingID,
  })
}

/** getLiveMeetingStatus 读取右侧状态卡的真实聚合状态。 */
export function getLiveMeetingStatus(
  meetingID: string,
): Promise<BindingResult<LiveMeetingStatus>> {
  return contentBinding().GetLiveMeetingStatus(meetingID)
}
