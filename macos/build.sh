#!/bin/bash
set -e

# LLrdc macOS Build Script
# This script compiles the native client and packages it into a .app bundle.

APP_NAME="LLrdc"
BUNDLE_ID="com.llrdc.client"
VERSION="1.0.0"

# Target directory
APP_DIR="macos/${APP_NAME}.app"
CONTENTS_DIR="${APP_DIR}/Contents"
MACOS_DIR="${CONTENTS_DIR}/MacOS"
RESOURCES_DIR="${CONTENTS_DIR}/Resources"

echo "Building LLrdc Native Client for macOS..."

# Create bundle structure
mkdir -p "${MACOS_DIR}"
mkdir -p "${RESOURCES_DIR}"

# Compile Go binary
# We use -tags native to enable the AppKit/VideoToolbox renderer
go build -tags native -o "${MACOS_DIR}/llrdc-client" cmd/client/main.go

# Copy Info.plist (if not exists, we'll create it below)
if [ ! -f "macos/Info.plist" ]; then
    cat > "macos/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>llrdc-client</string>
    <key>CFBundleIdentifier</key>
    <string>${BUNDLE_ID}</string>
    <key>CFBundleName</key>
    <string>${APP_NAME}</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>${VERSION}</string>
    <key>LSMinimumSystemVersion</key>
    <string>10.15</string>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
EOF
fi

cp "macos/Info.plist" "${CONTENTS_DIR}/Info.plist"

echo "Build complete: ${APP_DIR}"
echo "Run with: open ${APP_DIR} --args -server http://YOUR_SERVER_IP:8080"
