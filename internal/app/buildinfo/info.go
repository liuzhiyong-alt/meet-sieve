// Package buildinfo 保存由 ldflags 注入的构建元数据。
package buildinfo

// 以下变量由生产构建通过 ldflags 注入；开发构建保留可识别默认值。
var (
	Version   = "dev"
	Commit    = "local"
	BuildTime = "unknown"
	BuildMode = "development"
)

// Info 是前端可读取的构建信息。
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"buildTime"`
	BuildMode string `json:"buildMode"`
}

// Current 返回当前构建的稳定信息投影。
func Current() Info {
	return Info{Version: Version, Commit: Commit, BuildTime: BuildTime, BuildMode: BuildMode}
}
