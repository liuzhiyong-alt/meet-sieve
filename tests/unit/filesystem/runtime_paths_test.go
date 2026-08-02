package filesystem_test

import (
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/filesystem"
)

// TestResolveLogDir_ReturnsPlatformLogDirectory 验证 macOS 和 Windows 使用平台日志目录。
func TestResolveLogDir_ReturnsPlatformLogDirectory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  filesystem.RuntimeEnvironment
		want string
	}{
		{
			name: "macOS",
			env:  filesystem.RuntimeEnvironment{GOOS: "darwin", HomeDir: "/Users/alice"},
			want: filepath.Join("/Users/alice", "Library", "Logs", "MeetSieve"),
		},
		{
			name: "Windows",
			env: filesystem.RuntimeEnvironment{
				GOOS:         "windows",
				LocalAppData: `C:\Users\alice\AppData\Local`,
			},
			want: filepath.Join(`C:\Users\alice\AppData\Local`, "MeetSieve", "logs"),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := filesystem.ResolveLogDir(test.env)
			if err != nil {
				t.Fatalf("解析日志目录失败：%v", err)
			}
			if got != test.want {
				t.Fatalf("日志目录不正确：got %q, want %q", got, test.want)
			}
		})
	}
}

// TestResolveLogDir_RejectsUnsupportedPlatform 验证未支持平台不会静默选择目录。
func TestResolveLogDir_RejectsUnsupportedPlatform(t *testing.T) {
	t.Parallel()

	if _, err := filesystem.ResolveLogDir(filesystem.RuntimeEnvironment{GOOS: "linux"}); err == nil {
		t.Fatal("未支持平台必须返回错误")
	}
}

// TestResolveAppConfigDir_ReturnsPlatformLocatorDirectory 验证 locator 使用平台规定的系统应用目录。
func TestResolveAppConfigDir_ReturnsPlatformLocatorDirectory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  filesystem.RuntimeEnvironment
		want string
	}{
		{
			name: "macOS",
			env:  filesystem.RuntimeEnvironment{GOOS: "darwin", HomeDir: "/Users/alice"},
			want: filepath.Join("/Users/alice", "Library", "Application Support", "MeetSieve"),
		},
		{
			name: "Windows",
			env:  filesystem.RuntimeEnvironment{GOOS: "windows", AppData: `C:\Users\alice\AppData\Roaming`},
			want: filepath.Join(`C:\Users\alice\AppData\Roaming`, "MeetSieve"),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := filesystem.ResolveAppConfigDir(test.env)
			if err != nil {
				t.Fatalf("解析 locator 目录失败：%v", err)
			}
			if got != test.want {
				t.Fatalf("locator 目录不正确：got %q, want %q", got, test.want)
			}
		})
	}
}

// TestResolveAppDataDir_ReturnsPerUserModelDirectoryBase 验证模型使用每用户应用数据目录而非安装目录。
func TestResolveAppDataDir_ReturnsPerUserModelDirectoryBase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  filesystem.RuntimeEnvironment
		want string
	}{
		{
			name: "macOS",
			env:  filesystem.RuntimeEnvironment{GOOS: "darwin", HomeDir: "/Users/alice"},
			want: filepath.Join("/Users/alice", "Library", "Application Support", "MeetSieve"),
		},
		{
			name: "Windows",
			env:  filesystem.RuntimeEnvironment{GOOS: "windows", LocalAppData: `C:\Users\alice\AppData\Local`},
			want: filepath.Join(`C:\Users\alice\AppData\Local`, "MeetSieve"),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := filesystem.ResolveAppDataDir(test.env)
			if err != nil {
				t.Fatalf("解析应用数据目录失败：%v", err)
			}
			if got != test.want {
				t.Fatalf("应用数据目录不正确：got %q, want %q", got, test.want)
			}
		})
	}
}

// TestResolveBundledONNXLibrary_ReturnsInstalledResource 验证两平台只从安装资源目录加载运行时。
func TestResolveBundledONNXLibrary_ReturnsInstalledResource(t *testing.T) {
	t.Parallel()

	mac := filesystem.ResolveBundledONNXLibrary("/Applications/MeetSieve.app", "darwin")
	if mac != filepath.Join("/Applications/MeetSieve.app", "Contents", "Resources", "lib", "libonnxruntime.1.26.0.dylib") {
		t.Fatalf("macOS ONNX Runtime 路径不正确：%s", mac)
	}
	windows := filesystem.ResolveBundledONNXLibrary(`C:\Program Files\MeetSieve`, "windows")
	if windows != filepath.Join(`C:\Program Files\MeetSieve`, "onnxruntime.dll") {
		t.Fatalf("Windows ONNX Runtime 路径不正确：%s", windows)
	}
}
