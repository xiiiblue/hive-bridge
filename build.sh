#!/bin/sh
# 在 macOS 独立构建 Windows x64 和 macOS 通用（Apple Silicon + Intel）分发包。
# 默认构建不包含私有 hivebridge.json；如需为内部团队随包附带本地配置，可设置 BUNDLE_CONFIG=1。
set -eu
cd "$(dirname "$0")"
export TMPDIR=/tmp
stage=$(mktemp -d "$TMPDIR/hive-bridge-build.XXXXXXXX")
trap 'rm -rf "$stage"' EXIT
(cd frontend && npm ci --no-audit --no-fund && npm run build)
app="$stage/macos/HiveBridge.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources" "$stage/windows" dist
for arch in amd64 arm64; do
  MACOSX_DEPLOYMENT_TARGET=11.0 CGO_LDFLAGS="-framework UniformTypeIdentifiers" GOOS=darwin GOARCH="$arch" CGO_ENABLED=1 go build -trimpath -tags 'desktop production' -ldflags '-s -w' -o "$stage/mac-$arch" .
done
lipo -create "$stage/mac-amd64" "$stage/mac-arm64" -output "$app/Contents/MacOS/hivebridge"
cp assets/hivebridge.icns "$app/Contents/Resources/"
cp Info.plist "$app/Contents/Info.plist"
codesign --force --sign - "$app"
codesign --verify --strict "$app"

# 将 Windows 资源留在临时目录，避免不同平台或同时构建时修改源码。
src="$stage/source"
mkdir -p "$src/frontend"
cp ./*.go go.mod go.sum "$src/"
cp -R publisher "$src/"
cp -R frontend/dist "$src/frontend/"
go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.5.0 -64 \
  -manifest assets/windows.manifest -icon assets/hivebridge.ico \
  -o "$src/resource_windows_amd64.syso" assets/windows-version.json
(cd "$src" && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -tags 'desktop production' -ldflags '-s -w -H windowsgui' -o "$stage/windows/hivebridge.exe" .)
for platform in windows macos; do
  cp hivebridge.example.json README.md LICENSE "$stage/$platform/"
  if [ "${BUNDLE_CONFIG:-0}" = "1" ] && [ -f hivebridge.json ]; then
    cp hivebridge.json "$stage/$platform/"
  fi
  cp -R images "$stage/$platform/"
  name="hive-bridge-1.0-$platform.zip"
  (cd "$stage/$platform" && zip -qr "../$name" .)
  mv "$stage/$name" "dist/$name"
done
