package migration

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"meet-sieve/internal/domain/metadata"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"

	"github.com/google/uuid"
)

// ErrFinalizationTargetForbidden 表示 finalizer 被错误地用于已安装的正式数据库。
var ErrFinalizationTargetForbidden = errors.New("foundation finalization target forbidden")

// FinalizationTarget 限制动态 singleton 只在尚未安装的数据库副本中生成。
type FinalizationTarget string

const (
	// FinalizationTargetNewDatabase 表示首次初始化创建的临时数据库。
	FinalizationTargetNewDatabase FinalizationTarget = "new_database"
	// FinalizationTargetStaging 表示升级过程创建的 staging 副本。
	FinalizationTargetStaging FinalizationTarget = "staging"
	// FinalizationTargetInstalled 表示已安装的正式 meetings.db，必须拒绝。
	FinalizationTargetInstalled FinalizationTarget = "installed"
)

// DeviceCodeGenerator 定义 finalizer 所需的四位设备码生成边界。
type DeviceCodeGenerator interface {
	New() (metadata.DeviceCode, error)
}

// FoundationFinalizer 在独立 SQLite 事务中完成动态 singleton 初始化。
type FoundationFinalizer struct {
	idGenerator         identity.Generator
	deviceCodeGenerator DeviceCodeGenerator
	clock               clock.Clock
	appVersion          string
}

// NewFoundationFinalizer 创建 FoundationFinalizer；依赖将在执行时校验以便统一返回错误。
func NewFoundationFinalizer(
	idGenerator identity.Generator,
	deviceCodeGenerator DeviceCodeGenerator,
	currentClock clock.Clock,
	appVersion string,
) *FoundationFinalizer {
	return &FoundationFinalizer{
		idGenerator:         idGenerator,
		deviceCodeGenerator: deviceCodeGenerator,
		clock:               currentClock,
		appVersion:          appVersion,
	}
}

// Finalize 原子完成待处理的动态 singleton 与版本化 legacy 投影，并返回已验证身份。
func (finalizer *FoundationFinalizer) Finalize(db *sql.DB, target FinalizationTarget) (database.TypedIdentity, error) {
	if err := finalizer.validateTarget(db, target); err != nil {
		return database.TypedIdentity{}, err
	}
	metadataLegacy, err := hasTable(db, "app_metadata_legacy")
	if err != nil {
		return database.TypedIdentity{}, err
	}
	step5Legacy, err := hasTable(db, step5UtteranceLegacyTable)
	if err != nil {
		return database.TypedIdentity{}, err
	}
	if !metadataLegacy && !step5Legacy {
		return database.ReadTypedIdentity(db)
	}
	if err := finalizer.finalizePendingDatabase(db, metadataLegacy, step5Legacy); err != nil {
		return database.TypedIdentity{}, err
	}
	return database.ReadTypedIdentity(db)
}

// validateTarget 防止正式数据库在运行期被原地补写或修复。
func (finalizer *FoundationFinalizer) validateTarget(db *sql.DB, target FinalizationTarget) error {
	if db == nil {
		return fmt.Errorf("finalize 数据库不能为空")
	}
	if target != FinalizationTargetNewDatabase && target != FinalizationTargetStaging {
		return fmt.Errorf("%w: target=%s", ErrFinalizationTargetForbidden, target)
	}
	if finalizer == nil || finalizer.idGenerator == nil || finalizer.deviceCodeGenerator == nil || finalizer.clock == nil {
		return fmt.Errorf("finalizer 依赖不完整")
	}
	if strings.TrimSpace(finalizer.appVersion) == "" {
		return fmt.Errorf("finalizer 应用版本不能为空")
	}
	return nil
}

// hasTable 检查 migration 是否留下需要 finalizer 处理的 staging 表。
func hasTable(db *sql.DB, table string) (bool, error) {
	var count int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count); err != nil {
		return false, fmt.Errorf("检查 legacy 表失败：%w", err)
	}
	return count == 1, nil
}

