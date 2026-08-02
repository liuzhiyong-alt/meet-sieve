SHELL := /bin/sh

WAILS_VERSION := v2.13.0
WAILS_COMMAND := $(CURDIR)/.tools/bin/wails
GO_BIN := $(shell mise which go)
export GOCACHE := $(CURDIR)/.cache/go-build
WINDOWS_IMAGE := meetsieve-windows-builder:go1.25.9-wails2.13.0
BUILD_VERSION ?= 0.1.0-alpha
BUILD_MODE ?= production
BUILD_COMMIT := $(shell git rev-parse --short=12 HEAD)
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

.PHONY: bootstrap assets dev fmt lint typecheck test test-race test-contract test-asr-real smoke build build-windows-amd64 package-windows verify-package clean

# bootstrap 安装 mise 声明的工具并按 lockfile 恢复前端依赖。
bootstrap:
	mise install
	mkdir -p .tools/bin
	GOBIN="$(CURDIR)/.tools/bin" "$(GO_BIN)" install github.com/wailsapp/wails/v2/cmd/wails@$(WAILS_VERSION)
	mise exec -- pnpm --dir frontend install --frozen-lockfile

# assets 从官方锁定 URL 下载并校验当前平台和 Windows 构建所需动态库。
assets:
	mise exec -- go run ./buildtools/cmd/assets -all

# dev 从 Wails 配置所在目录启动本地桌面开发环境。
dev:
	cd cmd/meetsieve && GOCACHE="$(GOCACHE)" $(WAILS_COMMAND) dev -nocolour

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
	mise exec -- go test -race ./internal/domain/meeting ./internal/domain/transcript ./internal/domain/speaker ./internal/domain/correction ./internal/adapter/asr/volcano ./internal/service/meeting ./internal/service/transcript ./internal/service/speaker ./internal/service/correction ./tests/unit/app ./tests/unit/apperr ./tests/unit/assets ./tests/unit/clock ./tests/unit/codex ./tests/unit/config ./tests/unit/filesystem ./tests/unit/health ./tests/unit/identity ./tests/unit/logger ./tests/contract/port ./tests/contract/transport ./tests/contract/wails ./tests/integration/app ./tests/integration/codex ./tests/integration/http ./tests/integration/meeting -count=1

# test-contract 验证 Wails 与 Codex schema 的稳定契约文件。
test-contract:
	mise exec -- go test ./tests/contract/... -count=1
	test -f tests/contract/codex/schema/v1/InitializeParams.json
	test -f tests/contract/codex/schema/ClientNotification.json

# test-asr-real 显式使用真实火山凭据和默认麦克风；缺少开关或凭据时必须失败。
test-asr-real:
	test "$$MEETSIEVE_ASR_REAL" = "1"
	test -n "$$MEETSIEVE_VOLC_AUTH_MODE"
	mise exec -- go test -tags=asrreal -v ./tests/e2e/asr -count=1

# build 生成当前 macOS arm64 的 Wails production 应用包。
build: assets
	cd cmd/meetsieve && GOCACHE="$(GOCACHE)" $(WAILS_COMMAND) build -m -nocolour -trimpath \
		-ldflags "-X meet-sieve/internal/app/buildinfo.Version=$(BUILD_VERSION) -X meet-sieve/internal/app/buildinfo.Commit=$(BUILD_COMMIT) -X meet-sieve/internal/app/buildinfo.BuildTime=$(BUILD_TIME) -X meet-sieve/internal/app/buildinfo.BuildMode=$(BUILD_MODE)"
	LC_ALL=C perl -0pi -e 's/[ \t]+$$//mg; s/\n+\z/\n/' frontend/wailsjs/go/models.ts
	mkdir -p build/bin/MeetSieve.app/Contents/Resources/lib
	cp .cache/third_party/extracted/darwin-arm64/onnxruntime-osx-arm64-1.26.0/lib/libonnxruntime.1.26.0.dylib build/bin/MeetSieve.app/Contents/Resources/lib/
	cp .cache/third_party/extracted/darwin-arm64/onnxruntime-osx-arm64-1.26.0/LICENSE build/bin/MeetSieve.app/Contents/Resources/ONNXRUNTIME-LICENSE.txt
	if test -f models/voice-matching-profile.json; then mkdir -p build/bin/MeetSieve.app/Contents/Resources/models; cp models/voice-matching-profile.json build/bin/MeetSieve.app/Contents/Resources/models/; fi
	codesign --force --deep --sign - build/bin/MeetSieve.app

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

# build-windows-amd64 在固定 Docker 工具链中生成包含 CGO 依赖的 GUI PE。
build-windows-amd64: assets
	mise exec -- pnpm --dir frontend build
	docker build --platform linux/amd64 -t $(WINDOWS_IMAGE) buildtools/windows
	mkdir -p .cache/windows-go-build .cache/windows-go-mod
	docker run --rm --platform linux/amd64 \
		--user "$$(id -u):$$(id -g)" \
		-e TARGET_PLATFORM=windows/amd64 \
		-e BUILD_ACTION=build \
		-e BUILD_VERSION="$(BUILD_VERSION)" \
		-e BUILD_COMMIT="$(BUILD_COMMIT)" \
		-e BUILD_TIME="$(BUILD_TIME)" \
		-e GOCACHE=/work/.cache/windows-go-build \
		-e GOMODCACHE=/work/.cache/windows-go-mod \
		-v "$(CURDIR):/work" \
		$(WINDOWS_IMAGE)

# package-windows 使用同一固定镜像生成包含动态库和许可证的 NSIS 壳。
package-windows: assets
	mise exec -- pnpm --dir frontend build
	docker build --platform linux/amd64 -t $(WINDOWS_IMAGE) buildtools/windows
	mkdir -p .cache/windows-go-build .cache/windows-go-mod
	docker run --rm --platform linux/amd64 \
		--user "$$(id -u):$$(id -g)" \
		-e TARGET_PLATFORM=windows/amd64 \
		-e BUILD_ACTION=package \
		-e BUILD_VERSION="$(BUILD_VERSION)" \
		-e BUILD_COMMIT="$(BUILD_COMMIT)" \
		-e BUILD_TIME="$(BUILD_TIME)" \
		-e GOCACHE=/work/.cache/windows-go-build \
		-e GOMODCACHE=/work/.cache/windows-go-mod \
		-v "$(CURDIR):/work" \
		$(WINDOWS_IMAGE)

# verify-package 校验 PE 架构、GUI subsystem、CGO 链接标记、DLL、许可证和 NSIS。
verify-package:
	mise exec -- go run ./buildtools/cmd/verifywindows

# clean 只删除明确可重建的前端和构建产物，不触碰第三方下载 cache。
clean:
	rm -r build/bin frontend/dist
