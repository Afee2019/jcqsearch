#!/bin/bash

# ============================================================
# jcqsearch 版本号管理脚本
#
# 用法:
#   ./scripts/update-version.sh --check       检查版本
#   ./scripts/update-version.sh --set 1.0.0   设置版本号
#   ./scripts/update-version.sh --bump patch   升级补丁版本
#   ./scripts/update-version.sh --bump minor   升级次版本
#   ./scripts/update-version.sh --bump major   升级主版本
# ============================================================

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VERSION_FILE="$PROJECT_ROOT/VERSION"

print_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
print_success() { echo -e "${GREEN}[✓]${NC} $1"; }
print_error()   { echo -e "${RED}[ERROR]${NC} $1"; }

get_version() {
    if [ -f "$VERSION_FILE" ]; then
        cat "$VERSION_FILE" | tr -d '\n' | tr -d '\r' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//'
    else
        echo "0.0.0"
    fi
}

validate_version() {
    local version=$1
    if [[ ! $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        print_error "版本号格式错误：$version（正确格式: X.Y.Z）"
        return 1
    fi
    return 0
}

bump_version() {
    local current=$1
    local part=$2

    local major minor patch
    IFS='.' read -r major minor patch <<< "$current"

    case "$part" in
        major)
            major=$((major + 1))
            minor=0
            patch=0
            ;;
        minor)
            minor=$((minor + 1))
            patch=0
            ;;
        patch)
            patch=$((patch + 1))
            ;;
        *)
            print_error "无效的版本部分: $part（可选: major, minor, patch）"
            return 1
            ;;
    esac

    echo "${major}.${minor}.${patch}"
}

set_version() {
    local new_version=$1
    if ! validate_version "$new_version"; then
        return 1
    fi

    local old_version=$(get_version)
    echo "$new_version" > "$VERSION_FILE"
    print_success "VERSION 文件已更新: $old_version -> $new_version"
}

check_version() {
    local version=$(get_version)
    echo ""
    echo -e "${CYAN}========================================${NC}"
    echo -e "${CYAN}    jcqsearch 版本信息${NC}"
    echo -e "${CYAN}========================================${NC}"
    echo ""
    echo "VERSION 文件: $version"
    echo "Git 提交:     $(git -C "$PROJECT_ROOT" rev-parse --short HEAD 2>/dev/null || echo 'unknown')"
    echo "Git 分支:     $(git -C "$PROJECT_ROOT" rev-parse --abbrev-ref HEAD 2>/dev/null || echo 'unknown')"
    echo ""
}

main() {
    case "${1:-}" in
        --help|-h)
            echo "用法: $0 [选项]"
            echo ""
            echo "选项:"
            echo "  --check          显示版本信息"
            echo "  --set VERSION    设置新版本号"
            echo "  --bump TYPE      升级版本号 (patch|minor|major)"
            echo "  --help           显示帮助信息"
            ;;
        --check)
            check_version
            ;;
        --set)
            if [ -z "${2:-}" ]; then
                print_error "请提供版本号，例如: $0 --set 1.0.0"
                exit 1
            fi
            set_version "$2"
            ;;
        --bump)
            if [ -z "${2:-}" ]; then
                print_error "请指定升级类型: patch, minor, major"
                exit 1
            fi
            local current=$(get_version)
            local new_version=$(bump_version "$current" "$2")
            if [ $? -eq 0 ]; then
                set_version "$new_version"
            fi
            ;;
        *)
            check_version
            ;;
    esac
}

main "$@"
