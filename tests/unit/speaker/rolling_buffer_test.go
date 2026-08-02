package speaker_test

import (
	"errors"
	"sync"
	"testing"

	speakerservice "meet-sieve/internal/service/speaker"
)

// TestRollingBuffer_KeepsBoundedGlobalSampleRange 验证覆盖后只保留最近样本且半开区间精确。
func TestRollingBuffer_KeepsBoundedGlobalSampleRange(t *testing.T) {
	buffer, err := speakerservice.NewRollingBuffer(4)
	if err != nil {
		t.Fatalf("创建 rolling buffer 失败：%v", err)
	}
	if err := buffer.Write(0, []int16{0, 1, 2}); err != nil {
		t.Fatalf("写入首批样本失败：%v", err)
	}
	if err := buffer.Write(3, []int16{3, 4, 5}); err != nil {
		t.Fatalf("写入覆盖样本失败：%v", err)
	}
	if _, ok := buffer.Read(1, 3); ok {
		t.Fatal("已被覆盖的样本不得返回")
	}
	got, ok := buffer.Read(2, 6)
	if !ok || !equalSamples(got, []int16{2, 3, 4, 5}) {
		t.Fatalf("最近样本错误：got=%v ok=%v", got, ok)
	}
	got[0] = 99
	again, _ := buffer.Read(2, 3)
	if again[0] != 2 {
		t.Fatal("读取结果不得暴露内部可变切片")
	}
}

// TestRollingBuffer_RejectsDiscontinuityAndWritesAfterClose 验证 gap 不会被补零且关闭后不再接收 PCM。
func TestRollingBuffer_RejectsDiscontinuityAndWritesAfterClose(t *testing.T) {
	buffer, _ := speakerservice.NewRollingBuffer(8)
	if err := buffer.Write(10, []int16{1, 2}); err != nil {
		t.Fatalf("写入连续样本失败：%v", err)
	}
	if err := buffer.Write(13, []int16{3}); !errors.Is(err, speakerservice.ErrRollingDiscontinuity) {
		t.Fatalf("不连续写入必须拒绝：%v", err)
	}
	buffer.Close()
	if err := buffer.Write(12, []int16{3}); !errors.Is(err, speakerservice.ErrRollingClosed) {
		t.Fatalf("关闭后写入必须拒绝：%v", err)
	}
	if got, ok := buffer.Read(10, 12); !ok || len(got) != 2 {
		t.Fatal("关闭后仍应允许后台读取已保留证据")
	}
}

// TestRollingBuffer_SerializesConcurrentReaders 验证并发读取不会观察到可变内部存储。
func TestRollingBuffer_SerializesConcurrentReaders(t *testing.T) {
	buffer, _ := speakerservice.NewRollingBuffer(64)
	if err := buffer.Write(0, make([]int16, 64)); err != nil {
		t.Fatalf("准备并发样本失败：%v", err)
	}
	var wait sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := 0; index < 100; index++ {
				if samples, ok := buffer.Read(0, 64); !ok || len(samples) != 64 {
					t.Errorf("并发读取返回不完整范围")
					return
				}
			}
		}()
	}
	wait.Wait()
}

// equalSamples 比较两个短 PCM 结果。
func equalSamples(left []int16, right []int16) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
