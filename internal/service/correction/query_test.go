package correction

import (
	"testing"

	correctionrepository "meet-sieve/internal/repository/correction"
)

// TestMapEntries_PreservesClusterMapping 验证列表投影携带本场未知说话人的稳定编号和当前对应人。
func TestMapEntries_PreservesClusterMapping(t *testing.T) {
	entries := mapEntries([]correctionrepository.EntryRow{{
		Seq:                  1,
		UtteranceID:          "utterance-1",
		SpeakerClusterID:     "cluster-1",
		ClusterDisplayNo:     2,
		ClusterParticipantID: "participant-1",
		ClusterRevision:      3,
		ClusterCount:         4,
	}}, nil)
	if len(entries) != 1 {
		t.Fatalf("期望一条 entry，实际得到 %d 条", len(entries))
	}
	entry := entries[0]
	if entry.ClusterDisplayNo != 2 || entry.ClusterParticipantID != "participant-1" {
		t.Fatalf("cluster 投影不正确：%+v", entry)
	}
	if entry.SpeakerDisplay != "未知说话人 2" {
		t.Fatalf("未知说话人显示不正确：%s", entry.SpeakerDisplay)
	}
}

// TestMapEntries_UsesTrackNumberBeforeAutomaticMatching 验证 profile 未就绪时仍区分匿名说话人。
func TestMapEntries_UsesTrackNumberBeforeAutomaticMatching(t *testing.T) {
	entries := mapEntries([]correctionrepository.EntryRow{{
		Seq: 1, UtteranceID: "utterance-1", TrackDisplayNo: 3,
	}}, nil)
	if len(entries) != 1 || entries[0].SpeakerDisplay != "说话人 3" {
		t.Fatalf("待识别 track 显示不正确：%+v", entries)
	}
}
