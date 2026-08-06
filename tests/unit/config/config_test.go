package config_test

import (
	"strings"
	"testing"

	"meet-sieve/internal/infra/config"
)

// TestParse_RejectsUnknownField 验证未知字段不会被静默忽略。
func TestParse_RejectsUnknownField(t *testing.T) {
	t.Parallel()

	_, err := config.Parse([]byte("app:\n  name: MeetSieve\nunknown: true\n"), "1.26.0")

	if err == nil {
		t.Fatal("未知字段应被拒绝")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("错误应包含字段路径：%v", err)
	}
}

// TestLoadDefault_LoadsEmbeddedConfig 验证应用只从内嵌默认配置读取技术参数。
func TestLoadDefault_LoadsEmbeddedConfig(t *testing.T) {
	t.Parallel()

	cfg, err := config.LoadDefault("1.26.0")
	if err != nil {
		t.Fatalf("加载内嵌配置失败：%v", err)
	}
	if cfg.App.Name != "MeetSieve" {
		t.Fatalf("应用名不正确：got %q", cfg.App.Name)
	}
	if cfg.Recording.MaxSegmentSeconds != 60 || cfg.Recording.CheckpointSeconds != 2 || cfg.Recording.FirstFrameTimeoutSeconds != 5 || cfg.Recording.MinimumFreeSpaceGiB != 1 {
		t.Fatalf("Step 3 录音默认配置不正确：%+v", cfg.Recording)
	}
	if cfg.ASR.ResourceID != "volc.seedasr.sauc.duration" || cfg.ASR.PCMQueueSamples != 240000 || cfg.ASR.FinalQueueCapacity != 128 || cfg.ASR.TailTimeoutSeconds != 15 {
		t.Fatalf("Step 4 ASR 默认配置不正确：%+v", cfg.ASR)
	}
}

// TestParse_ValidatesStep1DatabaseQueueAndReadPool 验证 Step 1 SQLite 队列和读池配置必须严格合法。
func TestParse_ValidatesStep1DatabaseQueueAndReadPool(t *testing.T) {
	t.Parallel()

	validConfig := `app:
  name: MeetSieve
log:
  level: info
  max_size_mb: 20
  max_backups: 10
  max_age_days: 14
  compress: true
database:
  busy_timeout_ms: 5000
  read_max_open_conns: 4
  read_max_idle_conns: 2
  write_queue_capacity: 256
codex:
  command: codex
  initialize_timeout_seconds: 10
recording:
  max_segment_seconds: 60
  checkpoint_seconds: 2
  first_frame_timeout_seconds: 5
  minimum_free_space_gib: 1
asr:
  endpoint: wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async
  resource_id: volc.seedasr.sauc.duration
  connect_timeout_seconds: 10
  pcm_queue_samples: 240000
  final_queue_capacity: 128
  final_persist_timeout_seconds: 5
  tail_timeout_seconds: 15
  reconnect_backoff_seconds: [1, 2, 4, 8, 15]
runtime:
  onnx:
    version: 1.26.0
`

	if _, err := config.Parse([]byte(validConfig), "1.26.0"); err != nil {
		t.Fatalf("合法的 Step 1 数据库配置不应失败：%v", err)
	}

	tests := []struct {
		name string
		data string
	}{
		{name: "缺少读连接上限", data: strings.Replace(validConfig, "  read_max_open_conns: 4\n", "", 1)},
		{name: "缺少空闲读连接上限", data: strings.Replace(validConfig, "  read_max_idle_conns: 2\n", "", 1)},
		{name: "缺少写队列容量", data: strings.Replace(validConfig, "  write_queue_capacity: 256\n", "", 1)},
		{name: "写队列容量不是 256", data: strings.Replace(validConfig, "write_queue_capacity: 256", "write_queue_capacity: 255", 1)},
		{name: "读连接上限为零", data: strings.Replace(validConfig, "read_max_open_conns: 4", "read_max_open_conns: 0", 1)},
		{name: "空闲读连接上限为零", data: strings.Replace(validConfig, "read_max_idle_conns: 2", "read_max_idle_conns: 0", 1)},
		{name: "读连接上限过大", data: strings.Replace(validConfig, "read_max_open_conns: 4", "read_max_open_conns: 5", 1)},
		{name: "空闲连接超过读连接上限", data: strings.Replace(validConfig, "read_max_idle_conns: 2", "read_max_idle_conns: 5", 1)},
		{name: "分片时长不允许变更", data: strings.Replace(validConfig, "max_segment_seconds: 60", "max_segment_seconds: 59", 1)},
		{name: "ASR PCM 队列不允许偏离十五秒边界", data: strings.Replace(validConfig, "pcm_queue_samples: 240000", "pcm_queue_samples: 32000", 1)},
		{name: "ASR 重连退避不允许变更", data: strings.Replace(validConfig, "reconnect_backoff_seconds: [1, 2, 4, 8, 15]", "reconnect_backoff_seconds: [1, 2, 4]", 1)},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if _, err := config.Parse([]byte(test.data), "1.26.0"); err == nil {
				t.Fatal("非法 Step 1 数据库配置应被拒绝")
			}
		})
	}
}
