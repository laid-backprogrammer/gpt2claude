#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP_NAME="GPT2Claude Lite"
BUILD_DIR="$ROOT/dist"
TMP_DIR="$BUILD_DIR/tmp-universal"
APP_DIR="$BUILD_DIR/$APP_NAME.app"
CONTENTS="$APP_DIR/Contents"
MACOS="$CONTENTS/MacOS"
RESOURCES="$CONTENTS/Resources"
SWIFT_CACHE="${SWIFT_MODULE_CACHE:-/private/tmp/gpt2claude-swift-module-cache}"

rm -rf "$TMP_DIR"
mkdir -p "$MACOS" "$RESOURCES"
mkdir -p "$TMP_DIR"
mkdir -p "$SWIFT_CACHE"

cd "$ROOT"
GOCACHE="${GOCACHE:-/private/tmp/gpt2claude-go-cache}" GOOS=darwin GOARCH=arm64 go build -o "$TMP_DIR/gpt2claude-lite-arm64" .
GOCACHE="${GOCACHE:-/private/tmp/gpt2claude-go-cache}" GOOS=darwin GOARCH=amd64 go build -o "$TMP_DIR/gpt2claude-lite-amd64" .
lipo -create -output "$RESOURCES/gpt2claude-lite" "$TMP_DIR/gpt2claude-lite-arm64" "$TMP_DIR/gpt2claude-lite-amd64"

for ARCH in arm64 x86_64; do
  swiftc \
    -O \
    -parse-as-library \
    -target "$ARCH-apple-macos13.0" \
    -module-cache-path "$SWIFT_CACHE" \
    -framework SwiftUI \
    -framework AppKit \
    -o "$TMP_DIR/GPT2ClaudeLite-$ARCH" \
    "$ROOT/macos/GPT2ClaudeLiteApp.swift"
done
lipo -create -output "$MACOS/GPT2ClaudeLite" "$TMP_DIR/GPT2ClaudeLite-arm64" "$TMP_DIR/GPT2ClaudeLite-x86_64"

cat > "$CONTENTS/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key>
  <string>en</string>
  <key>CFBundleExecutable</key>
  <string>GPT2ClaudeLite</string>
  <key>CFBundleIdentifier</key>
  <string>local.gpt2claude.lite</string>
  <key>CFBundleInfoDictionaryVersion</key>
  <string>6.0</string>
  <key>CFBundleName</key>
  <string>GPT2Claude Lite</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>0.1.0</string>
  <key>CFBundleVersion</key>
  <string>1</string>
  <key>LSMinimumSystemVersion</key>
  <string>13.0</string>
  <key>NSHighResolutionCapable</key>
  <true/>
</dict>
</plist>
PLIST

chmod +x "$MACOS/GPT2ClaudeLite" "$RESOURCES/gpt2claude-lite"
if command -v codesign >/dev/null 2>&1; then
  codesign --force --deep --sign - "$APP_DIR" >/dev/null
fi
ditto -c -k --keepParent "$APP_DIR" "$BUILD_DIR/GPT2Claude-Lite-macOS-universal.zip"
echo "$APP_DIR"
echo "$BUILD_DIR/GPT2Claude-Lite-macOS-universal.zip"
