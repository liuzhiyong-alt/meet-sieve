// Package deletion 定义可恢复删除清单及其严格编解码规则。
package deletion

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

const ManifestVersion = 1

// Kind 表示删除任务的唯一业务范围。
type Kind string

const (
	KindRecording Kind = "recording"
	KindMeeting   Kind = "meeting"
)

// ItemType 表示扫描时通过 Lstat 识别的文件系统对象类型。
type ItemType string

const (
	ItemFile      ItemType = "file"
	ItemSymlink   ItemType = "symlink"
	ItemDirectory ItemType = "directory"
)

// Item 是清单中单个不可扩大的删除目标。
type Item struct {
	ID           string   `json:"id"`
	RelativePath string   `json:"relative_path"`
	Type         ItemType `json:"type"`
	SizeBytes    int64    `json:"size_bytes"`
	Known        bool     `json:"known"`
	SHA256       string   `json:"sha256,omitempty"`
}

// Manifest 是持久化到 deletion_jobs 的 v1 删除事实。
type Manifest struct {
	Version   int    `json:"version"`
	MeetingID string `json:"meeting_id"`
	MeetingNo string `json:"meeting_no"`
	Kind      Kind   `json:"kind"`
	Revision  int64  `json:"revision"`
	Items     []Item `json:"items"`
	Digest    string `json:"digest"`
}

// Encode 校验、计算稳定 digest 并输出清单 JSON。
func Encode(manifest Manifest) ([]byte, error) {
	manifest.Version = ManifestVersion
	manifest.Digest = ""
	if err := validateManifest(manifest, false); err != nil {
		return nil, err
	}
	digest, err := calculateDigest(manifest)
	if err != nil {
		return nil, err
	}
	manifest.Digest = digest
	return json.Marshal(manifest)
}

// Decode 严格读取清单并核对 digest，拒绝未知字段和尾随内容。
func Decode(data []byte) (Manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("解码删除清单: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Manifest{}, err
	}
	if err := validateManifest(manifest, true); err != nil {
		return Manifest{}, err
	}
	want := manifest.Digest
	manifest.Digest = ""
	got, err := calculateDigest(manifest)
	if err != nil || got != want {
		return Manifest{}, fmt.Errorf("删除清单 digest 不匹配")
	}
	manifest.Digest = want
	return manifest, nil
}

// validateManifest 校验清单枚举、关系、重复项和路径边界。
func validateManifest(manifest Manifest, requireDigest bool) error {
	if manifest.Version != ManifestVersion || manifest.MeetingID == "" || manifest.MeetingNo == "" || manifest.Revision < 0 {
		return fmt.Errorf("删除清单头无效")
	}
	if manifest.Kind != KindRecording && manifest.Kind != KindMeeting {
		return fmt.Errorf("删除清单类型无效")
	}
	if requireDigest && !isSHA256(manifest.Digest) {
		return fmt.Errorf("删除清单 digest 无效")
	}
	seenIDs := make(map[string]bool, len(manifest.Items))
	seenPaths := make(map[string]bool, len(manifest.Items))
	for _, item := range manifest.Items {
		if item.ID == "" || seenIDs[item.ID] || seenPaths[item.RelativePath] || item.SizeBytes < 0 {
			return fmt.Errorf("删除清单项目重复或无效")
		}
		if item.Type != ItemFile && item.Type != ItemSymlink && item.Type != ItemDirectory {
			return fmt.Errorf("删除清单项目类型无效")
		}
		if item.Type == ItemFile && !isSHA256(item.SHA256) || item.Type != ItemFile && item.SHA256 != "" {
			return fmt.Errorf("删除清单项目哈希无效")
		}
		if err := validateRelativePath(item.RelativePath); err != nil {
			return err
		}
		seenIDs[item.ID], seenPaths[item.RelativePath] = true, true
	}
	return nil
}

// validateRelativePath 拒绝根路径、绝对路径、非规范路径和路径逃逸。
func validateRelativePath(relativePath string) error {
	if relativePath == "" || relativePath == "." || filepath.IsAbs(relativePath) || strings.ContainsRune(relativePath, '\x00') {
		return fmt.Errorf("删除目标路径无效")
	}
	clean := filepath.Clean(relativePath)
	if clean != relativePath || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("删除目标路径越界")
	}
	return nil
}

// calculateDigest 对 digest 为空的规范 JSON 计算稳定 SHA-256。
func calculateDigest(manifest Manifest) (string, error) {
	data, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// isSHA256 校验小写十六进制 SHA-256。
func isSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// requireJSONEOF 确保一个 JSON 值之后只剩空白。
func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("删除清单包含尾随内容")
	}
	return nil
}

// SortForDeletion 按文件/链接优先、目录深度倒序返回执行副本。
func SortForDeletion(items []Item) []Item {
	result := append([]Item(nil), items...)
	sort.SliceStable(result, func(left int, right int) bool {
		leftDir := result[left].Type == ItemDirectory
		rightDir := result[right].Type == ItemDirectory
		if leftDir != rightDir {
			return !leftDir
		}
		if leftDir {
			leftDepth := strings.Count(result[left].RelativePath, string(filepath.Separator))
			rightDepth := strings.Count(result[right].RelativePath, string(filepath.Separator))
			if leftDepth != rightDepth {
				return leftDepth > rightDepth
			}
		}
		return result[left].RelativePath < result[right].RelativePath
	})
	return result
}
