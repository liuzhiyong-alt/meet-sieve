// Package configs 提供编译进二进制的技术默认配置。
package configs

import _ "embed"

// DefaultYAML 是 MeetSieve 的内嵌默认配置；不承载用户设置。
//
//go:embed config.yaml
var DefaultYAML []byte
