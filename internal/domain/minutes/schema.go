package minutes

import "encoding/json"

// OutputSchema 返回纪要 v1 的严格 JSON Schema。
func OutputSchema() ([]byte, error) {
	sequenceList := map[string]any{"type": "array", "minItems": 1, "uniqueItems": true, "items": map[string]any{"type": "integer", "minimum": 1}}
	conclusion := objectSchema([]string{"text", "source_seq"}, map[string]any{
		"text": map[string]any{"type": "string", "minLength": 1}, "source_seq": sequenceList,
	})
	topic := objectSchema([]string{"title", "content", "source_seq"}, map[string]any{
		"title": map[string]any{"type": "string", "minLength": 1}, "content": map[string]any{"type": "string", "minLength": 1}, "source_seq": sequenceList,
	})
	task := objectSchema([]string{"task", "owner", "due", "source_seq"}, map[string]any{
		"task":  map[string]any{"type": "string", "minLength": 1},
		"owner": map[string]any{"type": []string{"string", "null"}}, "due": map[string]any{"type": []string{"string", "null"}}, "source_seq": sequenceList,
	})
	reference := objectSchema([]string{"label", "resource_event_seq"}, map[string]any{
		"label": map[string]any{"type": "string", "minLength": 1}, "resource_event_seq": map[string]any{"type": "integer", "minimum": 1},
	})
	gap := objectSchema([]string{"start_sample", "end_sample", "state"}, map[string]any{
		"start_sample": map[string]any{"type": "integer", "minimum": 0}, "end_sample": map[string]any{"type": "integer", "minimum": 1}, "state": map[string]any{"type": "string", "enum": []string{"pending", "processing", "failed", "conflict"}},
	})
	return json.Marshal(objectSchema([]string{"v", "conclusions", "topics", "tasks", "references", "gap_notice"}, map[string]any{
		"v":           map[string]any{"const": 1},
		"conclusions": arraySchema(conclusion), "topics": arraySchema(topic), "tasks": arraySchema(task),
		"references": arraySchema(reference), "gap_notice": arraySchema(gap),
	}))
}

// objectSchema 构造禁止未知字段的对象定义。
func objectSchema(required []string, properties map[string]any) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": required, "properties": properties}
}

// arraySchema 构造有界数组定义，避免 provider 返回无界结构。
func arraySchema(item map[string]any) map[string]any {
	return map[string]any{"type": "array", "maxItems": 1000, "items": item}
}
