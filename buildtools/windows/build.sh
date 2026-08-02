#!/bin/sh
set -eu

# 本脚本只接受 Step 0 已确认的 windows/amd64 构建矩阵。
if [ "${TARGET_PLATFORM:-}" != "windows/amd64" ]; then
  echo "TARGET_PLATFORM 必须是 windows/amd64" >&2
  exit 2
fi
if [ -z "${BUILD_VERSION:-}" ] || [ -z "${BUILD_COMMIT:-}" ] || [ -z "${BUILD_TIME:-}" ]; then
  echo "BUILD_VERSION、BUILD_COMMIT、BUILD_TIME 不能为空" >&2
  exit 2
fi

export GOOS=windows
export GOARCH=amd64
export CGO_ENABLED=1
export CC=x86_64-w64-mingw32-gcc
export CXX=x86_64-w64-mingw32-g++

LD_FLAGS="-X meet-sieve/internal/app/buildinfo.Version=${BUILD_VERSION} -X meet-sieve/internal/app/buildinfo.Commit=${BUILD_COMMIT} -X meet-sieve/internal/app/buildinfo.BuildTime=${BUILD_TIME} -X meet-sieve/internal/app/buildinfo.BuildMode=production"

mkdir -p /work/build/bin/windows-resources
cp /work/.cache/third_party/extracted/windows-amd64/onnxruntime-win-x64-1.26.0/lib/onnxruntime.dll \
  /work/build/bin/windows-resources/onnxruntime.dll
cp /work/.cache/third_party/extracted/windows-amd64/onnxruntime-win-x64-1.26.0/LICENSE \
  /work/build/bin/windows-resources/ONNXRUNTIME-LICENSE.txt

cd /work/cmd/meetsieve

case "${BUILD_ACTION:-build}" in
  build)
    wails build \
      -platform windows/amd64 \
      -skipbindings \
      -s \
      -nopackage \
      -nocolour \
      -trimpath \
      -o MeetSieve.exe \
      -ldflags "${LD_FLAGS}"
    ;;
  package)
    wails build \
      -platform windows/amd64 \
      -skipbindings \
      -s \
      -nsis \
      -webview2 browser \
      -nocolour \
      -trimpath \
      -o MeetSieve.exe \
      -ldflags "${LD_FLAGS}"
    ;;
  *)
    echo "BUILD_ACTION 只支持 build 或 package" >&2
    exit 2
    ;;
esac

# Wails 生成的 NSIS 工具文件包含空白行尾；构建后归一化，避免污染工作树。
sed -i 's/[[:space:]]*$//' /work/build/windows/installer/wails_tools.nsh
