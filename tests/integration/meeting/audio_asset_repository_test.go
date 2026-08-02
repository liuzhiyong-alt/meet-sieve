package meeting_test

import (
	"context"
	"errors"
	"testing"

	meetingrepository "meet-sieve/internal/repository/meeting"
	"meet-sieve/models"
)

// TestRepositoryCreatesReadyAudioAssetIdempotently 验证完成文件元数据可幂等登记且不覆盖冲突事实。
func TestRepositoryCreatesReadyAudioAssetIdempotently(t *testing.T) {
	db := openMeetingDatabase(t)
	repository := meetingrepository.NewRepository(db)
	meeting := insertPreparingMeeting(t, db, "11111111-1111-4111-8111-111111111111", "20260801-ABCD-01")
	asset := models.AudioAsset{
		ID: "22222222-2222-4222-8222-222222222222", MeetingID: meeting.ID,
		Kind: "microphone", SequenceNo: 1, RelativePath: "meetings/20260801-ABCD-01/audio/segments/000001.wav",
		StartSample: 0, EndSample: 16000, SampleRate: 16000, BitDepth: 16, Channels: 1,
		SizeBytes: 32044, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		State: "ready", CreatedAt: 10, UpdatedAt: 10,
	}

	if err := repository.CreateReadyAudioAsset(context.Background(), asset); err != nil {
		t.Fatalf("登记完成音频资产失败：%v", err)
	}
	if err := repository.CreateReadyAudioAsset(context.Background(), asset); err != nil {
		t.Fatalf("相同音频资产重复登记应幂等：%v", err)
	}
	asset.SHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := repository.CreateReadyAudioAsset(context.Background(), asset); !errors.Is(err, meetingrepository.ErrAudioAssetConflict) {
		t.Fatalf("不一致的重复资产必须拒绝覆盖：%v", err)
	}

	assets, err := repository.ListReadyMicrophoneAssets(context.Background(), meeting.ID)
	if err != nil || len(assets) != 1 || assets[0].EndSample != 16000 {
		t.Fatalf("读取分片资产不正确：assets=%+v err=%v", assets, err)
	}
}
