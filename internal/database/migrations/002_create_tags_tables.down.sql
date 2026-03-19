-- 002_create_tags_tables.down.sql

DROP TABLE IF EXISTS file_tags;
DROP TABLE IF EXISTS tags;

ALTER TABLE files DROP COLUMN IF EXISTS partial_hash;
ALTER TABLE files DROP COLUMN IF EXISTS missing_since;
