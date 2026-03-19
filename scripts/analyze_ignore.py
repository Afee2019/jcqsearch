#!/usr/bin/env python3
"""
分析 ~/ 下的目录结构，找出应该被忽略的路径。

分析维度：
1. 已知的通用忽略目录（包管理、构建产物、缓存等）
2. 文件数量异常多的目录（可能是依赖/缓存）
3. 隐藏目录（.开头）的空间占用
"""

import os
import sys
from collections import defaultdict
from pathlib import Path

# ============ 已知应忽略的目录模式 ============

KNOWN_IGNORE_DIRS = {
    # 包管理 / 依赖
    "node_modules", "bower_components", ".pnpm-store",
    "vendor", ".venv", "venv", "env", ".env",
    "__pycache__", ".eggs", "*.egg-info",
    "Pods", "Carthage",
    ".gradle", ".m2", ".ivy2",
    ".cargo", ".rustup",
    ".gem", ".bundle",
    ".pub-cache", ".dart_tool",

    # 构建产物
    "dist", "build", "target", "out", "output",
    ".next", ".nuxt", ".svelte-kit",
    ".parcel-cache", ".turbo",
    "cmake-build-debug", "cmake-build-release",
    "DerivedData",

    # 版本控制
    ".git", ".svn", ".hg",

    # 缓存 / 临时
    ".cache", ".tox", ".pytest_cache", ".mypy_cache", ".ruff_cache",
    ".eslintcache", ".stylelintcache",
    "__MACOSX", ".Spotlight-V100", ".Trashes", ".fseventsd",
    ".TemporaryItems",

    # IDE / 编辑器
    ".idea", ".vscode", ".vs",
    ".eclipse", ".settings",
    "*.xcworkspace", "*.xcodeproj",

    # 虚拟机 / 容器
    ".vagrant", ".docker",

    # 其他
    ".terraform", ".serverless",
    "coverage", ".nyc_output", "htmlcov",
    ".tsbuildinfo",
}

# 已经在 jcqsearch 预置规则中的
ALREADY_IGNORED = {
    ".git", ".svn", ".hg",
    "node_modules", "vendor", ".venv", "__pycache__",
    "dist", "build", "target", ".next", ".nuxt",
    ".cache", ".tox", ".pytest_cache",
    ".Spotlight-V100", ".Trashes", ".DS_Store",
}

# macOS 应用和系统目录（通常不需要索引）
MACOS_SKIP = {
    "Library", "Applications", ".Trash",
    "Movies",  # 通常很大且用专门工具管理
}


def human_size(size_bytes):
    """格式化文件大小"""
    for unit in ["B", "KB", "MB", "GB", "TB"]:
        if abs(size_bytes) < 1024:
            return f"{size_bytes:.1f} {unit}"
        size_bytes /= 1024
    return f"{size_bytes:.1f} PB"


def count_entries(path, max_depth=1):
    """快速统计目录下的条目数（不递归太深）"""
    count = 0
    try:
        for entry in os.scandir(path):
            count += 1
            if count > 50000:  # 防止卡住
                return count
    except PermissionError:
        return -1
    return count


def dir_size_fast(path, max_files=10000):
    """快速估算目录大小（限制遍历文件数）"""
    total = 0
    file_count = 0
    try:
        for root, dirs, files in os.walk(path):
            for f in files:
                fp = os.path.join(root, f)
                try:
                    total += os.lstat(fp).st_size
                except (OSError, PermissionError):
                    pass
                file_count += 1
                if file_count >= max_files:
                    return total, file_count, True  # truncated
    except PermissionError:
        pass
    return total, file_count, False


