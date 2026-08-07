SHELL := /bin/sh

WAILS_VERSION := v2.13.0
WAILS_COMMAND := $(CURDIR)/.tools/bin/wails
GO_BIN := $(shell mise which go)
export GOCACHE := $(CURDIR)/.cache/go-build
WINDOWS_IMAGE := meetsieve-windows-builder:go1.25.9-wails2.13.0
BUILD_VERSION ?= 0.1.0-alpha.1
WINDOWS_FILE_VERSION ?= 0.1.0.1
BUILD_MODE ?= production
BUILD_COMMIT := $(shell git rev-parse HEAD)
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
MACOS_DMG := build/bin/MeetSieve-$(BUILD_VERSION)-macos-arm64.dmg
WINDOWS_INSTALLER := build/bin/MeetSieve-$(BUILD_VERSION)-windows-amd64-installer.exe

.PHONY: bootstrap assets calibrate-voice verify-voice-profile verify-build-metadata dev fmt lint typecheck test test-race test-contract test-asr-real guest-embed smoke build build-macos-arm64 package-macos verify-macos-package build-windows-amd64 package-windows verify-windows-package checksums verify-package clean

# bootstrap 安装 mise 声明的工具并按 lockfile 恢复前端依赖。
bootstrap:
	mise install
	mkdir -p .tools/bin
	GOBIN="$(CURDIR)/.tools/bin" "$(GO_BIN)" install github.com/wailsapp/wails/v2/cmd/wails@$(WAILS_VERSION)
	mise exec -- pnpm --dir frontend install --frozen-lockfile

# assets 从官方锁定 URL 下载并校验当前平台和 Windows 构建所需动态库。
assets:
	mise exec -- go run ./buildtools/cmd/assets -all

# calibrate-voice 仅接受显式真实数据和本机已锁资源，验收通过后才写正式档案。
calibrate-voice:
	test -n "$(VOICE_CALIBRATION_MANIFEST)"
	test -n "$(VOICE_MODEL_PATH)"
	test -n "$(VOICE_RUNTIME_PATH)"
	mise exec -- go run ./buildtools/cmd/calibratevoice \
		-manifest "$(VOICE_CALIBRATION_MANIFEST)" \
		-model "$(VOICE_MODEL_PATH)" \
		-runtime "$(VOICE_RUNTIME_PATH)"

# dev 从 Wails 配置所在目录启动本地桌面开发环境。
dev:
	cd cmd/meetsieve && GOCACHE="$(GOCACHE)" $(WAILS_COMMAND) dev -nocolour

# verify-voice-profile 阻止缺少真实校准记录或模型身份不匹配的发布构建。
verify-voice-profile:
	mise exec -- go run ./buildtools/cmd/verifyvoiceprofile

# verify-build-metadata 保证应用、PE 和安装器使用同一组合法构建身份。
verify-build-metadata:
	mise exec -- go run ./buildtools/cmd/verifybuildmeta \
		-version "$(BUILD_VERSION)" \
		-windows-file-version "$(WINDOWS_FILE_VERSION)" \
		-wails-config cmd/meetsieve/wails.json

# fmt 仅格式化项目源码和前端配置，不触碰依赖或构建产物。
fmt:
	mise exec -- gofmt -w $$(find cmd internal tests buildtools -name '*.go' -type f 2>/dev/null)
	mise exec -- pnpm --dir frontend format

# lint 运行当前已配置的 Go 与前端静态检查。
lint:
	mise exec -- go vet ./...
	mise exec -- pnpm --dir frontend lint
	mise exec -- pnpm --dir frontend format:check

# typecheck 执行 Vue 与 TypeScript 严格类型检查。
typecheck:
	mise exec -- pnpm --dir frontend typecheck

# test 执行当前所有 Go 单元和集成测试；前端测试目标在首个 Vitest 用例落盘后接入。
test:
	mise exec -- go test ./cmd/... ./internal/... ./buildtools/... ./tests/unit/... ./tests/integration/... ./tests/contract/... -count=1
	mise exec -- pnpm --dir frontend test

