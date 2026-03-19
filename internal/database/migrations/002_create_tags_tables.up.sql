-- 002_create_tags_tables.up.sql
-- 创建文件标签相关表

-- 标签定义表
CREATE TABLE tags (
    id          SERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    color       TEXT DEFAULT '',
    description TEXT DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_tags_name UNIQUE (name)
);

CREATE INDEX idx_tags_name_trgm ON tags USING GIN (name gin_trgm_ops);

COMMENT ON TABLE tags IS '标签定义表';

-- 文件-标签关联表
CREATE TABLE file_tags (
    file_id     BIGINT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    tag_id      INT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    tagged_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    source      TEXT NOT NULL DEFAULT 'manual',

    PRIMARY KEY (file_id, tag_id)
);

CREATE INDEX idx_file_tags_tag_id ON file_tags (tag_id);
CREATE INDEX idx_file_tags_source ON file_tags (source);

COMMENT ON TABLE file_tags IS '文件-标签关联表';
COMMENT ON COLUMN file_tags.source IS '标签来源：manual=手动, rule=自动规则, import=导入';

-- files 表增加指纹字段
ALTER TABLE files ADD COLUMN partial_hash TEXT DEFAULT '';
ALTER TABLE files ADD COLUMN missing_since TIMESTAMPTZ DEFAULT NULL;

CREATE INDEX idx_files_identity ON files (size, partial_hash) WHERE partial_hash <> '';

COMMENT ON COLUMN files.partial_hash IS '文件指纹（前 8KB 的 SHA-256），打标签时计算';
COMMENT ON COLUMN files.missing_since IS '文件在文件系统中未找到的时间，NULL=正常存在';
