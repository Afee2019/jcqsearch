# jcqsearch — 本地文件快速搜索工具 设计方案

## 一、需求概述

解决 macOS Spotlight / Finder 搜索不好用的问题：
- 只扫描自己关心的几个目录，不扫全盘
- 自动忽略 `node_modules`、`.git` 等无用目录
- 支持中文文件名模糊搜索
- 支持关键词 + 文件类型 + 时间范围 + 文件大小的组合筛选
- 索引存入 PostgreSQL，搜索速度快

---

## 二、技术选型分析

### 2.1 语言选择：Go vs Python

| 维度 | Go | Python |
|------|-----|--------|
| **文件扫描性能** | goroutine 并发扫描，万级文件秒级完成 | 单线程较慢，asyncio 写法复杂 |
| **部署** | 单二进制，无依赖 | 需要 venv + 依赖安装 |
| **CLI 启动速度** | ~5ms | ~200ms（import 开销） |
| **你的熟练度** | 高（jcmdm/jcgraph/jcnp 均为 Go） | 中 |
| **PG 生态** | pgx/GORM 成熟 | psycopg2/SQLAlchemy 成熟 |
| **开发速度** | 中 | 快 |

**结论：选 Go。**

理由：这是一个需要频繁调用的 CLI 工具，启动速度和扫描性能是核心体验。Go 单二进制部署也最省心。你已有成熟的 Go + PG 技术栈（jcmdm），可以复用经验。

### 2.2 应用形态：CLI vs Web

| 维度 | CLI | Web |
|------|-----|-----|
| **搜索速度** | 终端直接出结果，最快 | 需要开浏览器，多一步 |
| **组合筛选** | 命令行参数，灵活但需记参数 | 表单界面，直观 |
| **日常使用** | 随时可用，无需启动服务 | 需要后台运行服务 |
| **结果交互** | 可以管道组合（\| grep, \| xargs） | 可以点击打开文件 |

**结论：CLI 为主，内置轻量 Web 模式。**

- 日常高频搜索用 CLI（`jcqsearch find 报告 -t pdf -after 2024-01`）
- 偶尔需要浏览式探索时启动 Web（`jcqsearch serve`），提供简洁的搜索页面
- CLI 优先开发，Web 后续按需加

---

## 三、架构设计

### 3.1 整体架构

```
┌─────────────────────────────────────────────┐
│                 jcqsearch                    │
│                                              │
│  ┌──────────┐  ┌──────────┐  ┌───────────┐  │
│  │ CLI 命令  │  │ Web 服务  │  │ 扫描引擎   │  │
│  │ (cobra)  │  │ (可选)    │  │ (scanner) │  │
│  └────┬─────┘  └────┬─────┘  └─────┬─────┘  │
│       │              │              │         │
│  ┌────▼──────────────▼──────────────▼─────┐  │
│  │            搜索服务层 (searcher)         │  │
│  └────────────────┬───────────────────────┘  │
│                   │                          │
│  ┌────────────────▼───────────────────────┐  │
│  │         PostgreSQL (jcqsearch)          │  │
│  │    pg_trgm 模糊搜索 + GIN 索引          │  │
│  └────────────────────────────────────────┘  │
└─────────────────────────────────────────────┘
```

### 3.2 核心模块

| 模块 | 职责 |
|------|------|
| **scanner** | 遍历指定目录，收集文件元信息，写入数据库 |
| **searcher** | 接收查询条件，构造 SQL，返回结果 |
| **config** | 管理扫描路径、忽略规则、数据库连接 |
| **cmd** | Cobra CLI 命令入口 |

### 3.3 项目结构

```
jcqsearch/
├── main.go
├── go.mod                  # module jcqsearch
├── config.yaml             # 配置文件
├── cmd/
│   ├── root.go             # 根命令
│   ├── scan.go             # jcqsearch scan — 扫描建立索引
│   ├── find.go             # jcqsearch find — 搜索文件
│   ├── paths.go            # jcqsearch paths — 管理扫描路径
│   └── serve.go            # jcqsearch serve — 启动 Web 界面（后续）
├── internal/
│   ├── config/
│   │   └── config.go       # 配置加载
│   ├── scanner/
│   │   └── scanner.go      # 目录扫描引擎
│   ├── searcher/
│   │   └── searcher.go     # 搜索查询
│   ├── database/
│   │   ├── db.go           # 数据库连接
│   │   └── migrations/     # SQL 迁移脚本
│   └── model/
│       └── file.go         # 数据模型
└── web/                    # Web 界面（后续）
    ├── handler.go
    └── templates/
```

---

## 四、数据库设计

### 4.1 建库

```sql
CREATE DATABASE jcqsearch
    WITH TEMPLATE = template0
    ENCODING = 'UTF8'
    LC_COLLATE = 'zh_CN.UTF-8'
    LC_CTYPE = 'zh_CN.UTF-8'
    TABLESPACE = pg_default;
```