# test-race 只覆盖不依赖 GUI、音频设备或动态库的纯 Go 并发基础组件。
test-race:
	mise exec -- go test -race ./internal/domain/agent ./internal/domain/meeting ./internal/domain/transcript ./internal/domain/speaker ./internal/domain/correction ./internal/domain/guest ./internal/domain/gap ./internal/domain/minutes ./internal/adapter/agent/codex ./internal/adapter/asr/volcano ./internal/adapter/network ./internal/service/agent ./internal/service/meeting ./internal/service/transcript ./internal/service/speaker ./internal/service/correction ./internal/service/guest ./internal/service/lan ./internal/service/resource ./internal/service/finalization ./internal/service/gap ./internal/service/minutes ./internal/transport/http/guest ./tests/unit/agent ./tests/unit/app ./tests/unit/apperr ./tests/unit/assets ./tests/unit/clock ./tests/unit/codex ./tests/unit/config ./tests/unit/filesystem ./tests/unit/finalization ./tests/unit/gap ./tests/unit/health ./tests/unit/identity ./tests/unit/logger ./tests/unit/minutes ./tests/contract/port ./tests/contract/transport ./tests/contract/wails ./tests/integration/agent ./tests/integration/app ./tests/integration/codex ./tests/integration/finalization ./tests/integration/gap ./tests/integration/guest ./tests/integration/http ./tests/integration/meeting ./tests/integration/minutes ./tests/integration/resource -count=1

# test-contract 验证 Wails 与 Codex 必要 schema metadata 的稳定契约。
test-contract:
	mise exec -- go test ./tests/contract/... -count=1
	test -f tests/contract/codex/metadata.json

# test-asr-real 显式使用真实火山凭据和默认麦克风；缺少开关或凭据时必须失败。
test-asr-real:
	test "$$MEETSIEVE_ASR_REAL" = "1"
	test -n "$$MEETSIEVE_VOLC_API_KEY"
	mise exec -- go test -tags=asrreal -v ./tests/e2e/asr -count=1

# guest-embed 验证构建产物会同步到 Go embed 目录，防止二进制丢失 /join 访客入口。
guest-embed:
	mise exec -- pnpm --dir frontend build:guest
	mise exec -- pnpm --dir frontend build:embed
	test -f cmd/meetsieve/frontend/dist/guest/guest.html

# build 保持既有调用兼容，默认生成当前支持的 macOS arm64 production 应用。
build: build-macos-arm64

# build-macos-arm64 生成完整 macOS arm64 Wails production 应用包。
build-macos-arm64: assets verify-voice-profile verify-build-metadata
	cd cmd/meetsieve && GOCACHE="$(GOCACHE)" $(WAILS_COMMAND) build -m -nocolour -trimpath \
		-ldflags "-X meet-sieve/internal/app/buildinfo.Version=$(BUILD_VERSION) -X meet-sieve/internal/app/buildinfo.Commit=$(BUILD_COMMIT) -X meet-sieve/internal/app/buildinfo.BuildTime=$(BUILD_TIME) -X meet-sieve/internal/app/buildinfo.BuildMode=$(BUILD_MODE)"
	LC_ALL=C perl -0pi -e 's/[ \t]+$$//mg; s/\n+\z/\n/' frontend/wailsjs/go/models.ts
	mkdir -p build/bin/MeetSieve.app/Contents/Resources/lib
	cp .cache/third_party/extracted/darwin-arm64/onnxruntime-osx-arm64-1.26.0/lib/libonnxruntime.1.26.0.dylib build/bin/MeetSieve.app/Contents/Resources/lib/
	cp .cache/third_party/extracted/darwin-arm64/onnxruntime-osx-arm64-1.26.0/LICENSE build/bin/MeetSieve.app/Contents/Resources/ONNXRUNTIME-LICENSE.txt
	mkdir -p build/bin/MeetSieve.app/Contents/Resources/models
	cp models/voice-matching-profile.json build/bin/MeetSieve.app/Contents/Resources/models/
	codesign --force --deep --sign - build/bin/MeetSieve.app

# package-macos 使用系统 hdiutil 生成并挂载复核标准只读 DMG。 （生成mac os安装包）
package-macos: build-macos-arm64
	buildtools/macos/package.sh \
		build/bin/MeetSieve.app \
		"$(MACOS_DMG)" \
		"MeetSieve $(BUILD_VERSION)"
	$(MAKE) checksums BUILD_VERSION="$(BUILD_VERSION)"

# verify-macos-package 校验 DMG、Mach-O、动态库、许可证、profile 与资源哈希。
verify-macos-package:
	mise exec -- go run ./buildtools/cmd/verifymacos \
		-app build/bin/MeetSieve.app \
		-dmg "$(MACOS_DMG)"

