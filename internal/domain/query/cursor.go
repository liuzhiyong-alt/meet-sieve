// Package query 定义 Step 9 查询层使用的稳定值对象和状态投影。
package query

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const cursorVersion = 1

var (
	// ErrCursorInvalid 表示游标格式、版本或排序边界不可信。
	ErrCursorInvalid = errors.New("查询游标无效")
	// ErrCursorFilterChanged 表示游标不属于当前筛选条件。
	ErrCursorFilterChanged = errors.New("查询筛选已变化")
)

// Direction 表示游标相对当前页的读取方向。
type Direction string

const (
	// DirectionNext 表示读取排序边界之后的下一页。
	DirectionNext Direction = "next"
	// DirectionPrevious 表示反向读取排序边界之前的上一页。
	DirectionPrevious Direction = "previous"
)

// MeetingFilter 表示参与游标签名的会议列表筛选。
type MeetingFilter struct {
	Search string
	Status string
}

// Cursor 表示不透明游标中的唯一稳定排序边界。
type Cursor struct {
	Version    int       `json:"v"`
	Direction  Direction `json:"direction"`
	StartedAt  int64     `json:"started_at"`
	MeetingNo  string    `json:"meeting_no"`
	FilterHash string    `json:"filter_hash"`
}

// EncodeCursor 校验排序边界并返回不含 padding 的 Base64URL JSON。
func EncodeCursor(cursor Cursor, filter MeetingFilter) (string, error) {
	if err := validateCursor(cursor); err != nil {
		return "", err
	}
	cursor.FilterHash = buildFilterHash(filter)
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("编码查询游标：%w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeCursor 严格解码游标，并确认它仍属于当前规范化筛选。
func DecodeCursor(encoded string, filter MeetingFilter) (Cursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(payload) == 0 {
		return Cursor{}, ErrCursorInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor Cursor
	if err := decoder.Decode(&cursor); err != nil {
		return Cursor{}, ErrCursorInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Cursor{}, ErrCursorInvalid
	}
	if err := validateCursor(cursor); err != nil {
		return Cursor{}, err
	}
	if cursor.FilterHash != buildFilterHash(filter) {
		return Cursor{}, ErrCursorFilterChanged
	}
	return cursor, nil
}

// NormalizeFilter 去除搜索首尾空白并规范化筛选枚举。
func NormalizeFilter(filter MeetingFilter) (MeetingFilter, error) {
	filter.Search = strings.TrimSpace(filter.Search)
	filter.Status = strings.TrimSpace(strings.ToLower(filter.Status))
	if utf8.RuneCountInString(filter.Search) > 100 {
		return MeetingFilter{}, fmt.Errorf("搜索文字超过 100 个字符：%w", ErrCursorInvalid)
	}
	if !IsValidStatusFilter(filter.Status) {
		return MeetingFilter{}, fmt.Errorf("会议状态筛选无效：%w", ErrCursorInvalid)
	}
	return filter, nil
}

// validateCursor 校验游标版本、方向和唯一排序边界。
func validateCursor(cursor Cursor) error {
	if cursor.Version != cursorVersion || cursor.StartedAt < 0 || strings.TrimSpace(cursor.MeetingNo) == "" {
		return ErrCursorInvalid
	}
	if cursor.Direction != DirectionNext && cursor.Direction != DirectionPrevious {
		return ErrCursorInvalid
	}
	return nil
}

// buildFilterHash 对规范化筛选生成稳定摘要，避免在游标中暴露用户搜索原文。
func buildFilterHash(filter MeetingFilter) string {
	normalized, _ := NormalizeFilter(filter)
	digest := sha256.Sum256([]byte(normalized.Search + "\x00" + normalized.Status))
	return hex.EncodeToString(digest[:])
}
