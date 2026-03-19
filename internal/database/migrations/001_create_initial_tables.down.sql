-- 001_create_initial_tables.down.sql
-- 回滚初始表结构

DROP TABLE IF EXISTS files;
DROP TABLE IF EXISTS ignore_rules;
DROP TABLE IF EXISTS scan_paths;
DROP TABLE IF EXISTS schema_migrations;
