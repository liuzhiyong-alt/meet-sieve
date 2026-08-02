package meeting

import "testing"

// TestBuildParticipantSnapshotsDeduplicatesMembers 验证真实成员按 ID 去重并保留首次出现顺序。
func TestBuildParticipantSnapshotsDeduplicatesMembers(t *testing.T) {
	t.Parallel()

	snapshots, err := BuildParticipantSnapshots([]ParticipantInput{
		{MemberID: "member-1", DisplayName: "张三"},
		{MemberID: "member-1", DisplayName: "重复张三"},
		{MemberID: "member-2", DisplayName: "李四"},
	})
	if err != nil {
		t.Fatalf("构建参会者快照失败：%v", err)
	}
	if len(snapshots) != 2 || snapshots[0].MemberID != "member-1" || snapshots[0].SortOrder != 0 || snapshots[1].MemberID != "member-2" || snapshots[1].SortOrder != 1 {
		t.Fatalf("参会者去重或顺序不正确：%+v", snapshots)
	}
}

// TestBuildParticipantSnapshotsRejectsEmptySelection 验证创建会议至少需要一位参会者。
func TestBuildParticipantSnapshotsRejectsEmptySelection(t *testing.T) {
	t.Parallel()

	if _, err := BuildParticipantSnapshots(nil); err == nil {
		t.Fatal("空参会者选择必须被拒绝")
	}
}

// TestBuildParticipantSnapshotsKeepsTemporaryParticipants 验证临时成员不按空 MemberID 去重，只生成本场快照。
func TestBuildParticipantSnapshotsKeepsTemporaryParticipants(t *testing.T) {
	t.Parallel()

	snapshots, err := BuildParticipantSnapshots([]ParticipantInput{
		{DisplayName: "  访客甲  "},
		{DisplayName: "访客乙"},
	})
	if err != nil {
		t.Fatalf("构建临时参会者快照失败：%v", err)
	}
	if len(snapshots) != 2 || snapshots[0].Kind != "temporary" || snapshots[0].DisplayName != "访客甲" || snapshots[1].SortOrder != 1 {
		t.Fatalf("临时参会者快照不正确：%+v", snapshots)
	}
}

// TestBuildParticipantSnapshotsRejectsBlankName 验证快照名称不能为空，避免写入不可展示的历史事实。
func TestBuildParticipantSnapshotsRejectsBlankName(t *testing.T) {
	t.Parallel()

	if _, err := BuildParticipantSnapshots([]ParticipantInput{{DisplayName: "  "}}); err == nil {
		t.Fatal("空白参会者名称必须被拒绝")
	}
}

// TestBuildParticipantSnapshotsRejectsNormalizedDuplicateName 验证不同写法的同名参会者不会进入同一快照。
func TestBuildParticipantSnapshotsRejectsNormalizedDuplicateName(t *testing.T) {
	t.Parallel()

	_, err := BuildParticipantSnapshots([]ParticipantInput{
		{MemberID: "member-1", DisplayName: "Alice"},
		{DisplayName: "ＡＬＩＣＥ"},
	})
	if err == nil {
		t.Fatal("规范化后同名的参会者必须被拒绝")
	}
}
