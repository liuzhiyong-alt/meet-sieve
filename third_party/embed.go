// Package thirdparty 内嵌应用运行时必须信任的第三方资源锁。
package thirdparty

import _ "embed"

// AssetsLockJSON 是构建时冻结的第三方运行时与官方声纹模型目录。
//
//go:embed assets.lock.json
var AssetsLockJSON []byte
