package searcher

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"jcqsearch/internal/model"
)

type SearchParams struct {
	Keyword   string
	Exts      []string   // 扩展名列表（小写，不含点号）
	After     *time.Time // 修改时间起始
	Before    *time.Time // 修改时间截止
	Dir       string     // 限定目录前缀
	MinSize   int64      // 最小文件大小（字节）
	MaxSize   int64      // 最大文件大小（字节）
	Tags      []string   // 标签筛选（AND）
	DirsOnly  bool
	FilesOnly bool
	Limit     int
}

type SearchResult struct {
	Entries  []model.FileEntry
	Total    int
	Duration time.Duration
	FileTags map[int64][]string // file_id -> tag names
}

type Searcher struct {
	pool      *pgxpool.Pool
	threshold float64
}

func New(pool *pgxpool.Pool, threshold float64) *Searcher {
	return &Searcher{pool: pool, threshold: threshold}
}

func (s *Searcher) Search(ctx context.Context, params SearchParams) (*SearchResult, error) {
	start := time.Now()

	// 使用事务 + SET LOCAL 控制 similarity_threshold，确保 % 运算符走 GIN 索引
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback(ctx)

	if params.Keyword != "" {
		_, err := tx.Exec(ctx,
			fmt.Sprintf("SET LOCAL pg_trgm.similarity_threshold = %f", s.threshold))
		if err != nil {
			return nil, fmt.Errorf("设置相似度阈值失败: %w", err)
		}
	}

	var conditions []string
	var args []any
	argIdx := 1

	// 关键词模糊匹配：% 运算符利用 GIN 索引加速，阈值由 SET LOCAL 控制
	if params.Keyword != "" {
		conditions = append(conditions,
			fmt.Sprintf("stem %% $%d", argIdx))
		args = append(args, params.Keyword)
		argIdx++
	}

	// 扩展名
	if len(params.Exts) > 0 {
		if len(params.Exts) == 1 {
			conditions = append(conditions, fmt.Sprintf("ext = $%d", argIdx))
			args = append(args, params.Exts[0])
		} else {
			conditions = append(conditions, fmt.Sprintf("ext = ANY($%d)", argIdx))
			args = append(args, params.Exts)
		}
		argIdx++
	}

	// 时间范围
	if params.After != nil {
		conditions = append(conditions, fmt.Sprintf("mod_time >= $%d", argIdx))
		args = append(args, *params.After)
		argIdx++
	}
	if params.Before != nil {
		conditions = append(conditions, fmt.Sprintf("mod_time < $%d", argIdx))
		args = append(args, *params.Before)
		argIdx++
	}

	// 目录限定
	if params.Dir != "" {
		conditions = append(conditions, fmt.Sprintf("dir LIKE $%d", argIdx))
		args = append(args, params.Dir+"%")
		argIdx++
	}

	// 文件大小
	if params.MinSize > 0 {
		conditions = append(conditions, fmt.Sprintf("size >= $%d", argIdx))
		args = append(args, params.MinSize)
		argIdx++
	}
	if params.MaxSize > 0 {
		conditions = append(conditions, fmt.Sprintf("size <= $%d", argIdx))
		args = append(args, params.MaxSize)
		argIdx++
	}

	// 标签筛选
	if len(params.Tags) > 0 {
		conditions = append(conditions, fmt.Sprintf(`id IN (
			SELECT ft.file_id FROM file_tags ft
			JOIN tags t ON t.id = ft.tag_id
			WHERE t.name = ANY($%d)
			GROUP BY ft.file_id
			HAVING COUNT(DISTINCT t.id) = $%d
		)`, argIdx, argIdx+1))
		args = append(args, params.Tags, len(params.Tags))
		argIdx += 2
	}

	// 文件/目录筛选
	if params.FilesOnly {
		conditions = append(conditions, "is_dir = false")
	} else if params.DirsOnly {
		conditions = append(conditions, "is_dir = true")
	}

	// 构造 SQL
	query := "SELECT id, path, dir, name, stem, ext, is_dir, size, mod_time FROM files"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// ORDER BY
	if params.Keyword != "" {
		query += " ORDER BY similarity(stem, $1) DESC, mod_time DESC"
	} else {
		query += " ORDER BY mod_time DESC"
	}

	// LIMIT
	query += fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, params.Limit)

	// 执行查询
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("搜索查询失败: %w", err)
	}
	defer rows.Close()

	var entries []model.FileEntry
	for rows.Next() {
		var f model.FileEntry
		if err := rows.Scan(&f.ID, &f.Path, &f.Dir, &f.Name, &f.Stem, &f.Ext, &f.IsDir, &f.Size, &f.ModTime); err != nil {
			return nil, fmt.Errorf("解析查询结果失败: %w", err)
		}
		entries = append(entries, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 加载标签
	fileTags, _ := s.loadFileTags(ctx, entries)

	return &SearchResult{
		Entries:  entries,
		Total:    len(entries),
		Duration: time.Since(start),
		FileTags: fileTags,
	}, nil
}

// loadFileTags 批量加载文件的标签
func (s *Searcher) loadFileTags(ctx context.Context, entries []model.FileEntry) (map[int64][]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	ids := make([]int64, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}

	rows, err := s.pool.Query(ctx, `
		SELECT ft.file_id, t.name
		FROM file_tags ft
		JOIN tags t ON t.id = ft.tag_id
		WHERE ft.file_id = ANY($1)
		ORDER BY t.name`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64][]string)
	for rows.Next() {
		var fileID int64
		var tagName string
		if err := rows.Scan(&fileID, &tagName); err != nil {
			return nil, err
		}
		result[fileID] = append(result[fileID], tagName)
	}
	return result, rows.Err()
}
