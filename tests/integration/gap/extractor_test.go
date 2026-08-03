package gap_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	domainaudio "meet-sieve/internal/domain/audio"
	"meet-sieve/internal/infra/database"
	gaprepository "meet-sieve/internal/repository/gap"
	gapservice "meet-sieve/internal/service/gap"
	"meet-sieve/models"
)

// TestExtractor_UsesVerifiedMixedAndWritesAtomicGapWAV 验证提取器按样本切片并登记真实哈希。
func TestExtractor_UsesVerifiedMixedAndWritesAtomicGapWAV(t *testing.T) {
	db := openGapDatabase(t)
	repository := gaprepository.NewRepository(db, database.NewTransactionManager(db))
	root := t.TempDir()
	meetingDirectory := filepath.Join(root, "meetings", "gap")
	if err := os.MkdirAll(filepath.Join(meetingDirectory, "audio"), 0o700); err != nil {
		t.Fatal(err)
	}
	pcm := make([]byte, 16000*2)
	for index := 0; index < 16000; index++ {
		binary.LittleEndian.PutUint16(pcm[index*2:index*2+2], uint16(int16(index)))
	}
	header, _ := domainaudio.EncodeCanonicalWAVHeader(16000)
	wav := append(header, pcm...)
	path := filepath.Join(meetingDirectory, "audio", "recording.wav")
	if err := os.WriteFile(path, wav, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(wav)
	asset := models.AudioAsset{ID: "61616161-6161-4616-8616-616161616161", MeetingID: testMeetingID, Kind: "mixed", SequenceNo: 1, RelativePath: "meetings/gap/audio/recording.wav", StartSample: 0, EndSample: 16000, SampleRate: 16000, BitDepth: 16, Channels: 1, SizeBytes: int64(len(wav)), SHA256: hex.EncodeToString(digest[:]), State: "ready", CreatedAt: 1, UpdatedAt: 1}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	extractor := gapservice.NewExtractor(repository, root)
	extracted, extractedPath, err := extractor.Extract(context.Background(), testMeetingID, "62626262-6262-4626-8626-626262626262", 1000, 3000, 2)
	if err != nil {
		t.Fatalf("提取 gap WAV：%v", err)
	}
	data, err := os.ReadFile(extractedPath)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := domainaudio.DecodeCanonicalWAV(data)
	if err != nil || len(decoded) != 4000 || extracted.SHA256 == "" {
		t.Fatalf("gap WAV 范围或哈希错误：bytes=%d asset=%#v err=%v", len(decoded), extracted, err)
	}
	if _, err := os.Stat(extractedPath + ".part"); !os.IsNotExist(err) {
		t.Fatalf("提交后不应残留 part：%v", err)
	}
}
