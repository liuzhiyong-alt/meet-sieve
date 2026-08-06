package minutes

import (
	"encoding/json"
	"sort"
)

// OutputSchema 返回纪要 v1 的严格 JSON Schema。
func OutputSchema() ([]byte, error) {
	return buildOutputSchema(nil, false)
}

// OutputSchemaForResources 返回只允许引用本次资料白名单的纪要 Schema。
func OutputSchemaForResources(resourceSeq map[int64]struct{}) ([]byte, error) {
	return buildOutputSchema(resourceSeq, true)
}

// buildOutputSchema 构造纪要 Schema；运行时可进一步收紧资料引用范围。
func buildOutputSchema(resourceSeq map[int64]struct{}, restrictResources bool) ([]byte, error) {
	// Codex response format 不支持 uniqueItems，来源去重由本地 validSources 继续强制校验。
	sequenceList := map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "integer", "minimum": 1}}
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
	reference := buildReferenceSchema(resourceSeq, restrictResources)
	gap := objectSchema([]string{"start_sample", "end_sample", "state"}, map[string]any{
		"start_sample": map[string]any{"type": "integer", "minimum": 0}, "end_sample": map[string]any{"type": "integer", "minimum": 1}, "state": map[string]any{"type": "string", "enum": []string{"pending", "processing", "failed", "conflict"}},
	})
	references := arraySchema(reference)
	if restrictResources && len(resourceSeq) == 0 {
		// 没有已完成资料时从 provider 边界强制返回空数组。
		references["maxItems"] = 0
	}
	return json.Marshal(objectSchema([]string{"v", "conclusions", "topics", "tasks", "references", "gap_notice"}, map[string]any{
		"v":           map[string]any{"type": "integer", "enum": []int{1}},
		"conclusions": arraySchema(conclusion), "topics": arraySchema(topic), "tasks": arraySchema(task),
		"references": references, "gap_notice": arraySchema(gap),
	}))
}

// buildReferenceSchema 构造资料引用，并在运行时限定为真实 resource 事件序号。
func buildReferenceSchema(resourceSeq map[int64]struct{}, restrictResources bool) map[string]any {
	sequence := map[string]any{"type": "integer", "minimum": 1}
	if restrictResources && len(resourceSeq) > 0 {
		allowed := make([]int64, 0, len(resourceSeq))
		for value := range resourceSeq {
			allowed = append(allowed, value)
		}
		sort.Slice(allowed, func(left int, right int) bool { return allowed[left] < allowed[right] })
		sequence["enum"] = allowed
	}
	return objectSchema([]string{"label", "resource_event_seq"}, map[string]any{
		"label": map[string]any{"type": "string", "minLength": 1}, "resource_event_seq": sequence,
	})
}

// objectSchema 构造禁止未知字段的对象定义。
func objectSchema(required []string, properties map[string]any) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": required, "properties": properties}
}

// arraySchema 构造有界数组定义，避免 provider 返回无界结构。
func arraySchema(item map[string]any) map[string]any {
	return map[string]any{"type": "array", "maxItems": 1000, "items": item}
}
