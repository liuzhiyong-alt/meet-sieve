package agent

import (
	"context"
	"fmt"

	domainagent "meet-sieve/internal/domain/agent"
)

// ListContextEvents 按固定 cutoff 读取全部 event header，并只 join 当前安全投影字段。
func (repository *Repository) ListContextEvents(ctx context.Context, meetingID string, afterSeq int64, cutoffSeq int64) ([]domainagent.ContextEvent, error) {
	if repository == nil || repository.reader == nil || meetingID == "" || afterSeq < 0 || cutoffSeq < afterSeq {
		return nil, fmt.Errorf("读取智能体上下文事件：参数无效")
	}
	const statement = `
SELECT event.seq, event.kind, event.occurred_at, event.source,
       COALESCE(message.display_name_snapshot, guest.display_name, '') AS display_name,
       COALESCE(utterance.current_text, corrected_utterance.current_text, message.content,
                json_extract(event.payload_json, '$.text'), '') AS text,
       COALESCE(resource.source_url, corrected_resource.source_url, '') AS url,
       COALESCE(resource.relative_path, corrected_resource.relative_path, '') AS relative_path,
       COALESCE(resource.kind, corrected_resource.kind, '') AS resource_kind,
       COALESCE(resource.state, corrected_resource.state, '') AS resource_state,
       COALESCE(resource.size_bytes, corrected_resource.size_bytes, 0) AS size_bytes,
       COALESCE(resource.sha256, corrected_resource.sha256, '') AS sha256,
       COALESCE(gap.reason, '') AS gap_reason
FROM meeting_events AS event
LEFT JOIN utterances AS utterance
  ON utterance.event_id = event.id AND utterance.meeting_id = event.meeting_id
LEFT JOIN agent_voice_command_utterances AS voice_command
  ON voice_command.utterance_id = utterance.id
LEFT JOIN messages AS message
  ON message.event_id = event.id AND message.meeting_id = event.meeting_id
LEFT JOIN resources AS resource
  ON resource.event_id = event.id AND resource.meeting_id = event.meeting_id
LEFT JOIN asr_gaps AS gap
  ON gap.event_id = event.id AND gap.meeting_id = event.meeting_id
LEFT JOIN corrections AS correction
  ON correction.id = event.entity_id AND correction.meeting_id = event.meeting_id
LEFT JOIN utterances AS corrected_utterance
  ON correction.target_kind = 'utterance' AND corrected_utterance.id = correction.target_id
LEFT JOIN agent_voice_command_utterances AS corrected_voice_command
  ON corrected_voice_command.utterance_id = corrected_utterance.id
LEFT JOIN resources AS corrected_resource
  ON correction.target_kind = 'resource' AND corrected_resource.id = correction.target_id
LEFT JOIN guest_sessions AS guest
  ON guest.id = COALESCE(resource.guest_session_id, corrected_resource.guest_session_id)
WHERE event.meeting_id = ? AND event.seq > ? AND event.seq <= ?
  AND (voice_command.id IS NULL OR voice_command.state = 'released')
  AND (corrected_voice_command.id IS NULL OR corrected_voice_command.state = 'released')
ORDER BY event.seq ASC`
	var events []domainagent.ContextEvent
	if err := repository.reader.WithContext(ctx).Raw(statement, meetingID, afterSeq, cutoffSeq).Scan(&events).Error; err != nil {
		return nil, fmt.Errorf("读取智能体上下文事件失败：%w", err)
	}
	return events, nil
}
