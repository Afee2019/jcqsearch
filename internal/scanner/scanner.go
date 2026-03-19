package scanner

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"jcqsearch/internal/config"
	"jcqsearch/internal/model"
)

type Scanner struct {
	pool      *pgxpool.Pool
	batchSize int
	conc      int
	ignore    config.IgnoreConfig
}

type ScanResult struct {
	Path      string
	FileCount int64
	Duration  time.Duration
}

func New(pool *pgxpool.Pool, batchSize, concurrency int, ignore config.IgnoreConfig) *Scanner {
	return &Scanner{
		pool:      pool,
		batchSize: batchSize,
		conc:      concurrency,
		ignore:    ignore,
	}
}

// ScanAll 扫描所有已启用的扫描路径
func (s *Scanner) ScanAll(ctx context.Context) ([]ScanResult, error) {
	paths, err := s.loadScanPaths(ctx)
	if err != nil {
		return nil, fmt.Errorf("加载扫描路径失败: %w", err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("未配置扫描路径，请先运行: jcqsearch paths add <目录路径>")
	}

	scanTime := time.Now()
	results := make([]ScanResult, len(paths))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.conc)

	for i, sp := range paths {
		g.Go(func() error {
			start := time.Now()
			count, err := s.scanPath(gctx, sp, scanTime)
			if err != nil {
				return fmt.Errorf("扫描 %s 失败: %w", sp.Path, err)
			}
			results[i] = ScanResult{
				Path:      sp.Path,
				FileCount: count,
				Duration:  time.Since(start),
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return results, nil
}

// ScanOne 扫描单个指定路径（临时扫描，需先确保 scan_path 存在）
func (s *Scanner) ScanOne(ctx context.Context, scanPath model.ScanPath) (*ScanResult, error) {
	scanTime := time.Now()
	start := time.Now()
	count, err := s.scanPath(ctx, scanPath, scanTime)
	if err != nil {
		return nil, err
	}

	return &ScanResult{
		Path:      scanPath.Path,
		FileCount: count,
		Duration:  time.Since(start),
	}, nil
}

func (s *Scanner) scanPath(ctx context.Context, sp model.ScanPath, scanTime time.Time) (int64, error) {
	var count atomic.Int64
	var batch []model.FileEntry

	err := filepath.WalkDir(sp.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// 跳过无权限的目录
			fmt.Fprintf(os.Stderr, "  警告: 无法访问 %s: %v\n", path, err)
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		name := d.Name()

		// 检查忽略规则
		if d.IsDir() {
			if s.shouldIgnoreDir(name) {
				return filepath.SkipDir
			}
		} else {
			if s.shouldIgnoreFile(name) {
				return nil
			}
		}

		// 检查深度限制
		if sp.MaxDepth > 0 {
			rel, _ := filepath.Rel(sp.Path, path)
			depth := strings.Count(rel, string(filepath.Separator))
			if depth > sp.MaxDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		info, err := d.Info()
		if err != nil {
			return nil // 跳过无法获取信息的文件
		}

		entry := model.FileEntry{
			Path:       path,
			Dir:        filepath.Dir(path),
			Name:       name,
			Stem:       stemOf(name),
			Ext:        extOf(name),
			IsDir:      d.IsDir(),
			Size:       info.Size(),
			ModTime:    info.ModTime(),
			ScanPathID: sp.ID,
			ScannedAt:  scanTime,
		}

		batch = append(batch, entry)
		count.Add(1)

		if len(batch) >= s.batchSize {
			if err := s.upsertBatch(ctx, batch); err != nil {
				return fmt.Errorf("批量写入失败: %w", err)
			}
			fmt.Fprintf(os.Stderr, "\r  扫描中: %d 个文件 ...", count.Load())
			batch = batch[:0]
		}
		return nil
	})

	if err != nil {
		return 0, err
	}

	// 写入最后一批
	if len(batch) > 0 {
		if err := s.upsertBatch(ctx, batch); err != nil {
			return 0, fmt.Errorf("批量写入失败: %w", err)
		}
	}

	// 清理已删除文件
	deleted, err := s.cleanupDeleted(ctx, sp.ID, scanTime)
	if err != nil {
		return 0, fmt.Errorf("清理过期记录失败: %w", err)
	}
	if deleted > 0 {
		fmt.Fprintf(os.Stderr, "\r  清理已删除文件: %d 条\n", deleted)
	}

	return count.Load(), nil
}

func (s *Scanner) upsertBatch(ctx context.Context, entries []model.FileEntry) error {
	if len(entries) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(`INSERT INTO files (path, dir, name, stem, ext, is_dir, size, mod_time, scan_path_id, scanned_at) VALUES `)

	args := make([]any, 0, len(entries)*10)
	for i, e := range entries {
		if i > 0 {
			b.WriteString(",")
		}
		base := i * 10
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5,
			base+6, base+7, base+8, base+9, base+10)
		args = append(args, e.Path, e.Dir, e.Name, e.Stem, e.Ext,
			e.IsDir, e.Size, e.ModTime, e.ScanPathID, e.ScannedAt)
	}

	b.WriteString(` ON CONFLICT (path) DO UPDATE SET
		dir=EXCLUDED.dir, name=EXCLUDED.name, stem=EXCLUDED.stem,
		ext=EXCLUDED.ext, is_dir=EXCLUDED.is_dir, size=EXCLUDED.size,
		mod_time=EXCLUDED.mod_time, scan_path_id=EXCLUDED.scan_path_id,
		scanned_at=EXCLUDED.scanned_at`)

	_, err := s.pool.Exec(ctx, b.String(), args...)
	return err
}

func (s *Scanner) cleanupDeleted(ctx context.Context, scanPathID int, scanTime time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		"DELETE FROM files WHERE scan_path_id = $1 AND scanned_at < $2",
		scanPathID, scanTime)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Scanner) loadScanPaths(ctx context.Context) ([]model.ScanPath, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT id, path, label, enabled, max_depth, created_at, updated_at FROM scan_paths WHERE enabled = true ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []model.ScanPath
	for rows.Next() {
		var sp model.ScanPath
		if err := rows.Scan(&sp.ID, &sp.Path, &sp.Label, &sp.Enabled, &sp.MaxDepth, &sp.CreatedAt, &sp.UpdatedAt); err != nil {
			return nil, err
		}
		paths = append(paths, sp)
	}
	return paths, rows.Err()
}

// shouldIgnoreDir 检查目录是否应该被忽略（基于配置文件）
func (s *Scanner) shouldIgnoreDir(name string) bool {
	for _, d := range s.ignore.Dirs {
		if name == d {
			return true
		}
	}
	for _, g := range s.ignore.Globs {
		if matched, _ := filepath.Match(g, name); matched {
			return true
		}
	}
	return false
}

// shouldIgnoreFile 检查文件是否应该被忽略（基于配置文件）
func (s *Scanner) shouldIgnoreFile(name string) bool {
	ext := extOf(name)
	for _, e := range s.ignore.Exts {
		if ext == e {
			return true
		}
	}
	for _, g := range s.ignore.Globs {
		if matched, _ := filepath.Match(g, name); matched {
			return true
		}
	}
	return false
}

// stemOf 提取不含扩展名的文件名
func stemOf(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return name
	}
	return strings.TrimSuffix(name, ext)
}

// extOf 提取小写扩展名（不含点号）
func extOf(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(ext, "."))
}
