#!/bin/bash

# ============================================================
# jcqsearch 多平台打包脚本
#
# 用法:
#   ./scripts/package.sh                        # 打包 6 个主要平台
#   ./scripts/package.sh --current              # 只打包当前平台
#   ./scripts/package.sh --platform linux-amd64 # 指定平台
#   ./scripts/package.sh --help                 # 显示帮助
# ============================================================

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
NC='\033[0m'

print_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
print_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
print_error()   { echo -e "${RED}[ERROR]${NC} $1"; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

VERSION_FILE="$PROJECT_ROOT/VERSION"
if [ -f "$VERSION_FILE" ]; then
    VERSION=$(cat "$VERSION_FILE" | tr -d '\n\r' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
else
    VERSION="0.0.0"
fi

BUILD_TIME=$(date '+%Y-%m-%d_%H:%M:%S')
COMMIT_SHA=$(git -C "$PROJECT_ROOT" rev-parse --short HEAD 2>/dev/null || echo "unknown")

BUILD_DIR="$PROJECT_ROOT/build"
DIST_DIR="$PROJECT_ROOT/deploy/dist"

LDFLAGS="-X 'jcqsearch/cmd.Version=$VERSION' -X 'jcqsearch/cmd.BuildTime=$BUILD_TIME' -X 'jcqsearch/cmd.CommitSHA=$COMMIT_SHA' -s -w"

# 构建单个平台
build_platform() {
    local GOOS=$1
    local GOARCH=$2
    local SUFFIX="${GOOS}_${GOARCH}"

    print_info "构建 ${GOOS}/${GOARCH} ..."

    local PKG_NAME="jcqsearch_${VERSION}_${SUFFIX}"
    local PKG_DIR="$BUILD_DIR/$PKG_NAME"
    mkdir -p "$PKG_DIR"

    local BIN_NAME="jcqsearch"
    [ "$GOOS" = "windows" ] && BIN_NAME="jcqsearch.exe"

    env CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH \
        go build -trimpath -ldflags "$LDFLAGS" \
        -o "$PKG_DIR/$BIN_NAME" "$PROJECT_ROOT"

    # 复制配置示例和 VERSION
    cp -f "$PROJECT_ROOT/config.example.yaml" "$PKG_DIR/"
    cp -f "$PROJECT_ROOT/VERSION" "$PKG_DIR/"

    # 创建压缩包
    cd "$BUILD_DIR"
    if [ "$GOOS" = "windows" ]; then
        zip -qr "$DIST_DIR/${PKG_NAME}.zip" "$PKG_NAME"
        local SIZE=$(du -h "$DIST_DIR/${PKG_NAME}.zip" | cut -f1)
        print_success "$PKG_NAME.zip ($SIZE)"
    else
        tar -czf "$DIST_DIR/${PKG_NAME}.tar.gz" "$PKG_NAME"
        local SIZE=$(du -h "$DIST_DIR/${PKG_NAME}.tar.gz" | cut -f1)
        print_success "$PKG_NAME.tar.gz ($SIZE)"
    fi
    cd "$PROJECT_ROOT"

    rm -rf "$PKG_DIR"
}

build_main_platforms() {
    build_platform linux   amd64
    build_platform linux   arm64
    build_platform darwin  amd64
    build_platform darwin  arm64
    build_platform windows amd64
    build_platform windows arm64
}

show_result() {
    echo ""
    echo -e "${CYAN}════════════════════════════════════════${NC}"
    echo -e "${GREEN}打包完成！${NC}"
    echo -e "${CYAN}════════════════════════════════════════${NC}"
    echo ""
    print_info "版本: $VERSION"
    print_info "输出目录: $DIST_DIR"
    echo ""
    if [ -d "$DIST_DIR" ]; then
        ls -lh "$DIST_DIR"/jcqsearch_*.{tar.gz,zip} 2>/dev/null | awk '{printf "  %-50s %s\n", $NF, $5}' || echo "  (无文件)"
    fi
    echo ""
}

main() {
    if ! command -v go &>/dev/null; then
        print_error "Go 未安装"
        exit 1
    fi

    echo ""
    echo -e "${MAGENTA}╔════════════════════════════════════════╗${NC}"
    echo -e "${MAGENTA}║     jcqsearch 多平台打包脚本            ║${NC}"
    echo -e "${MAGENTA}╚════════════════════════════════════════╝${NC}"
    echo ""
    print_info "版本: $VERSION"
    print_info "提交: $COMMIT_SHA"
    echo ""

    mkdir -p "$DIST_DIR"
    rm -rf "$BUILD_DIR"
    mkdir -p "$BUILD_DIR"

    case "${1:-}" in
        --help|-h)
            echo "用法: $0 [选项]"
            echo ""
            echo "  (无参数)                  打包 6 个主要平台"
            echo "  --current                 只打包当前平台"
            echo "  --platform <goos-goarch>  打包指定平台"
            exit 0
            ;;
        --current)
            local CURRENT_OS=$(uname -s | tr '[:upper:]' '[:lower:]')
            local CURRENT_ARCH=$(uname -m)
            case "$CURRENT_ARCH" in
                x86_64|amd64)   CURRENT_ARCH="amd64" ;;
                aarch64|arm64)  CURRENT_ARCH="arm64" ;;
            esac
            print_info "打包当前平台: ${CURRENT_OS}/${CURRENT_ARCH}"
            echo ""
            build_platform "$CURRENT_OS" "$CURRENT_ARCH"
            ;;
        --platform)
            if [ -z "${2:-}" ]; then
                print_error "请指定平台，例如: $0 --platform linux-amd64"
                exit 1
            fi
            local GOOS="${2%-*}"
            local GOARCH="${2#*-}"
            build_platform "$GOOS" "$GOARCH"
            ;;
        *)
            print_info "打包 6 个主要平台..."
            echo ""
            build_main_platforms
            ;;
    esac

    rm -rf "$BUILD_DIR"
    show_result
}

main "$@"
