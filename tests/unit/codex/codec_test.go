package codex_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"meet-sieve/internal/adapter/agent/codex"
)

// TestCodec_WritesOneJSONRequestPerLine 验证 JSONL 编码器生成单行 initialize 请求。
func TestCodec_WritesOneJSONRequestPerLine(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	codec := codex.NewCodec(nil, &output)
	requestID := codex.IntRequestID(1)
	request := codex.Request{
		ID:     &requestID,
		Method: "initialize",
		Params: map[string]any{"clientInfo": map[string]string{"name": "meetsieve", "version": "dev"}},
	}
	if err := codec.Write(request); err != nil {
		t.Fatalf("写入请求失败：%v", err)
	}
	if bytes.Count(output.Bytes(), []byte{'\n'}) != 1 {
		t.Fatalf("JSONL 请求不是单行：%q", output.String())
	}

	var decoded codex.Request
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &decoded); err != nil {
		t.Fatalf("解码请求失败：%v", err)
	}
	if decoded.ID == nil || !decoded.ID.Equal(requestID) || decoded.Method != "initialize" {
		t.Fatalf("请求字段不正确：%+v", decoded)
	}
}

// TestCodec_ConcurrentWritesRemainWholeLines 验证并发 RPC 不会交错破坏 JSONL envelope。
func TestCodec_ConcurrentWritesRemainWholeLines(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	codec := codex.NewCodec(nil, &output)
	var waitGroup sync.WaitGroup
	for index := range 64 {
		waitGroup.Add(1)
		go func(value int) {
			defer waitGroup.Done()
			requestID := codex.IntRequestID(int64(value + 1))
			if err := codec.Write(codex.Request{ID: &requestID, Method: "test/call", Params: map[string]int{"value": value}}); err != nil {
				t.Errorf("并发写入失败：%v", err)
			}
		}(index)
	}
	waitGroup.Wait()

	scanner := bufio.NewScanner(bytes.NewReader(output.Bytes()))
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		var request codex.Request
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			t.Fatalf("第 %d 行不是完整 JSON：%v", lineCount, err)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("扫描 JSONL 失败：%v", err)
	}
	if lineCount != 64 {
		t.Fatalf("JSONL 行数不正确：got %d, want %d, raw=%s", lineCount, 64, fmt.Sprintf("%d bytes", output.Len()))
	}
}

// TestCodec_ReadDistinguishesResponseAndNotification 验证通知不会被误判为请求响应。
func TestCodec_ReadDistinguishesResponseAndNotification(t *testing.T) {
	t.Parallel()

	input := bytes.NewBufferString("{\"method\":\"server/ready\"}\n{\"id\":1,\"result\":{\"userAgent\":\"codex\"}}\n")
	codec := codex.NewCodec(input, nil)

	notification, err := codec.Read()
	if err != nil {
		t.Fatalf("读取通知失败：%v", err)
	}
	if notification.Method != "server/ready" || notification.ID != nil {
		t.Fatalf("通知识别错误：%+v", notification)
	}

	response, err := codec.Read()
	if err != nil {
		t.Fatalf("读取响应失败：%v", err)
	}
	if response.ID == nil || !response.ID.Equal(codex.IntRequestID(1)) || len(response.Result) == 0 {
		t.Fatalf("响应识别错误：%+v", response)
	}
}

// TestCodec_ReadPreservesStringRequestID 验证 ServerRequest 的字符串 ID 不会丢失类型。
func TestCodec_ReadPreservesStringRequestID(t *testing.T) {
	t.Parallel()

	codec := codex.NewCodec(bytes.NewBufferString("{\"id\":\"approval-7\",\"method\":\"item/fileChange/requestApproval\"}\n"), nil)
	message, err := codec.Read()
	if err != nil {
		t.Fatalf("读取 ServerRequest 失败：%v", err)
	}
	if message.ID == nil || !message.ID.Equal(codex.StringRequestID("approval-7")) {
		t.Fatalf("字符串 request ID 未保留：%+v", message.ID)
	}
}

// TestCodec_ReadRejectsInvalidJSON 验证非法 JSON 会返回可诊断错误。
func TestCodec_ReadRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	codec := codex.NewCodec(bytes.NewBufferString("{invalid}\n"), nil)
	if _, err := codec.Read(); err == nil {
		t.Fatal("非法 JSON 应读取失败")
	}
}
