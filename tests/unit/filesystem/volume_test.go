package filesystem_test

import (
	"testing"

	"meet-sieve/internal/infra/filesystem"
)

// TestClassifyFilesystemType_FailsClosedForUnknownVolume 验证常见网络卷被识别，未知文件系统不被默认当成本地卷。
func TestClassifyFilesystemType_FailsClosedForUnknownVolume(t *testing.T) {
	tests := []struct {
		filesystemType string
		want           filesystem.VolumeKind
	}{
		{filesystemType: "apfs", want: filesystem.VolumeLocal},
		{filesystemType: "hfs", want: filesystem.VolumeLocal},
		{filesystemType: "exfat", want: filesystem.VolumeLocal},
		{filesystemType: "smbfs", want: filesystem.VolumeNetwork},
		{filesystemType: "nfs", want: filesystem.VolumeNetwork},
		{filesystemType: "webdav", want: filesystem.VolumeNetwork},
		{filesystemType: "fusefs", want: filesystem.VolumeUnknown},
	}

	for _, test := range tests {
		test := test
		t.Run(test.filesystemType, func(t *testing.T) {
			t.Parallel()
			if got := filesystem.ClassifyFilesystemType(test.filesystemType); got != test.want {
				t.Fatalf("卷类型分类不正确：got=%q want=%q", got, test.want)
			}
		})
	}
}
