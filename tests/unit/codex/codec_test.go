package codex_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"meet-sieve/internal/adapter/agent/codex"
)

// TestCodec_WritesOneJSONRequestPerLine 验证 JSONL 编码器生成单行 initialize 请求。
func TestCodec_WritesOneJSONRequestPerLine(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	codec := codex.NewCodec(nil, &output)
	request := codex.Request{
		ID:     1,
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
	if decoded.ID != 1 || decoded.Method != "initialize" {
		t.Fatalf("请求字段不正确：%+v", decoded)
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
	if response.ID == nil || *response.ID != 1 || len(response.Result) == 0 {
		t.Fatalf("响应识别错误：%+v", response)
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