### 4.2 启用扩展

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;  -- 模糊搜索
```

### 4.3 核心表

```sql
-- 扫描路径配置
CREATE TABLE scan_paths (
    id          SERIAL PRIMARY KEY,
    path        TEXT NOT NULL UNIQUE,       -- 绝对路径，如 /Users/shawn/dev
    label       TEXT,                        -- 备注，如 "开发目录"
    enabled     BOOLEAN DEFAULT true,
    max_depth   INT DEFAULT 0,              -- 0=无限制
    created_at  TIMESTAMPTZ DEFAULT now()
);

-- 忽略规则
CREATE TABLE ignore_rules (
    id          SERIAL PRIMARY KEY,
    pattern     TEXT NOT NULL UNIQUE,        -- 如 node_modules, .git, *.pyc
    rule_type   TEXT NOT NULL DEFAULT 'dir', -- dir=目录名, ext=扩展名, glob=通配符
    enabled     BOOLEAN DEFAULT true,
    created_at  TIMESTAMPTZ DEFAULT now()
);

-- 文件索引（核心表）
CREATE TABLE files (
    id          BIGSERIAL PRIMARY KEY,
    path        TEXT NOT NULL,              -- 完整绝对路径
    dir         TEXT NOT NULL,              -- 所在目录
    name        TEXT NOT NULL,              -- 文件名（含扩展名）
    stem        TEXT NOT NULL,              -- 文件名（不含扩展名）
    ext         TEXT NOT NULL DEFAULT '',   -- 扩展名（小写，不含点，如 pdf）
    is_dir      BOOLEAN NOT NULL DEFAULT false,
    size        BIGINT NOT NULL DEFAULT 0,  -- 字节
    mod_time    TIMESTAMPTZ NOT NULL,       -- 修改时间
    scan_path_id INT REFERENCES scan_paths(id), -- 属于哪个扫描路径
    scanned_at  TIMESTAMPTZ DEFAULT now(),  -- 本次扫描时间
    UNIQUE(path)
);

-- 索引
CREATE INDEX idx_files_name_trgm ON files USING GIN (name gin_trgm_ops);
CREATE INDEX idx_files_stem_trgm ON files USING GIN (stem gin_trgm_ops);
CREATE INDEX idx_files_ext ON files (ext);
CREATE INDEX idx_files_mod_time ON files (mod_time);
CREATE INDEX idx_files_dir ON files (dir);
CREATE INDEX idx_files_is_dir ON files (is_dir);
CREATE INDEX idx_files_size ON files (size);
CREATE INDEX idx_files_scan_path_id ON files (scan_path_id);
```

### 4.4 搜索查询示例

```sql
-- 模糊搜索文件名包含"报告"的 PDF 文件
SELECT path, name, size, mod_time
FROM files
WHERE name % '报告'                           -- pg_trgm 相似度匹配
  AND ext = 'pdf'
  AND is_dir = false
ORDER BY similarity(name, '报告') DESC, mod_time DESC
LIMIT 20;

-- 关键词 + 时间范围
SELECT path, name, size, mod_time
FROM files
WHERE name ILIKE '%数据治理%'
  AND mod_time >= '2024-01-01'
  AND mod_time < '2025-01-01'
ORDER BY mod_time DESC;

-- 查找大文件
SELECT path, name, pg_size_pretty(size), mod_time
FROM files
WHERE size > 100 * 1024 * 1024    -- > 100MB
  AND is_dir = false
ORDER BY size DESC;
```

---

## 五、CLI 命令设计

### 5.1 命令一览

```bash
jcqsearch scan                          # 扫描所有已配置路径，更新索引
jcqsearch scan /Users/shawn/dev        # 扫描指定路径（临时，不保存配置）

jcqsearch find 报告                     # 模糊搜索文件名
jcqsearch find 报告 -t pdf              # 限定类型
jcqsearch find 报告 -t pdf -after 2024-01  # 限定时间
jcqsearch find 报告 --dir /Users/shawn/dev  # 限定目录范围
jcqsearch find -t mkv --larger 100M    # 查找大视频文件
jcqsearch find --recent 7d             # 最近 7 天修改的文件

jcqsearch paths list                    # 查看已配置的扫描路径
jcqsearch paths add /Users/shawn/dev "开发目录"
jcqsearch paths add /Users/shawn/Documents "文档"
jcqsearch paths remove /old/path

jcqsearch ignore list                   # 查看忽略规则
jcqsearch ignore add node_modules dir
jcqsearch ignore add .git dir
jcqsearch ignore add "*.pyc" glob

jcqsearch stats                         # 统计信息（已索引文件数、目录数、占用空间等）

