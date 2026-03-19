-- 001_create_initial_tables.up.sql
-- 创建 jcqsearch 初始表结构

-- 迁移记录表
CREATE TABLE schema_migrations (
    version     TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    checksum    TEXT
);

COMMENT ON TABLE schema_migrations IS '数据库迁移版本记录表';

-- 扫描路径配置表
CREATE TABLE scan_paths (
    id          SERIAL PRIMARY KEY,
    path        TEXT NOT NULL,
    label       TEXT DEFAULT '',
    enabled     BOOLEAN NOT NULL DEFAULT true,
    max_depth   INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_scan_paths_path UNIQUE (path),
    CONSTRAINT ck_scan_paths_max_depth CHECK (max_depth >= 0)
);

COMMENT ON TABLE scan_paths IS '扫描路径配置表';

-- 忽略规则表
CREATE TABLE ignore_rules (
    id          SERIAL PRIMARY KEY,
    pattern     TEXT NOT NULL,
    rule_type   TEXT NOT NULL DEFAULT 'dir',
    enabled     BOOLEAN NOT NULL DEFAULT true,
    is_default  BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_ignore_rules_pattern_type UNIQUE (pattern, rule_type),
    CONSTRAINT ck_ignore_rules_type CHECK (rule_type IN ('dir', 'ext', 'glob'))
);

COMMENT ON TABLE ignore_rules IS '扫描忽略规则表';

-- 文件索引表
CREATE TABLE files (
    id           BIGSERIAL PRIMARY KEY,
    path         TEXT NOT NULL,
    dir          TEXT NOT NULL,
    name         TEXT NOT NULL,
    stem         TEXT NOT NULL,
    ext          TEXT NOT NULL DEFAULT '',
    is_dir       BOOLEAN NOT NULL DEFAULT false,
    size         BIGINT NOT NULL DEFAULT 0,
    mod_time     TIMESTAMPTZ NOT NULL,
    scan_path_id INT NOT NULL,
    scanned_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_files_path UNIQUE (path),
    CONSTRAINT fk_files_scan_path FOREIGN KEY (scan_path_id)
        REFERENCES scan_paths(id) ON DELETE CASCADE
);

COMMENT ON TABLE files IS '文件索引表';

-- 索引
CREATE INDEX idx_files_name_trgm ON files USING GIN (name gin_trgm_ops);
CREATE INDEX idx_files_stem_trgm ON files USING GIN (stem gin_trgm_ops);
CREATE INDEX idx_files_ext ON files (ext);
CREATE INDEX idx_files_mod_time ON files (mod_time);
CREATE INDEX idx_files_dir ON files USING BTREE (dir text_pattern_ops);
CREATE INDEX idx_files_is_dir ON files (is_dir);
CREATE INDEX idx_files_size ON files (size);
CREATE INDEX idx_files_scan_path_id ON files (scan_path_id);
CREATE INDEX idx_files_cleanup ON files (scan_path_id, scanned_at);

-- 预置忽略规则
INSERT INTO ignore_rules (pattern, rule_type, is_default) VALUES
    ('.git',           'dir', true),
    ('.svn',           'dir', true),
    ('.hg',            'dir', true),
    ('node_modules',   'dir', true),
    ('vendor',         'dir', true),
    ('.venv',          'dir', true),
    ('__pycache__',    'dir', true),
    ('dist',           'dir', true),
    ('build',          'dir', true),
    ('target',         'dir', true),
    ('.next',          'dir', true),
    ('.nuxt',          'dir', true),
    ('.cache',         'dir', true),
    ('.tox',           'dir', true),
    ('.pytest_cache',  'dir', true),
    ('.Spotlight-V100','dir', true),
    ('.Trashes',       'dir', true),
    ('.DS_Store',      'glob', true),
    ('pyc',            'ext', true),
    ('pyo',            'ext', true),
    ('o',              'ext', true),
    ('so',             'ext', true),
    ('dylib',          'ext', true),
    ('class',          'ext', true)
ON CONFLICT (pattern, rule_type) DO NOTHING;
