// Package models 提供与 Step 1 SQLite migration 对齐的显式 GORM 映射。
package models

import "gorm.io/gorm"

// AppMetadata 映射 app_metadata typed singleton。
type AppMetadata struct {
	ID                    string `gorm:"column:id"`
	SingletonKey          int    `gorm:"column:singleton_key"`
	Product               string `gorm:"column:product"`
	DatabaseID            string `gorm:"column:database_id"`
	DeviceCode            string `gorm:"column:device_code"`
	CreatedWithAppVersion string `gorm:"column:created_with_app_version"`
	CreatedAt             int64  `gorm:"column:created_at"`
	UpdatedAt             int64  `gorm:"column:updated_at"`
}

// TableName 返回 AppMetadata 的显式数据库表名。
func (AppMetadata) TableName() string { return "app_metadata" }

// Settings 映射 settings typed singleton。
type Settings struct {
	ID                     string  `gorm:"column:id"`
	SingletonKey           int     `gorm:"column:singleton_key"`
	VolcAuthMode           *string `gorm:"column:volc_auth_mode"`
	VolcAPIAppKey          *string `gorm:"column:volc_api_app_key"`
	VolcAPIAccessKey       *string `gorm:"column:volc_api_access_key"`
	VolcAPIKey             *string `gorm:"column:volc_api_key"`
	DefaultMicrophoneID    *string `gorm:"column:default_microphone_id"`
	WakeWord               string  `gorm:"column:wake_word"`
	MinutePrompt           *string `gorm:"column:minute_prompt"`
	CodexExecutablePath    *string `gorm:"column:codex_executable_path"`
	CodexProxyPort         *int    `gorm:"column:codex_proxy_port"`
	CodexAvailabilityState string  `gorm:"column:codex_availability_state"`
	CodexVersion           string  `gorm:"column:codex_version"`
	CodexAccountState      string  `gorm:"column:codex_account_state"`
	CodexProtocolState     string  `gorm:"column:codex_protocol_state"`
	CodexProbeMessage      string  `gorm:"column:codex_probe_message"`
	CodexProbedAt          *int64  `gorm:"column:codex_probed_at"`
	CreatedAt              int64   `gorm:"column:created_at"`
	UpdatedAt              int64   `gorm:"column:updated_at"`
}

// TableName 返回 Settings 的显式数据库表名。
func (Settings) TableName() string { return "settings" }

// BeforeCreate 补齐新增检测列的稳定初始值，兼容显式创建 Settings 的测试与初始化流程。
func (settings *Settings) BeforeCreate(_ *gorm.DB) error {
	if settings.CodexAvailabilityState == "" {
		settings.CodexAvailabilityState = "unchecked"
	}
	if settings.CodexAccountState == "" {
		settings.CodexAccountState = "unknown"
	}
	if settings.CodexProtocolState == "" {
		settings.CodexProtocolState = "unchecked"
	}
	if settings.CodexProbeMessage == "" {
		settings.CodexProbeMessage = "尚未检测"
	}
	return nil
}
