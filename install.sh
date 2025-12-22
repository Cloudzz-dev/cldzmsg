#!/bin/bash
set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}"
echo "  ╔═══════════════════════════════════════╗"
echo "  ║         CLDZMSG Installer             ║"
echo "  ╚═══════════════════════════════════════╝"
echo -e "${NC}"

# Check for required tools
if ! command -v curl &> /dev/null; then
    echo -e "${RED}Error: curl is required but not installed.${NC}"
    exit 1
fi

# Configuration
REPO="cloudzz-dev/cldzmsg"
INSTALL_DIR="/usr/local/bin"
BINARY_NAME="cldzmsg"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64)
        ARCH="amd64"
        ;;
    aarch64|arm64)
        ARCH="arm64"
        ;;
    *)
        echo -e "${RED}Unsupported architecture: $ARCH${NC}"
        exit 1
        ;;
esac

echo -e "${YELLOW}Detected: ${OS}/${ARCH}${NC}"

# Get latest release
echo -e "${BLUE}Fetching latest release...${NC}"
LATEST=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$LATEST" ]; then
    echo -e "${YELLOW}No releases found. Building from source...${NC}"
    
    # Check for Go
    if ! command -v go &> /dev/null; then
        echo -e "${RED}Error: Go is required to build from source.${NC}"
        echo -e "Install Go: ${BLUE}sudo pacman -S go${NC} (Arch) or ${BLUE}sudo apt install golang${NC} (Debian/Ubuntu)"
        exit 1
    fi
    
    TEMP_DIR=$(mktemp -d)
    echo -e "${BLUE}Cloning repository...${NC}"
    git clone --depth 1 "https://github.com/${REPO}.git" "$TEMP_DIR"
    cd "$TEMP_DIR"
    
    echo -e "${BLUE}Building client...${NC}"
    go build -o "$BINARY_NAME" ./cmd/client
    
    echo -e "${BLUE}Installing to ${INSTALL_DIR}...${NC}"
    sudo mv "$BINARY_NAME" "$INSTALL_DIR/"
    
    cd -
    rm -rf "$TEMP_DIR"
else
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST}/cldzmsg-${OS}-${ARCH}"
    
    echo -e "${BLUE}Downloading ${LATEST}...${NC}"
    curl -sL "$DOWNLOAD_URL" -o "/tmp/${BINARY_NAME}"
    
    echo -e "${BLUE}Installing to ${INSTALL_DIR}...${NC}"
    sudo mv "/tmp/${BINARY_NAME}" "$INSTALL_DIR/"
    sudo chmod +x "$INSTALL_DIR/$BINARY_NAME"
fi

echo -e "${GREEN}"
echo "  ✓ Installation complete!"
echo ""
echo "  Run 'cldzmsg' to start messaging."
echo ""
echo "  Set your server with:"
echo "    export CLDZMSG_SERVER=ws://your-server:8080/ws"
echo -e "${NC}"
