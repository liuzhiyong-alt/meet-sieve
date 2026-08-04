package query_test

import (
	"testing"

	querydomain "meet-sieve/internal/domain/query"
)

// TestCursor_RoundTripsAndRejectsChangedFilter 验证游标不透明编码和筛选绑定。
func TestCursor_RoundTripsAndRejectsChangedFilter(t *testing.T) {
	filter := querydomain.MeetingFilter{Search: "  周会  ", Status: "gap_pending"}
	cursor := querydomain.Cursor{Version: 1, Direction: querydomain.DirectionNext, StartedAt: 1785720600000, MeetingNo: "20260803-A7K2-08"}
	encoded, err := querydomain.EncodeCursor(cursor, filter)
	if err != nil {
		t.Fatalf("编码游标失败：%v", err)
	}
	decoded, err := querydomain.DecodeCursor(encoded, filter)
	if err != nil {
		t.Fatalf("解码游标失败：%v", err)
	}
	if decoded.StartedAt != cursor.StartedAt || decoded.MeetingNo != cursor.MeetingNo || decoded.Direction != cursor.Direction {
		t.Fatalf("游标往返不一致：got=%+v want=%+v", decoded, cursor)
	}
	if _, err := querydomain.DecodeCursor(encoded, querydomain.MeetingFilter{Search: "周会", Status: "saved"}); err == nil {
		t.Fatal("筛选变化后旧游标必须失效")
	}
}

// TestCursor_RejectsTrailingJSONAndUnknownStatus 验证严格 JSON 和固定状态枚举。
func TestCursor_RejectsTrailingJSONAndUnknownStatus(t *testing.T) {
	if _, err := querydomain.NormalizeFilter(querydomain.MeetingFilter{Status: "future_state"}); err == nil {
		t.Fatal("未知状态筛选必须在查询前拒绝")
	}
	// e30e30 是两个连续空 JSON 对象的 RawURL Base64，不能被当作单个游标接受。
	if _, err := querydomain.DecodeCursor("e317e30", querydomain.MeetingFilter{}); err == nil {
		t.Fatal("尾随 JSON 必须被拒绝")
	}
}

// TestCursor_RejectsMalformedBoundaries 验证版本、方向和排序边界不能被猜测。
func TestCursor_RejectsMalformedBoundaries(t *testing.T) {
	filter := querydomain.MeetingFilter{}
	for _, cursor := range []querydomain.Cursor{
		{Version: 2, Direction: querydomain.DirectionNext, StartedAt: 1, MeetingNo: "M"},
		{Version: 1, Direction: "sideways", StartedAt: 1, MeetingNo: "M"},
		{Version: 1, Direction: querydomain.DirectionNext, StartedAt: -1, MeetingNo: "M"},
		{Version: 1, Direction: querydomain.DirectionNext, StartedAt: 1, MeetingNo: ""},
	} {
		if _, err := querydomain.EncodeCursor(cursor, filter); err == nil {
			t.Fatalf("非法游标必须被拒绝：%+v", cursor)
		}
	}
	if _, err := querydomain.DecodeCursor("not-base64", filter); err == nil {
		t.Fatal("损坏游标必须被拒绝")
	}
}
