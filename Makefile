# ============================================================================
# jcqsearch — 本地文件快速搜索工具 Makefile
# ============================================================================

# ============================================================================
# 变量定义
# ============================================================================

# 版本信息
VERSION := $(shell cat VERSION 2>/dev/null || echo "0.0.0")
BUILD_TIME := $(shell date '+%Y-%m-%d_%H:%M:%S')
COMMIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BRANCH := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")

# Go 配置
GO := go
CGO_ENABLED := 0
BIN_NAME := jcqsearch
BIN_DIR := bin

# ldflags 注入版本信息
LDFLAGS := -ldflags="-X 'jcqsearch/cmd.Version=$(VERSION)' \
    -X 'jcqsearch/cmd.BuildTime=$(BUILD_TIME)' \
    -X 'jcqsearch/cmd.CommitSHA=$(COMMIT_SHA)' -s -w"

# 打包配置
DIST_DIR := deploy/dist

# 数据库配置（可通过环境变量覆盖）
DB_HOST ?= localhost
DB_PORT ?= 5432
DB_USER ?= shawn
DB_NAME ?= jcqsearch
DB_PASSWORD ?=

# 安装目标
INSTALL_DIR ?= $(HOME)/dev/jcsoft

# ============================================================================
# .PHONY 声明
# ============================================================================

.PHONY: all help build clean install
.PHONY: test fmt vet tidy deps check audit
.PHONY: version version-set version-bump-patch version-bump-minor version-bump-major version-check
.PHONY: package package-current package-linux-amd64 package-linux-arm64 package-darwin-amd64 package-darwin-arm64 package-windows-amd64 package-windows-arm64
.PHONY: dist-list dist-clean
.PHONY: db-migrate db-rollback db-status
.PHONY: confirm no-dirty

# ============================================================================
# 默认目标与帮助
# ============================================================================

all: build

help: ## 显示帮助信息
	@echo "jcqsearch Make 命令"
	@echo "=================================================="
	@echo "版本: $(VERSION)  提交: $(COMMIT_SHA)  分支: $(BRANCH)"
	@echo ""
	@echo "快捷命令:"
	@grep -E '^(all|help|build|clean|install|version):.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-30s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "构建与测试:"
	@grep -E '^(test|fmt|vet|tidy|deps|check|audit):.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[32m%-30s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "版本管理:"
	@grep -E '^version-[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[35m%-30s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "打包部署:"
	@grep -E '^(package|package-current|package-linux|package-darwin|package-windows|dist-list|dist-clean)[a-zA-Z0-9_-]*:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[34m%-30s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "数据库:"
	@grep -E '^db-[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[33m%-30s\033[0m %s\n", $$1, $$2}'

# ============================================================================
# 辅助目标
# ============================================================================

confirm:
	@echo -n '确定要执行吗？[y/N] ' && read ans && [ $${ans:-N} = y ]

no-dirty:
	@test -z "$$(git status --porcelain)" || (echo "存在未提交的更改"; exit 1)

# ============================================================================
# 构建
# ============================================================================

build: ## 构建 jcqsearch
	@echo "正在构建 $(BIN_NAME) $(VERSION)..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build -trimpath $(LDFLAGS) -o $(BIN_DIR)/$(BIN_NAME) .
	@echo "构建完成: $(BIN_DIR)/$(BIN_NAME)"

install: build ## 构建并安装到 ~/dev/jcsoft/
	@mkdir -p $(INSTALL_DIR)
	@cp $(BIN_DIR)/$(BIN_NAME) $(INSTALL_DIR)/$(BIN_NAME)
	@echo "已安装到: $(INSTALL_DIR)/$(BIN_NAME)"

clean: ## 清理构建产物
	@rm -rf $(BIN_DIR)
	@rm -rf build
	@echo "已清理"

# ============================================================================
# 测试与代码质量
# ============================================================================

test: ## 运行测试
	$(GO) test -v -race ./...

fmt: ## 格式化代码
	$(GO) fmt ./...

vet: ## 静态分析
	$(GO) vet ./...

tidy: ## 整理依赖
	$(GO) mod tidy -v

deps: ## 下载依赖
	$(GO) mod download
	$(GO) mod tidy

check: fmt vet test ## 完整检查（fmt + vet + test）

