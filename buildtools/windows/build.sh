#!/bin/sh
set -eu

# 本脚本只接受 Step 0 已确认的 windows/amd64 构建矩阵。
if [ "${TARGET_PLATFORM:-}" != "windows/amd64" ]; then
  echo "TARGET_PLATFORM 必须是 windows/amd64" >&2
  exit 2
fi
if [ -z "${BUILD_VERSION:-}" ] || [ -z "${WINDOWS_FILE_VERSION:-}" ] || [ -z "${BUILD_COMMIT:-}" ] || [ -z "${BUILD_TIME:-}" ]; then
  echo "BUILD_VERSION、WINDOWS_FILE_VERSION、BUILD_COMMIT、BUILD_TIME 不能为空" >&2
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
mkdir -p /work/build/bin/windows-resources/models
cp /work/models/voice-matching-profile.json \
  /work/build/bin/windows-resources/models/voice-matching-profile.json

cd /work/cmd/meetsieve

case "${BUILD_ACTION:-build}" in
  build | package) ;;
  *)
    echo "BUILD_ACTION 只支持 build 或 package" >&2
    exit 2
    ;;
esac

wails build \
  -platform windows/amd64 \
  -skipbindings \
  -s \
  -nopackage \
  -nocolour \
  -trimpath \
  -o MeetSieve.exe \
  -ldflags "${LD_FLAGS}"

if [ "${BUILD_ACTION:-build}" = "package" ]; then
  WINDOWS_RESOURCES=/work/build/bin/windows-resources
  EXECUTABLE=/work/build/bin/MeetSieve.exe
  INSTALL_MARKER=${WINDOWS_RESOURCES}/meetsieve-install.json
  FILE_MANIFEST=${WINDOWS_RESOURCES}/meetsieve-files.json
  BUILD_INCLUDE=${WINDOWS_RESOURCES}/meetsieve_build.nsh

  executable_sha=$(sha256sum "${EXECUTABLE}" | awk '{print $1}')
  runtime_sha=$(sha256sum "${WINDOWS_RESOURCES}/onnxruntime.dll" | awk '{print $1}')
  license_sha=$(sha256sum "${WINDOWS_RESOURCES}/ONNXRUNTIME-LICENSE.txt" | awk '{print $1}')
  profile_sha=$(sha256sum "${WINDOWS_RESOURCES}/models/voice-matching-profile.json" | awk '{print $1}')

  # 安装标识只用于识别产品目录，不作为卸载时可执行的动态删除指令。
  printf '%s\n' \
    "{\"schema_version\":1,\"product_id\":\"meet-sieve\",\"version\":\"${BUILD_VERSION}\",\"arch\":\"amd64\",\"commit\":\"${BUILD_COMMIT}\",\"build_time\":\"${BUILD_TIME}\"}" \
    > "${INSTALL_MARKER}"
  printf '%s\n' \
    "{\"schema_version\":1,\"files\":[{\"path\":\"MeetSieve.exe\",\"sha256\":\"${executable_sha}\"},{\"path\":\"onnxruntime.dll\",\"sha256\":\"${runtime_sha}\"},{\"path\":\"ONNXRUNTIME-LICENSE.txt\",\"sha256\":\"${license_sha}\"},{\"path\":\"models/voice-matching-profile.json\",\"sha256\":\"${profile_sha}\"}]}" \
    > "${FILE_MANIFEST}"
  printf '%s\n' \
    "!define MEETSIEVE_BUILD_VERSION \"${BUILD_VERSION}\"" \
    "!define MEETSIEVE_FILE_VERSION \"${WINDOWS_FILE_VERSION}\"" \
    > "${BUILD_INCLUDE}"

  cd /work/build/windows/installer
  makensis \
    -DARG_WAILS_AMD64_BINARY="${EXECUTABLE}" \
    project.nsi

  INSTALLER=/work/build/bin/MeetSieve-${BUILD_VERSION}-windows-amd64-installer.exe
  VERIFY_DIR=$(mktemp -d)
  trap 'rm -rf "${VERIFY_DIR}"' EXIT INT TERM
  7z x -y -o"${VERIFY_DIR}" "${INSTALLER}" \
    MeetSieve.exe \
    onnxruntime.dll \
    ONNXRUNTIME-LICENSE.txt \
    models/voice-matching-profile.json \
    meetsieve-install.json \
    meetsieve-files.json >/dev/null

  # 解包后的必装资源必须与进入 NSIS 前的锁定输入逐字节一致。
  cmp "${EXECUTABLE}" "${VERIFY_DIR}/MeetSieve.exe"
  cmp "${WINDOWS_RESOURCES}/onnxruntime.dll" "${VERIFY_DIR}/onnxruntime.dll"
  cmp "${WINDOWS_RESOURCES}/ONNXRUNTIME-LICENSE.txt" "${VERIFY_DIR}/ONNXRUNTIME-LICENSE.txt"
  cmp "${WINDOWS_RESOURCES}/models/voice-matching-profile.json" "${VERIFY_DIR}/models/voice-matching-profile.json"
  cmp "${INSTALL_MARKER}" "${VERIFY_DIR}/meetsieve-install.json"
  cmp "${FILE_MANIFEST}" "${VERIFY_DIR}/meetsieve-files.json"
  if 7z l "${INSTALLER}" | grep -Eiq '(^|[[:space:]/])[^/]*\.onnx([[:space:]]|$)'; then
    echo "NSIS 安装包不得内置声纹模型权重" >&2
    exit 1
  fi
  rm -rf "${VERIFY_DIR}"
  trap - EXIT INT TERM
fi
