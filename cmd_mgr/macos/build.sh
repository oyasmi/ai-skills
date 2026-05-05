#!/bin/bash
set -e

cd "$(dirname "$0")"

echo "🚀 Building CmdMgr for macOS..."

# Build release binary
swift build -c release 2>&1

# Get the binary path
BINARY=$(swift build -c release --show-bin-path)/CmdMgr

# Create .app bundle
APP_DIR="dist/CmdMgr.app/Contents"
rm -rf dist
mkdir -p "$APP_DIR/MacOS"
mkdir -p "$APP_DIR/Resources"

cp "$BINARY" "$APP_DIR/MacOS/CmdMgr"

# Copy icon if available
ICON_PATH="../icon.icns"
if [ -f "$ICON_PATH" ]; then
    cp "$ICON_PATH" "$APP_DIR/Resources/AppIcon.icns"
fi

# Create Info.plist
cat > "$APP_DIR/Info.plist" << 'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>CmdMgr</string>
    <key>CFBundleIdentifier</key>
    <string>com.cmdmgr.app</string>
    <key>CFBundleName</key>
    <string>CmdMgr</string>
    <key>CFBundleDisplayName</key>
    <string>Command Manager</string>
    <key>CFBundleVersion</key>
    <string>1.0.0</string>
    <key>CFBundleShortVersionString</key>
    <string>1.0.0</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>LSMinimumSystemVersion</key>
    <string>13.0</string>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>CFBundleInfoDictionaryVersion</key>
    <string>6.0</string>
</dict>
</plist>
EOF

echo "✅ Build successful!"
echo "📦 App bundle: $(pwd)/dist/CmdMgr.app"

# Show size
SIZE=$(du -sh dist/CmdMgr.app | cut -f1)
echo "📐 Bundle size: $SIZE"
