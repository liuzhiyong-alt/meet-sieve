package config

import (
	"fmt"
	"strings"

	"meet-sieve/configs"

	"github.com/spf13/viper"
)

const (
	// Step1WriteQueueCapacity 是单 writer 的固定有界队列容量。
	Step1WriteQueueCapacity = 256
	// MaxReadOpenConns 是本地 SQLite 读池允许的最大连接数。
	MaxReadOpenConns = 4
)

// Config 是应用启动所需的技术默认配置。
type Config struct {
	App       AppConfig       `mapstructure:"app"`
	Log       LogConfig       `mapstructure:"log"`
	Database  DatabaseConfig  `mapstructure:"database"`
	Codex     CodexConfig     `mapstructure:"codex"`
	Recording RecordingConfig `mapstructure:"recording"`
	ASR       ASRConfig       `mapstructure:"asr"`
	Runtime   RuntimeConfig   `mapstructure:"runtime"`
}

// LoadDefault 解析编译进二进制的技术默认配置。
func LoadDefault(expectedONNXVersion string) (Config, error) {
	return Parse(configs.DefaultYAML, expectedONNXVersion)
}

// AppConfig 描述应用基础信息。
type AppConfig struct {
	Name string `mapstructure:"name"`
}

// LogConfig 描述文件日志轮转参数。
type LogConfig struct {
	Level      string `mapstructure:"level"`
	MaxSizeMB  int    `mapstructure:"max_size_mb"`
	MaxBackups int    `mapstructure:"max_backups"`
	MaxAgeDays int    `mapstructure:"max_age_days"`
	Compress   bool   `mapstructure:"compress"`
}

// DatabaseConfig 描述 SQLite 单 writer、读池与锁等待限制。
type DatabaseConfig struct {
	BusyTimeoutMS      int `mapstructure:"busy_timeout_ms"`
	ReadMaxOpenConns   int `mapstructure:"read_max_open_conns"`
	ReadMaxIdleConns   int `mapstructure:"read_max_idle_conns"`
	WriteQueueCapacity int `mapstructure:"write_queue_capacity"`
}

// CodexConfig 描述本机 Codex app-server 启动参数。
type CodexConfig struct {
	Command                  string `mapstructure:"command"`
	InitializeTimeoutSeconds int    `mapstructure:"initialize_timeout_seconds"`
}

// RecordingConfig 描述 Step 3 本地安全录音的固定技术参数。
type RecordingConfig struct {
	MaxSegmentSeconds        int `mapstructure:"max_segment_seconds"`
	CheckpointSeconds        int `mapstructure:"checkpoint_seconds"`
	FirstFrameTimeoutSeconds int `mapstructure:"first_frame_timeout_seconds"`
	MinimumFreeSpaceGiB      int `mapstructure:"minimum_free_space_gib"`
}

// ASRConfig 描述 Step 4 火山实时转写的固定技术参数，不包含用户凭据。
type ASRConfig struct {
	Endpoint                   string `mapstructure:"endpoint"`
	ResourceID                 string `mapstructure:"resource_id"`
	ConnectTimeoutSeconds      int    `mapstructure:"connect_timeout_seconds"`
	PCMQueueSamples            int64  `mapstructure:"pcm_queue_samples"`
	FinalQueueCapacity         int    `mapstructure:"final_queue_capacity"`
	FinalPersistTimeoutSeconds int    `mapstructure:"final_persist_timeout_seconds"`
	TailTimeoutSeconds         int    `mapstructure:"tail_timeout_seconds"`
	ReconnectBackoffSeconds    []int  `mapstructure:"reconnect_backoff_seconds"`
}

// RuntimeConfig 描述动态运行时配置。
type RuntimeConfig struct {
	ONNX ONNXConfig `mapstructure:"onnx"`
}

// ONNXConfig 描述 ONNX Runtime 版本。
type ONNXConfig struct {
	Version string `mapstructure:"version"`
}

// Parse 严格解析 YAML 并校验运行时版本约束。
func Parse(data []byte, expectedONNXVersion string) (Config, error) {
	parser := viper.New()
	parser.SetConfigType("yaml")
	if err := parser.ReadConfig(strings.NewReader(string(data))); err != nil {
		return Config{}, fmt.Errorf("读取配置失败: %w", err)
	}

	var cfg Config
	if err := parser.UnmarshalExact(&cfg); err != nil {
		return Config{}, fmt.Errorf("配置字段不合法: %w", err)
	}
	if err := validate(cfg, expectedONNXVersion); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate 校验所有技术默认值以及 ONNX Runtime 版本约束。
func validate(cfg Config, expectedONNXVersion string) error {
	if strings.TrimSpace(cfg.App.Name) == "" {
		return fmt.Errorf("配置 app.name 不能为空")
	}
	if cfg.Log.MaxSizeMB <= 0 || cfg.Log.MaxBackups < 0 || cfg.Log.MaxAgeDays < 0 {
		return fmt.Errorf("配置 log 轮转参数不合法")
	}
	if !isValidDatabaseConfig(cfg.Database) {
		return fmt.Errorf("配置 database 连接参数不合法")
	}
	if strings.TrimSpace(cfg.Codex.Command) == "" || cfg.Codex.InitializeTimeoutSeconds <= 0 {
		return fmt.Errorf("配置 codex 参数不合法")
	}
	if !isValidRecordingConfig(cfg.Recording) {
		return fmt.Errorf("配置 recording 参数不合法")
	}
	if !isValidASRConfig(cfg.ASR) {
		return fmt.Errorf("配置 asr 参数不合法")
	}
	if cfg.Runtime.ONNX.Version != expectedONNXVersion {
		return fmt.Errorf("配置 runtime.onnx.version 与资源清单不一致")
	}
	return nil
}

// isValidASRConfig 校验 endpoint、容量、超时和退避均等于 Step 4 冻结边界。
func isValidASRConfig(cfg ASRConfig) bool {
	wantBackoff := []int{1, 2, 4, 8, 15}
	if strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.ResourceID) == "" || cfg.ConnectTimeoutSeconds != 10 || cfg.PCMQueueSamples != 240000 || cfg.FinalQueueCapacity != 128 || cfg.FinalPersistTimeoutSeconds != 5 || cfg.TailTimeoutSeconds != 15 || len(cfg.ReconnectBackoffSeconds) != len(wantBackoff) {
		return false
	}
	for index, value := range wantBackoff {
		if cfg.ReconnectBackoffSeconds[index] != value {
			return false
		}
	}
	return true
}

// isValidRecordingConfig 校验已确认的 Step 3 分片、检查点、首帧和磁盘空间门槛。
func isValidRecordingConfig(cfg RecordingConfig) bool {
	return cfg.MaxSegmentSeconds == 60 &&
		cfg.CheckpointSeconds == 2 &&
		cfg.FirstFrameTimeoutSeconds == 5 &&
		cfg.MinimumFreeSpaceGiB == 1
}

// isValidDatabaseConfig 校验 Step 1 固定写队列和有限读池配置。
func isValidDatabaseConfig(cfg DatabaseConfig) bool {
	return cfg.BusyTimeoutMS > 0 &&
		cfg.WriteQueueCapacity == Step1WriteQueueCapacity &&
		cfg.ReadMaxOpenConns >= 1 && cfg.ReadMaxOpenConns <= MaxReadOpenConns &&
		cfg.ReadMaxIdleConns >= 1 && cfg.ReadMaxIdleConns <= cfg.ReadMaxOpenConns
}
