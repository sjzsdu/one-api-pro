#!/bin/bash
# One API Pro - Install Script for Codex/OpenCode AI Agent Environments
# 
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/modelbus/one-api-pro/main/scripts/install.sh | bash
#   OR
#   bash scripts/install.sh
#
# This script installs one-api-pro binary to ~/.local/bin/
# and creates a default .env configuration file if needed.

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
INSTALL_DIR="${HOME}/.local/bin"
CONFIG_DIR="${HOME}/.config/one-api-pro"
BINARY_NAME="one-api-pro"
GITHUB_REPO="modelbus/one-api-pro"
VERSION="${VERSION:-latest}"

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if command exists
check_command() {
    command -v "$1" >/dev/null 2>&1
}

# Detect platform
detect_platform() {
    local os=$(uname -s | tr '[:upper:]' '[:lower:]')
    local arch=$(uname -m)
    
    case $os in
        linux) OS="linux" ;;
        darwin) OS="darwin" ;;
        *) log_error "Unsupported OS: $os"; exit 1 ;;
    esac
    
    case $arch in
        x86_64|amd64) ARCH="amd64" ;;
        arm64|aarch64) ARCH="arm64" ;;
        *) log_error "Unsupported architecture: $arch"; exit 1 ;;
    esac
    
    PLATFORM="${OS}_${ARCH}"
    log_info "Detected platform: ${PLATFORM}"
}

# Check dependencies
check_dependencies() {
    local missing=()
    
    if ! check_command curl; then
        missing+=("curl")
    fi
    
    if ! check_command tar; then
        missing+=("tar")
    fi
    
    if [ ${#missing[@]} -gt 0 ]; then
        log_error "Missing dependencies: ${missing[*]}"
        log_info "Please install them first:"
        if [[ "$OSTYPE" == "darwin"* ]]; then
            log_info "  brew install ${missing[*]}"
        else
            log_info "  sudo apt-get install ${missing[*]}"
        fi
        exit 1
    fi
}

# Get latest version from GitHub
get_latest_version() {
    if [ "$VERSION" = "latest" ]; then
        VERSION=$(curl -s "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
        if [ -z "$VERSION" ]; then
            VERSION="v0.0.16"  # fallback
        fi
    fi
    log_info "Version: ${VERSION}"
}

# Download and install binary
install_binary() {
    local temp_dir=$(mktemp -d)
    local archive_name="${BINARY_NAME}-${VERSION}-${PLATFORM}.tar.gz"
    local download_url="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/${archive_name}"
    
    log_info "Downloading from: ${download_url}"
    
    # Try downloading the release archive
    if curl -fsSL "$download_url" -o "${temp_dir}/${archive_name}" 2>/dev/null; then
        log_info "Extracting..."
        tar -xzf "${temp_dir}/${archive_name}" -C "${temp_dir}"
        
        # Find the binary
        local binary_path=$(find "${temp_dir}" -name "${BINARY_NAME}" -type f -executable | head -1)
        if [ -z "$binary_path" ]; then
            binary_path=$(find "${temp_dir}" -name "${BINARY_NAME}" -type f | head -1)
        fi
        
        if [ -z "$binary_path" ]; then
            log_error "Binary not found in archive"
            exit 1
        fi
    else
        log_warn "Release archive not found, attempting to download binary directly..."
        # Fallback: try to download the binary directly
        local binary_url="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/${BINARY_NAME}-${PLATFORM}"
        curl -fsSL "$binary_url" -o "${temp_dir}/${BINARY_NAME}"
        chmod +x "${temp_dir}/${BINARY_NAME}"
        binary_path="${temp_dir}/${BINARY_NAME}"
    fi
    
    # Create install directory
    mkdir -p "${INSTALL_DIR}"
    
    # Install binary
    install -m 0755 "${binary_path}" "${INSTALL_DIR}/${BINARY_NAME}"
    
    # Cleanup
    rm -rf "${temp_dir}"
    
    log_info "Binary installed to: ${INSTALL_DIR}/${BINARY_NAME}"
}

# Create default configuration
create_config() {
    local env_file="${CONFIG_DIR}/.env"
    
    if [ -f "${env_file}" ]; then
        log_warn "Configuration file already exists: ${env_file}"
        return
    fi
    
    mkdir -p "${CONFIG_DIR}"
    
    cat > "${env_file}" << 'EOF'
# One API Pro Configuration
# See documentation for more options

# Session secret (CHANGE THIS to a random string)
SESSION_SECRET=random_string_change_me

# Database configuration
# SQLITE_PATH=./one-api-pro.db

# Redis configuration (optional)
# REDIS_URL=redis://localhost:6379

# Logging
LOG_DIR=./logs

# Port
PORT=3000

# Theme
# THEME=default
EOF
    
    log_info "Default configuration created at: ${env_file}"
    log_warn "Please edit ${env_file} and change SESSION_SECRET to a random string!"
}

# Add to PATH
setup_path() {
    if [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
        log_info "Adding ${INSTALL_DIR} to PATH..."
        
        # Detect shell
        local shell_name=$(basename "$SHELL")
        local rc_file=""
        
        case "$shell_name" in
            bash)
                rc_file="$HOME/.bashrc"
                ;;
            zsh)
                rc_file="$HOME/.zshrc"
                ;;
            fish)
                rc_file="$HOME/.config/fish/config.fish"
                ;;
        esac
        
        if [ -n "$rc_file" ]; then
            if [[ "$shell_name" == "fish" ]]; then
                echo "set -gx PATH \$PATH ${INSTALL_DIR}" >> "$rc_file"
            else
                echo "export PATH=\"\$PATH:${INSTALL_DIR}\"" >> "$rc_file"
            fi
            log_info "Updated ${rc_file}"
            log_info "Please restart your shell or run: source ${rc_file}"
        fi
    fi
}

# Print usage
print_usage() {
    echo ""
    echo "One API Pro has been installed successfully!"
    echo ""
    echo "Usage:"
    echo "  ${BINARY_NAME} --help                  Show help"
    echo "  ${BINARY_NAME} --version               Show version"
    echo "  ${BINARY_NAME} --port 3000             Start on port 3000"
    echo "  ${BINARY_NAME} --daemon                Start in daemon mode"
    echo "  ${BINARY_NAME} --stop                  Stop daemon"
    echo "  ${BINARY_NAME} --status                Check daemon status"
    echo ""
    echo "Configuration:"
    echo "  Edit ${CONFIG_DIR}/.env"
    echo ""
    echo "Quick start:"
    echo "  ${BINARY_NAME} --env ${CONFIG_DIR}/.env"
    echo ""
}

# Main
main() {
    log_info "Installing One API Pro..."
    
    detect_platform
    check_dependencies
    get_latest_version
    install_binary
    create_config
    setup_path
    print_usage
}

main "$@"
