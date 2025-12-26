#!/bin/bash
set -e

APP_NAME="cldzmsg"
VERSION="v0.1.1"
DIST_DIR="dist"

# Clean up
rm -rf $DIST_DIR
mkdir -p $DIST_DIR

echo "📦 Building $APP_NAME $VERSION..."

# 1. Linux AMD64 (Standard PC/Server)
echo "  • Building Linux (amd64)..."
GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o "$DIST_DIR/$APP_NAME-linux-amd64" ./cmd/client

# 2. Linux ARM64 (Raspberry Pi 64-bit)
echo "  • Building Linux (arm64)..."
GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o "$DIST_DIR/$APP_NAME-linux-arm64" ./cmd/client

# 3. macOS AMD64 (Intel Mac)
echo "  • Building macOS (amd64)..."
GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w" -o "$DIST_DIR/$APP_NAME-darwin-amd64" ./cmd/client

# 4. macOS ARM64 (Apple Silicon M1/M2/M3)
echo "  • Building macOS (arm64)..."
GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w" -o "$DIST_DIR/$APP_NAME-darwin-arm64" ./cmd/client

# 5. Windows AMD64
echo "  • Building Windows (amd64)..."
GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o "$DIST_DIR/$APP_NAME-windows-amd64.exe" ./cmd/client

echo "✅ Build complete. Artifacts in $DIST_DIR/"
ls -lh $DIST_DIR