def analyze_home():
    home = Path.home()
    print(f"\n{'='*70}")
    print(f"  分析目录: {home}")
    print(f"{'='*70}\n")

    # ============ 第 1 部分：扫描一级目录 ============
    print("━" * 70)
    print("  一、一级目录概览（建议哪些目录值得索引）")
    print("━" * 70)
    print()
    print(f"  {'目录':<30} {'条目数':>8}  {'建议'}")
    print(f"  {'─'*28}  {'─'*8}  {'─'*25}")

    top_dirs = []
    for entry in sorted(os.scandir(home), key=lambda e: e.name):
        if not entry.is_dir(follow_symlinks=False):
            continue
        name = entry.name
        count = count_entries(entry.path)
        top_dirs.append((name, entry.path, count))

        if name in MACOS_SKIP:
            suggestion = "⊘ 系统/大型目录，建议不索引"
        elif name.startswith("."):
            suggestion = "⊘ 隐藏目录，通常不需要"
        elif name in ("Desktop", "Documents", "Downloads", "dev", "work", "projects", "src", "code"):
            suggestion = "★ 推荐索引"
        elif name in ("Public", "Sites"):
            suggestion = "○ 可选"
        else:
            suggestion = "○ 按需决定"

        count_str = f"{count:,}" if count >= 0 else "无权限"
        print(f"  {name:<30} {count_str:>8}  {suggestion}")

    # ============ 第 2 部分：发现已知忽略目录 ============
    print()
    print("━" * 70)
    print("  二、发现的已知忽略目录（尚未在 jcqsearch 预置规则中）")
    print("━" * 70)
    print()

    # 要扫描的一级目录
    scan_candidates = []
    for name, path, count in top_dirs:
        if name not in MACOS_SKIP and not name.startswith("."):
            scan_candidates.append(path)

    found_ignore = defaultdict(list)  # pattern -> [paths]
    new_patterns = set()

    for scan_root in scan_candidates:
        try:
            for root, dirs, files in os.walk(scan_root):
                # 限制深度为 5
                depth = root[len(scan_root):].count(os.sep)
                if depth > 5:
                    dirs.clear()
                    continue

                # 检查子目录名
                for d in list(dirs):
                    if d in KNOWN_IGNORE_DIRS:
                        full = os.path.join(root, d)
                        found_ignore[d].append(full)
                        if d not in ALREADY_IGNORED:
                            new_patterns.add(d)
                        dirs.remove(d)  # 不再递归进去

                    # 跳过已知忽略，加速扫描
                    elif d in ALREADY_IGNORED:
                        dirs.remove(d)

        except PermissionError:
            continue

    if new_patterns:
        print(f"  {'目录名':<25} {'类型':<8} {'出现次数':>8}  示例路径")
        print(f"  {'─'*23}  {'─'*6}  {'─'*8}  {'─'*30}")

        for pattern in sorted(new_patterns):
            paths = found_ignore[pattern]
            print(f"  {pattern:<25} {'dir':<8} {len(paths):>8}  {paths[0]}")
            for p in paths[1:3]:  # 最多再显示 2 个
                print(f"  {'':25} {'':8} {'':8}  {p}")
    else:
        print("  未发现新的忽略目录模式。")

    # ============ 第 3 部分：隐藏目录空间分析 ============
    print()
    print("━" * 70)
    print("  三、~/ 下隐藏目录空间占用（TOP 20）")
    print("━" * 70)
    print()

    hidden_dirs = []
    for entry in os.scandir(home):
        if entry.is_dir(follow_symlinks=False) and entry.name.startswith("."):
            size, fc, truncated = dir_size_fast(entry.path, max_files=50000)
            hidden_dirs.append((entry.name, size, fc, truncated))

    hidden_dirs.sort(key=lambda x: x[1], reverse=True)

    print(f"  {'目录':<30} {'大小':>12} {'文件数':>10}  说明")
    print(f"  {'─'*28}  {'─'*12} {'─'*10}  {'─'*20}")

    for name, size, fc, truncated in hidden_dirs[:20]:
        size_str = human_size(size)
        fc_str = f"{fc:,}+" if truncated else f"{fc:,}"
        note = ""
        if name in (".cache", ".npm", ".pnpm-store", ".cargo", ".rustup", ".gradle"):
            note = "← 缓存/依赖，可忽略"
        elif name in (".git",):
            note = "← 已忽略"
        elif name in (".Trash", ".Trashes"):
            note = "← 回收站"
        elif size > 1024 * 1024 * 1024:
            note = "← 较大，建议检查"
        print(f"  {name:<30} {size_str:>12} {fc_str:>10}  {note}")

    # ============ 第 4 部分：文件数异常多的目录 ============
    print()
    print("━" * 70)
    print("  四、文件数异常多的目录 TOP 20（可能是依赖/缓存/日志）")
    print("━" * 70)
    print()

    heavy_dirs = []

    for scan_root in scan_candidates:
        try:
            for root, dirs, files in os.walk(scan_root):
                depth = root[len(scan_root):].count(os.sep)
                if depth > 6:
                    dirs.clear()
                    continue

                # 跳过已知忽略目录
                dirs[:] = [d for d in dirs if d not in ALREADY_IGNORED and d not in new_patterns]

                total = len(files) + len(dirs)
                if total > 500:
                    heavy_dirs.append((root, total))

        except PermissionError:
            continue

    heavy_dirs.sort(key=lambda x: x[1], reverse=True)

    print(f"  {'路径':<55} {'条目数':>8}")
    print(f"  {'─'*53}  {'─'*8}")

    for path, count in heavy_dirs[:20]:
        # 缩短路径显示
        display = path.replace(str(home), "~")
        if len(display) > 53:
            display = "..." + display[-50:]
        print(f"  {display:<55} {count:>8,}")

    # ============ 第 5 部分：建议操作 ============
    print()
    print("━" * 70)
    print("  五、建议的 jcqsearch ignore add 命令")
    print("━" * 70)
    print()

    if new_patterns:
        for pattern in sorted(new_patterns):
            count = len(found_ignore[pattern])
            print(f"  jcqsearch ignore add {pattern} dir    # 出现 {count} 次")
    else:
        print("  当前忽略规则已较完善，无新增建议。")

    print()


if __name__ == "__main__":
    analyze_home()