// finalizePendingDatabase 在同一事务中处理所有 staging，任一映射失败都不留下部分投影。
func (finalizer *FoundationFinalizer) finalizePendingDatabase(db *sql.DB, metadataLegacy bool, step5Legacy bool) (err error) {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("开始 finalizer 事务失败：%w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if metadataLegacy {
		if err = finalizer.finalizeMetadata(tx); err != nil {
			return err
		}
	}
	if step5Legacy {
		if err = finalizeStep5Legacy(tx); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("提交 finalizer 事务失败：%w", err)
	}
	return nil
}

// finalizeMetadata 生成并写入 typed singleton，再移除 Step 0 占位表。
func (finalizer *FoundationFinalizer) finalizeMetadata(tx *sql.Tx) error {
	metadataID, databaseID, deviceCode, err := finalizer.generateMetadataValues()
	if err != nil {
		return err
	}
	now := finalizer.clock.Now().UnixMilli()
	if err := insertMetadata(tx, metadataID, databaseID, deviceCode, finalizer.appVersion, now); err != nil {
		return err
	}
	settingsID := finalizer.idGenerator.New()
	if !isUUIDv4(settingsID) {
		return fmt.Errorf("生成 settings UUID v4 失败")
	}
	if err := insertDefaultSettings(tx, settingsID, now); err != nil {
		return err
	}
	return deleteLegacyMetadata(tx)
}

// generateMetadataValues 生成并校验两个 UUID v4 与四位设备码。
func (finalizer *FoundationFinalizer) generateMetadataValues() (string, string, metadata.DeviceCode, error) {
	metadataID := finalizer.idGenerator.New()
	databaseID := finalizer.idGenerator.New()
	if !isUUIDv4(metadataID) || !isUUIDv4(databaseID) {
		return "", "", "", fmt.Errorf("生成 metadata UUID v4 失败")
	}
	deviceCode, err := finalizer.deviceCodeGenerator.New()
	if err != nil {
		return "", "", "", fmt.Errorf("生成设备码失败：%w", err)
	}
	if _, err := metadata.ParseDeviceCode(deviceCode.String()); err != nil {
		return "", "", "", fmt.Errorf("校验设备码失败：%w", err)
	}
	return metadataID, databaseID, deviceCode, nil
}

// insertMetadata 写入 typed metadata singleton，不解释旧 key/value。
func insertMetadata(tx *sql.Tx, metadataID string, databaseID string, deviceCode metadata.DeviceCode, appVersion string, now int64) error {
	_, err := tx.Exec(`INSERT INTO app_metadata (
		id, singleton_key, product, database_id, device_code, created_with_app_version, created_at, updated_at
	) VALUES (?, 1, 'meet-sieve', ?, ?, ?, ?, ?)`, metadataID, databaseID, deviceCode.String(), appVersion, now, now)
	if err != nil {
		return fmt.Errorf("写入 app_metadata 失败：%w", err)
	}
	return nil
}

// insertDefaultSettings 写入 settings singleton，并保留 SQLite 声明的默认唤醒词。
func insertDefaultSettings(tx *sql.Tx, settingsID string, now int64) error {
	_, err := tx.Exec("INSERT INTO settings (id, singleton_key, created_at, updated_at) VALUES (?, 1, ?, ?)", settingsID, now, now)
	if err != nil {
		return fmt.Errorf("写入 settings 失败：%w", err)
	}
	return nil
}

// deleteLegacyMetadata 移除不再可解释的 Step 0 key/value 占位表。
func deleteLegacyMetadata(tx *sql.Tx) error {
	if _, err := tx.Exec("DROP TABLE app_metadata_legacy"); err != nil {
		return fmt.Errorf("删除 legacy app_metadata 失败：%w", err)
	}
	return nil
}

// isUUIDv4 确保 finalizer 只接受 UUID v4 作为业务主键和数据库身份。
func isUUIDv4(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 4
}
