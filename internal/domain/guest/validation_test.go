package guest

import (
	"strings"
	"testing"
)

// TestNormalizeDisplayName 验证显示名称的 Unicode 长度、空白和不可见字符边界。
func TestNormalizeDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "trim", input: "  王小明\t", want: "王小明"},
		{name: "forty code points", input: strings.Repeat("会", 40), want: strings.Repeat("会", 40)},
		{name: "empty", input: " \n\t ", wantErr: true},
		{name: "over limit", input: strings.Repeat("会", 41), wantErr: true},
		{name: "control", input: "王\x00小明", wantErr: true},
		{name: "bidi override", input: "王\u202e小明", wantErr: true},
		{name: "invalid utf8", input: string([]byte{0xff}), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeDisplayName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeDisplayName() error=%v, wantErr=%v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("NormalizeDisplayName()=%q, want %q", got, tt.want)
			}
		})
	}
}

// TestNormalizeMessage 验证消息按 UTF-8 bytes 限制并只规范化换行。
func TestNormalizeMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "normalize newlines", input: " 第一行\r\n第二行\r ", want: " 第一行\n第二行\n "},
		{name: "exact bytes", input: strings.Repeat("a", MaxMessageBytes), want: strings.Repeat("a", MaxMessageBytes)},
		{name: "over bytes", input: strings.Repeat("a", MaxMessageBytes+1), wantErr: true},
		{name: "unicode over bytes", input: strings.Repeat("会", MaxMessageBytes/3+1), wantErr: true},
		{name: "blank", input: " \n\t", wantErr: true},
		{name: "nul", input: "消息\x00末尾", wantErr: true},
		{name: "invalid utf8", input: string([]byte{0xff}), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeMessage(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeMessage() error=%v, wantErr=%v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("NormalizeMessage() length=%d, want length=%d", len(got), len(tt.want))
			}
		})
	}
}

// TestNormalizeLink 验证链接只接受无 userinfo 的绝对 HTTP/HTTPS URL。
func TestNormalizeLink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "https", input: " https://example.com/design?q=1#part ", want: "https://example.com/design?q=1#part"},
		{name: "http", input: "http://192.168.1.2/path", want: "http://192.168.1.2/path"},
		{name: "userinfo", input: "https://user:pass@example.com", wantErr: true},
		{name: "relative", input: "/design", wantErr: true},
		{name: "unsupported scheme", input: "file:///tmp/a", wantErr: true},
		{name: "missing host", input: "https:///path", wantErr: true},
		{name: "control", input: "https://example.com/\nsecret", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeLink(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeLink() error=%v, wantErr=%v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("NormalizeLink()=%q, want %q", got, tt.want)
			}
		})
	}
}

// TestValidateRequestID 验证客户端幂等键必须是标准 UUID。
func TestValidateRequestID(t *testing.T) {
	t.Parallel()

	if err := ValidateRequestID("9d4f6920-266a-4ab9-b4ae-76f4330a4f12"); err != nil {
		t.Fatalf("合法 UUID 被拒绝：%v", err)
	}
	for _, invalid := range []string{"", "request-1", "9d4f6920266a4ab9b4ae76f4330a4f12"} {
		if err := ValidateRequestID(invalid); err == nil {
			t.Fatalf("非法 request ID 被接受：%q", invalid)
		}
	}
}