jcqsearch serve                         # 启动 Web 界面（后续）
```

### 5.2 搜索参数

| 参数 | 缩写 | 说明 | 示例 |
|------|------|------|------|
| `--type` | `-t` | 文件扩展名 | `-t pdf`, `-t "pdf,docx"` |
| `--after` | | 修改时间起始 | `--after 2024-01` |
| `--before` | | 修改时间截止 | `--before 2025-06` |
| `--dir` | `-d` | 限定搜索目录 | `-d /Users/shawn/dev` |
| `--larger` | | 最小文件大小 | `--larger 10M` |
| `--smaller` | | 最大文件大小 | `--smaller 1G` |
| `--recent` | `-r` | 最近 N 天/小时 | `-r 7d`, `-r 24h` |
| `--dirs-only` | | 仅搜索目录 | |
| `--files-only` | | 仅搜索文件 | |
| `--limit` | `-n` | 返回条数 | `-n 50`（默认 20） |
| `--open` | `-o` | 用系统默认程序打开第 N 个结果 | `-o 1` |
| `--reveal` | | 在 Finder 中显示 | `--reveal 1` |

### 5.3 输出格式

```
$ jcqsearch find 数据治理 -t pdf -after 2024-01

  #  名称                                    大小      修改时间             路径
  1  数据治理平台产品白皮书V3.pdf               2.1 MB   2024-03-15 10:30    ~/dev/jcmdm/docs/
  2  2024-数据治理方案.pdf                     856 KB   2024-02-20 14:22    ~/Documents/projects/
  3  数据治理培训材料.pdf                       5.4 MB   2024-01-08 09:15    ~/Desktop/

  共 3 条结果（耗时 12ms）
```

---

## 六、扫描引擎设计

### 6.1 扫描策略

```
全量扫描（首次 / jcqsearch scan --full）:
  1. 读取 scan_paths 表中所有 enabled=true 的路径
  2. 并发遍历各路径（每个路径一个 goroutine）
  3. 遍历时检查 ignore_rules 跳过匹配的目录/文件
  4. 批量写入 files 表（每 1000 条一批 UPSERT）
  5. 删除数据库中已不存在的文件记录（以 scanned_at 判断）

增量扫描（jcqsearch scan）:
  1. 同上遍历，但对比 mod_time 与数据库记录
  2. 仅 UPSERT 有变化的文件
  3. 清理已删除的文件
```

### 6.2 默认忽略规则

预置以下忽略规则，用户可按需增删：

```
目录: node_modules, .git, .svn, __pycache__, .venv, .tox,
      .next, .nuxt, dist, build, .cache, .DS_Store,
      vendor (可选), target (可选)

扩展名: .pyc, .pyo, .o, .so, .dylib, .class
```

### 6.3 性能预估

以 `~/dev` 目录（~5 万文件，排除 node_modules 等）为例：
- 首次全量扫描：约 3-8 秒（Go 并发 + 批量 UPSERT）
- 增量扫描：约 1-2 秒
- 搜索查询：< 50ms（pg_trgm GIN 索引）

---

## 七、配置文件

```yaml
# ~/.config/jcqsearch/config.yaml

database:
  host: localhost
  port: 5432
  dbname: jcqsearch
  user: shawn
  password: Mark2019
  sslmode: disable

scan:
  batch_size: 1000        # 批量写入条数
  concurrency: 4          # 并发扫描数

search:
  default_limit: 20       # 默认返回条数
  similarity_threshold: 0.1  # pg_trgm 相似度阈值（越低越宽松）
```

---

## 八、开发路线

### Phase 1：核心功能（MVP）

- [x] 项目骨架 + Cobra CLI
- [ ] 数据库建库 + 迁移脚本
- [ ] `scan` 命令：全量扫描 + 忽略规则
- [ ] `find` 命令：关键词模糊搜索 + 类型/时间筛选
- [ ] `paths` / `ignore` 命令：管理配置
- [ ] `stats` 命令

### Phase 2：体验优化

- [ ] 增量扫描（对比 mod_time）
- [ ] `--open` / `--reveal` 快捷操作
- [ ] 搜索结果高亮匹配关键词
- [ ] 搜索历史

### Phase 3：Web 界面（可选）

- [ ] `serve` 命令启动 HTTP 服务
- [ ] 简洁搜索页面（单页应用，无需前端框架）
- [ ] 实时搜索（输入即查）

---

## 九、技术依赖

| 依赖 | 用途 |
|------|------|
| `github.com/spf13/cobra` | CLI 框架 |
| `github.com/spf13/viper` | 配置管理 |
| `github.com/jackc/pgx/v5` | PostgreSQL 驱动（高性能） |
| `github.com/fatih/color` | 终端彩色输出 |
| `github.com/dustin/go-humanize` | 文件大小格式化 |

不使用 GORM，直接用 pgx 写 SQL —— 对于这种查询密集型工具，原生 SQL 更灵活、性能更好。

---

## 十、总结

| 决策 | 选择 | 理由 |
|------|------|------|
| 语言 | **Go** | 单二进制、启动快、扫描性能好、你最熟练 |
| 形态 | **CLI 为主** | 搜索工具追求快，终端随时可用 |
| 数据库 | **PostgreSQL + pg_trgm** | 本机已有，模糊搜索开箱即用 |
| CLI 框架 | **Cobra** | 你已有使用经验（jcnp） |
| PG 驱动 | **pgx** | 性能优于 lib/pq，功能更全 |
| ORM | **不用** | 直接 SQL，更灵活 |
