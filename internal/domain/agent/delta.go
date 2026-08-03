package agent

import (
	"bytes"
	"encoding/json"
	"unicode/utf8"
)

const maxAnswerDeltaBuffer = MaxSnapshotBytes + MaxAnswerRunes*utf8.UTFMax + 16*1024

// AnswerDeltaParser 用顶层 JSON 字符串状态机只提取 answer 的已完整解码部分。
type AnswerDeltaParser struct {
	buffer    []byte
	published string
	disabled  bool
}

// NewAnswerDeltaParser 创建单 turn 使用的增量回答解析器。
func NewAnswerDeltaParser() *AnswerDeltaParser {
	return &AnswerDeltaParser{}
}

// Push 接收任意字节分片；结构或转义不完整时只等待，不泄漏其他字段。
func (parser *AnswerDeltaParser) Push(fragment []byte) string {
	if parser == nil || parser.disabled || len(fragment) == 0 {
		return ""
	}
	if len(parser.buffer)+len(fragment) > maxAnswerDeltaBuffer {
		parser.disabled = true
		parser.buffer = nil
		return ""
	}
	parser.buffer = append(parser.buffer, fragment...)
	start, found := findTopLevelAnswer(parser.buffer)
	if !found {
		return ""
	}
	raw := completeJSONStringPrefix(parser.buffer[start:])
	if len(raw) == 0 {
		return ""
	}
	var decoded string
	quoted := append(append([]byte{'"'}, raw...), '"')
	if json.Unmarshal(quoted, &decoded) != nil || !bytes.HasPrefix([]byte(decoded), []byte(parser.published)) {
		return ""
	}
	delta := decoded[len(parser.published):]
	parser.published = decoded
	return delta
}

// findTopLevelAnswer 扫描完整的前置字段，只接受根对象的 answer 字符串。
func findTopLevelAnswer(content []byte) (int, bool) {
	index := skipJSONSpaces(content, 0)
	if index >= len(content) || content[index] != '{' {
		return 0, false
	}
	index++
	for {
		index = skipJSONSpacesAndCommas(content, index)
		keyEnd, ok := scanJSONString(content, index)
		if !ok {
			return 0, false
		}
		var key string
		if json.Unmarshal(content[index:keyEnd], &key) != nil {
			return 0, false
		}
		index = skipJSONSpaces(content, keyEnd)
		if index >= len(content) || content[index] != ':' {
			return 0, false
		}
		index = skipJSONSpaces(content, index+1)
		if key == "answer" {
			return index + 1, index < len(content) && content[index] == '"'
		}
		valueEnd, ok := scanJSONValue(content, index)
		if !ok {
			return 0, false
		}
		index = valueEnd
	}
}

// scanJSONValue 使用标准 decoder 跳过一个完整的非 answer 值。
func scanJSONValue(content []byte, start int) (int, bool) {
	if start >= len(content) {
		return 0, false
	}
	decoder := json.NewDecoder(bytes.NewReader(content[start:]))
	var value json.RawMessage
	if decoder.Decode(&value) != nil {
		return 0, false
	}
	return start + int(decoder.InputOffset()), true
}

// scanJSONString 返回包含双引号的完整 JSON 字符串末尾。
func scanJSONString(content []byte, start int) (int, bool) {
	if start >= len(content) || content[start] != '"' {
		return 0, false
	}
	escaped := false
	for index := start + 1; index < len(content); index++ {
		if escaped {
			escaped = false
			continue
		}
		if content[index] == '\\' {
			escaped = true
		} else if content[index] == '"' {
			return index + 1, true
		}
	}
	return 0, false
}

// completeJSONStringPrefix 截取不含未完成 UTF-8 或转义序列的字符串正文。
func completeJSONStringPrefix(content []byte) []byte {
	safeEnd := 0
	for index := 0; index < len(content); {
		if content[index] == '"' {
			return content[:index]
		}
		if content[index] == '\\' {
			width := completeEscapeWidth(content[index:])
			if width == 0 {
				return content[:safeEnd]
			}
			index += width
			safeEnd = index
			continue
		}
		if content[index] < utf8.RuneSelf {
			if content[index] < 0x20 {
				return content[:safeEnd]
			}
			index++
			safeEnd = index
			continue
		}
		if !utf8.FullRune(content[index:]) {
			return content[:safeEnd]
		}
		_, width := utf8.DecodeRune(content[index:])
		if width == 1 {
			return content[:safeEnd]
		}
		index += width
		safeEnd = index
	}
	return content[:safeEnd]
}

// completeEscapeWidth 返回一个完整合法 JSON 转义的字节宽度。
func completeEscapeWidth(content []byte) int {
	if len(content) < 2 {
		return 0
	}
	if bytes.ContainsRune([]byte(`"\/bfnrt`), rune(content[1])) {
		return 2
	}
	if content[1] != 'u' || len(content) < 6 {
		return 0
	}
	for _, value := range content[2:6] {
		if !isHex(value) {
			return 0
		}
	}
	return 6
}

// isHex 判断 JSON Unicode 转义使用的十六进制字符。
func isHex(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}

// skipJSONSpaces 跳过 JSON 允许的四种空白。
func skipJSONSpaces(content []byte, index int) int {
	for index < len(content) && bytes.ContainsRune([]byte(" \t\r\n"), rune(content[index])) {
		index++
	}
	return index
}

// skipJSONSpacesAndCommas 跳过对象属性间的空白与逗号。
func skipJSONSpacesAndCommas(content []byte, index int) int {
	for {
		index = skipJSONSpaces(content, index)
		if index >= len(content) || content[index] != ',' {
			return index
		}
		index++
	}
}
