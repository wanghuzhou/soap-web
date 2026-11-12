#!/bin/sh
# 构建 macOS 桌面版 .app。
#
# 前置条件：
#   - 本机已安装 Go（go.mod 要求 1.27.1+）
#   - 已安装 Xcode Command Line Tools（xcode-select --install），Wails 的
#     macOS 后端是 cgo + Objective-C，必须有 macOS SDK，无法在 Linux 上交叉编译
#
# 用法：./build-macos.sh        产物：bin/soap-web.app
set -eu

cd "$(dirname "$0")"

APP_NAME="soap-web"
EXEC_NAME="soapweb-desktop"
BUNDLE_ID="com.wanghz.soapweb"
VERSION="${VERSION:-0.1.0}"
OUT="bin/${APP_NAME}.app"

rm -rf "$OUT"
mkdir -p "$OUT/Contents/MacOS" "$OUT/Contents/Resources"

# CGO 必须开启：Wails 通过 cgo 调用 Cocoa/WebKit。
CGO_ENABLED=1 go build -tags production -trimpath \
	-o "$OUT/Contents/MacOS/${EXEC_NAME}" ./cmd/soapweb-desktop

cat > "$OUT/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key><string>zh_CN</string>
	<key>CFBundleExecutable</key><string>${EXEC_NAME}</string>
	<key>CFBundleIdentifier</key><string>${BUNDLE_ID}</string>
	<key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
	<key>CFBundleName</key><string>${APP_NAME}</string>
	<key>CFBundleDisplayName</key><string>${APP_NAME}</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>${VERSION}</string>
	<key>CFBundleVersion</key><string>${VERSION}</string>
	<key>LSMinimumSystemVersion</key><string>11.0</string>
	<key>NSHighResolutionCapable</key><true/>
	<key>NSPrincipalClass</key><string>NSApplication</string>
</dict>
</plist>
PLIST

# 本地自测用 ad-hoc 签名；对外分发需要 Developer ID 证书 + 公证。
codesign --force --deep -s - "$OUT"

echo "已生成 $OUT"
echo "数据目录：~/Library/Application Support/soap-web/data（可用 -data 指定其他目录）"