audit: test ## 综合代码审计
	$(GO) mod tidy -diff
	$(GO) mod verify
	@test -z "$$(gofmt -l .)" || (echo "代码未格式化"; exit 1)
	$(GO) vet ./...

# ============================================================================
# 版本管理
# ============================================================================

version: ## 显示版本信息
	@echo "版本:     $(VERSION)"
	@echo "构建时间: $(BUILD_TIME)"
	@echo "提交:     $(COMMIT_SHA)"
	@echo "分支:     $(BRANCH)"

version-set: ## 设置版本号（make version-set V=x.y.z）
	@if [ -z "$(V)" ]; then echo "用法: make version-set V=x.y.z"; exit 1; fi
	@./scripts/update-version.sh --set $(V)

version-bump-patch: ## 升级补丁版本（x.y.Z）
	@./scripts/update-version.sh --bump patch

version-bump-minor: ## 升级次版本（x.Y.z）
	@./scripts/update-version.sh --bump minor

version-bump-major: ## 升级主版本（X.y.z）
	@./scripts/update-version.sh --bump major

version-check: ## 检查版本信息
	@./scripts/update-version.sh --check

# ============================================================================
# 打包部署
# ============================================================================

package: ## 打包 6 个主要平台
	@./scripts/package.sh
	@echo ""
	@$(MAKE) dist-list

package-current: ## 打包当前平台
	@./scripts/package.sh --current

package-linux-amd64: ## 打包 Linux x86-64
	@./scripts/package.sh --platform linux-amd64

package-linux-arm64: ## 打包 Linux ARM64
	@./scripts/package.sh --platform linux-arm64

package-darwin-amd64: ## 打包 macOS Intel
	@./scripts/package.sh --platform darwin-amd64

package-darwin-arm64: ## 打包 macOS Apple Silicon
	@./scripts/package.sh --platform darwin-arm64

package-windows-amd64: ## 打包 Windows x86-64
	@./scripts/package.sh --platform windows-amd64

package-windows-arm64: ## 打包 Windows ARM64
	@./scripts/package.sh --platform windows-arm64

dist-list: ## 列出打包文件
	@echo "打包文件列表:"
	@echo "=================================================="
	@if [ -d "$(DIST_DIR)" ]; then \
		count=$$(ls -1 $(DIST_DIR)/jcqsearch_*.tar.gz $(DIST_DIR)/jcqsearch_*.zip 2>/dev/null | wc -l | tr -d ' '); \
		if [ "$$count" -gt 0 ]; then \
			ls -lh $(DIST_DIR)/jcqsearch_*.tar.gz $(DIST_DIR)/jcqsearch_*.zip 2>/dev/null | awk '{printf "  %-50s %s\n", $$NF, $$5}'; \
		else \
			echo "  (暂无打包文件)"; \
		fi; \
	else \
		echo "  (暂无打包文件)"; \
	fi
	@echo "=================================================="

dist-clean: ## 清理打包文件
	@echo "清理打包文件..."
	@rm -rf $(DIST_DIR)
	@echo "已清理"

# ============================================================================
# 数据库
# ============================================================================

db-migrate: ## 执行数据库迁移（make db-migrate M=001_create_initial_tables）
	@if [ -z "$(M)" ]; then echo "用法: make db-migrate M=001_create_initial_tables"; exit 1; fi
	PGPASSWORD=$(DB_PASSWORD) psql -h $(DB_HOST) -p $(DB_PORT) -U $(DB_USER) -d $(DB_NAME) \
		-f internal/database/migrations/$(M).up.sql

db-rollback: ## 回滚数据库迁移（make db-rollback M=001_create_initial_tables）
	@if [ -z "$(M)" ]; then echo "用法: make db-rollback M=001_create_initial_tables"; exit 1; fi
	PGPASSWORD=$(DB_PASSWORD) psql -h $(DB_HOST) -p $(DB_PORT) -U $(DB_USER) -d $(DB_NAME) \
		-f internal/database/migrations/$(M).down.sql

db-status: ## 检查数据库连接和迁移状态
	@PGPASSWORD=$(DB_PASSWORD) psql -h $(DB_HOST) -p $(DB_PORT) -U $(DB_USER) -d $(DB_NAME) \
		-c "SELECT version, name, applied_at FROM schema_migrations ORDER BY version;" 2>/dev/null \
		&& echo "" && echo "数据库连接正常" \
		|| echo "数据库连接失败"