# smoke 运行真实音频、ONNX、Codex smoke，并确认 production .app 能保持启动后正常退出。
smoke: assets
	mise exec -- go test -v ./tests/e2e/audio ./tests/e2e/onnx ./tests/e2e/codex -count=1
	$(MAKE) build BUILD_MODE=smoke
	app_log="$$HOME/Library/Logs/MeetSieve/app.log"; \
		start_lines=0; \
		if test -f "$$app_log"; then start_lines=$$(wc -l < "$$app_log"); fi; \
		build/bin/MeetSieve.app/Contents/MacOS/MeetSieve & \
		app_pid=$$!; \
		sleep 3; \
		kill -0 $$app_pid; \
		kill $$app_pid; \
		wait $$app_pid || true; \
		test -f "$$app_log"; \
		tail -n "+$$((start_lines + 1))" "$$app_log" | \
			grep -q '"msg":"Wails event round-trip completed".*"payload":"step0-smoke"'

# build-windows-amd64 在固定 Docker 工具链中生成包含 CGO 依赖的 GUI PE。 （生成windows安装包）
build-windows-amd64: assets verify-voice-profile verify-build-metadata guest-embed
	mise exec -- pnpm --dir frontend build
	mise exec -- pnpm --dir frontend build:embed
	docker build --platform linux/amd64 -t $(WINDOWS_IMAGE) buildtools/windows
	mkdir -p .cache/windows-go-build .cache/windows-go-mod
	docker run --rm --platform linux/amd64 \
		--user "$$(id -u):$$(id -g)" \
		-e TARGET_PLATFORM=windows/amd64 \
		-e BUILD_ACTION=build \
		-e BUILD_VERSION="$(BUILD_VERSION)" \
		-e WINDOWS_FILE_VERSION="$(WINDOWS_FILE_VERSION)" \
		-e BUILD_COMMIT="$(BUILD_COMMIT)" \
		-e BUILD_TIME="$(BUILD_TIME)" \
		-e GOCACHE=/work/.cache/windows-go-build \
		-e GOMODCACHE=/work/.cache/windows-go-mod \
		-v "$(CURDIR):/work" \
		$(WINDOWS_IMAGE)

# package-windows 使用同一固定镜像生成包含动态库和许可证的 NSIS 壳。
package-windows: assets verify-voice-profile verify-build-metadata guest-embed
	mise exec -- pnpm --dir frontend build
	mise exec -- pnpm --dir frontend build:embed
	docker build --platform linux/amd64 -t $(WINDOWS_IMAGE) buildtools/windows
	mkdir -p .cache/windows-go-build .cache/windows-go-mod
	docker run --rm --platform linux/amd64 \
		--user "$$(id -u):$$(id -g)" \
		-e TARGET_PLATFORM=windows/amd64 \
		-e BUILD_ACTION=package \
		-e BUILD_VERSION="$(BUILD_VERSION)" \
		-e WINDOWS_FILE_VERSION="$(WINDOWS_FILE_VERSION)" \
		-e BUILD_COMMIT="$(BUILD_COMMIT)" \
		-e BUILD_TIME="$(BUILD_TIME)" \
		-e GOCACHE=/work/.cache/windows-go-build \
		-e GOMODCACHE=/work/.cache/windows-go-mod \
		-v "$(CURDIR):/work" \
		$(WINDOWS_IMAGE)
	$(MAKE) checksums BUILD_VERSION="$(BUILD_VERSION)"

# verify-windows-package 校验 PE、NSIS、安全安装契约、资源哈希和许可证。
verify-windows-package:
	mise exec -- go run ./buildtools/cmd/verifywindows \
		-installer "$(WINDOWS_INSTALLER)"

# checksums 为当前版本已存在的最终安装介质生成可追溯 SHA-256 清单。
checksums:
	@mkdir -p build/bin
	@: > build/bin/SHA256SUMS.txt
	@if test -f "$(MACOS_DMG)"; then shasum -a 256 "$(MACOS_DMG)" >> build/bin/SHA256SUMS.txt; fi
	@if test -f "$(WINDOWS_INSTALLER)"; then shasum -a 256 "$(WINDOWS_INSTALLER)" >> build/bin/SHA256SUMS.txt; fi
	@test -s build/bin/SHA256SUMS.txt

# verify-package 聚合双平台校验；静态通过不替代 Windows 真机验证。
verify-package: verify-macos-package verify-windows-package checksums

# clean 只删除明确可重建的前端和构建产物，不触碰第三方下载 cache。
clean:
	rm -r build/bin frontend/dist
