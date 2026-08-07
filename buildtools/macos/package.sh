#!/bin/sh
set -eu

# 本脚本只把已完成校验的 .app 制作为标准只读 DMG，并在挂载后复核最小卷结构。
if [ "$#" -ne 3 ]; then
  echo "usage: package.sh <app-path> <dmg-path> <volume-name>" >&2
  exit 2
fi

APP_PATH=$1
DMG_PATH=$2
VOLUME_NAME=$3

if [ ! -d "$APP_PATH" ]; then
  echo "macOS 应用不存在: $APP_PATH" >&2
  exit 2
fi

STAGING_DIR=$(mktemp -d)
MOUNT_DIR=$(mktemp -d)
MOUNTED=0

# cleanup 只清理本脚本创建的临时目录，并在异常退出时卸载测试卷。
cleanup() {
  if [ "$MOUNTED" -eq 1 ]; then
    hdiutil detach "$MOUNT_DIR" >/dev/null 2>&1 || true
  fi
  rm -rf "$STAGING_DIR" "$MOUNT_DIR"
}
trap cleanup EXIT INT TERM

cp -R "$APP_PATH" "$STAGING_DIR/MeetSieve.app"
ln -s /Applications "$STAGING_DIR/Applications"
mkdir -p "$(dirname "$DMG_PATH")"
rm -f "$DMG_PATH"

hdiutil create \
  -volname "$VOLUME_NAME" \
  -srcfolder "$STAGING_DIR" \
  -format UDZO \
  -ov \
  "$DMG_PATH" >/dev/null

hdiutil attach -readonly -nobrowse -mountpoint "$MOUNT_DIR" "$DMG_PATH" >/dev/null
MOUNTED=1
test -d "$MOUNT_DIR/MeetSieve.app"
test -L "$MOUNT_DIR/Applications"
test "$(readlink "$MOUNT_DIR/Applications")" = "/Applications"
hdiutil detach "$MOUNT_DIR" >/dev/null
MOUNTED=0

hdiutil verify "$DMG_PATH" >/dev/null
