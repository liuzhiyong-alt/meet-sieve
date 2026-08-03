package asr_test

import (
	"strings"
	"testing"

	"meet-sieve/internal/adapter/asr/volcano/fileflash"
	"meet-sieve/internal/port"
)

// TestFileFlashAdapterContract 固定极速文件识别的生产入口、资源标识与业务 port。
func TestFileFlashAdapterContract(t *testing.T) {
	if !strings.HasPrefix(fileflash.Endpoint, "https://openspeech.bytedance.com/") {
		t.Fatalf("极速文件识别必须使用固定火山 HTTPS 入口：%q", fileflash.Endpoint)
	}
	if fileflash.ResourceID != "volc.bigasr.auc_turbo" {
		t.Fatalf("极速文件识别资源标识发生漂移：%q", fileflash.ResourceID)
	}
	var transcriber port.FileTranscriber = fileflash.NewDynamicAdapter(nil)
	if transcriber == nil {
		t.Fatal("极速文件识别 adapter 必须实现 FileTranscriber")
	}
}
