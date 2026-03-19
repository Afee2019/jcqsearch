package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"jcqsearch/internal/searcher"
)

var tagCmd = &cobra.Command{
	Use:   "tag <path|tags...> [tags...]",
	Short: "给文件打标签或查看标签",
	Long: `给文件打标签，或批量打标签。

单文件:
  jcqsearch tag ~/Desktop/报告.pdf 重要 work        # 打标签
  jcqsearch tag ~/Desktop/报告.pdf                  # 查看标签

批量打标签（复用搜索参数）:
  jcqsearch tag -t pdf document                    # 所有 PDF 标为 document
  jcqsearch tag -d ~/Desktop -t pptx 演示文稿        # 桌面上的 pptx
  jcqsearch tag --query "数据治理" project:数据治理    # 按关键词批量
  jcqsearch tag --recent 7d -t pdf 最近文档          # 最近 7 天的 PDF`,
	Args: cobra.MinimumNArgs(1),
	RunE: runTag,
}

func init() {
	f := tagCmd.Flags()
	f.String("query", "", "搜索关键词（批量模式）")
	f.StringP("type", "t", "", "文件类型筛选（批量模式）")
	f.StringP("dir", "d", "", "目录范围（批量模式）")
	f.StringP("recent", "r", "", "最近 N 天/小时（批量模式）")
	f.String("after", "", "修改时间起始（批量模式）")
	f.String("before", "", "修改时间截止（批量模式）")
	f.String("larger", "", "最小文件大小（批量模式）")
	f.String("smaller", "", "最大文件大小（批量模式）")
	f.BoolP("yes", "y", false, "跳过确认")
	rootCmd.AddCommand(tagCmd)
}

func runTag(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// 检测批量模式：如果有搜索相关 flag 被设置
	isBatch := cmd.Flags().Changed("query") || cmd.Flags().Changed("type") ||
		cmd.Flags().Changed("dir") || cmd.Flags().Changed("recent") ||
		cmd.Flags().Changed("after") || cmd.Flags().Changed("before") ||
		cmd.Flags().Changed("larger") || cmd.Flags().Changed("smaller")

	if isBatch {
		return runBatchTag(cmd, args)
	}

	absPath, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("解析路径失败: %w", err)
	}

	// 查找文件
	var fileID int64
	var fileName string
	var fileSize int64
	var modTime time.Time
	var partialHash string
	err = pool.QueryRow(ctx,
		"SELECT id, name, size, mod_time, partial_hash FROM files WHERE path = $1",
		absPath).Scan(&fileID, &fileName, &fileSize, &modTime, &partialHash)
	if err != nil {
		return fmt.Errorf("文件未在索引中: %s\n请先运行 jcqsearch scan", absPath)
	}

	// 无标签参数 → 查看标签
	if len(args) == 1 {
		return showFileTags(ctx, fileID, absPath, fileSize, modTime)
	}

	// 计算 partial_hash（如果还没有）
	if partialHash == "" {
		hash, err := computePartialHash(absPath)
		if err == nil && hash != "" {
			pool.Exec(ctx, "UPDATE files SET partial_hash = $1 WHERE id = $2", hash, fileID)
		}
	}

	// 打标签
	tagNames := args[1:]
	for _, name := range tagNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		// 自动创建标签
		var tagID int
		err := pool.QueryRow(ctx,
			"INSERT INTO tags (name) VALUES ($1) ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name RETURNING id",
			name).Scan(&tagID)
		if err != nil {
			return fmt.Errorf("创建标签失败: %w", err)
		}

		// 创建关联
		_, err = pool.Exec(ctx,
			"INSERT INTO file_tags (file_id, tag_id, source) VALUES ($1, $2, 'manual') ON CONFLICT DO NOTHING",
			fileID, tagID)
		if err != nil {
			return fmt.Errorf("打标签失败: %w", err)
		}
	}

	// 输出结果
	green := color.New(color.FgGreen).SprintFunc()
	fmt.Printf("\n已标记: %s\n", fileName)
	for _, name := range tagNames {
		fmt.Printf("  %s %s\n", green("+"), name)
	}

	// 显示当前全部标签
	allTags, _ := getFileTags(ctx, fileID)
	if len(allTags) > 0 {
		fmt.Printf("  当前标签: %s\n", strings.Join(allTags, ", "))
	}
	fmt.Println()

	return nil
}

func showFileTags(ctx context.Context, fileID int64, path string, size int64, modTime time.Time) error {
	dim := color.New(color.Faint).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()

	homeDir, _ := os.UserHomeDir()
	displayPath := path
	if homeDir != "" && strings.HasPrefix(displayPath, homeDir) {
		displayPath = "~" + displayPath[len(homeDir):]
	}

	fmt.Printf("\n文件: %s\n\n", displayPath)

	rows, err := pool.Query(ctx, `
		SELECT t.name, ft.source, ft.tagged_at
		FROM file_tags ft
		JOIN tags t ON t.id = ft.tag_id
		WHERE ft.file_id = $1
		ORDER BY ft.tagged_at`, fileID)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var name, source string
		var taggedAt time.Time
		if err := rows.Scan(&name, &source, &taggedAt); err != nil {
			return err
		}

		sourceStr := "手动"
		switch source {
		case "rule":
			sourceStr = "规则"
		case "import":
			sourceStr = "导入"
		}

		fmt.Printf("  %-20s  %s  %s\n",
			cyan(name),
			dim(sourceStr),
			dim(taggedAt.Format("2006-01-02")))
		count++
	}

	if count == 0 {
		fmt.Printf("  %s\n", dim("（无标签）"))
	}
	fmt.Println()

	return rows.Err()
}

func getFileTags(ctx context.Context, fileID int64) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT t.name FROM file_tags ft
		JOIN tags t ON t.id = ft.tag_id
		WHERE ft.file_id = $1
		ORDER BY t.name`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tags = append(tags, name)
	}
	return tags, rows.Err()
}

func computePartialHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	// 读取前 8KB
	buf := make([]byte, 8192)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	h.Write(buf[:n])
	return hex.EncodeToString(h.Sum(nil)), nil
}

func runBatchTag(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// args 全部是标签名
	if len(args) == 0 {
		return fmt.Errorf("请提供标签名")
	}
	tagNames := args

	// 构造搜索参数
	params := searcher.SearchParams{Limit: 100000} // 不限制

	if q, _ := cmd.Flags().GetString("query"); q != "" {
		params.Keyword = q
	}
	if t, _ := cmd.Flags().GetString("type"); t != "" {
		for _, ext := range strings.Split(t, ",") {
			ext = strings.TrimSpace(strings.ToLower(ext))
			ext = strings.TrimPrefix(ext, ".")
			if ext != "" {
				params.Exts = append(params.Exts, ext)
			}
		}
	}
	if d, _ := cmd.Flags().GetString("dir"); d != "" {
		absDir, _ := filepath.Abs(d)
		params.Dir = absDir
	}
	if after, _ := cmd.Flags().GetString("after"); after != "" {
		t, err := parseTime(after)
		if err != nil {
			return err
		}
		params.After = &t
	}
	if before, _ := cmd.Flags().GetString("before"); before != "" {
		t, err := parseTime(before)
		if err != nil {
			return err
		}
		params.Before = &t
	}
	if recent, _ := cmd.Flags().GetString("recent"); recent != "" {
		t, err := parseRecent(recent)
		if err != nil {
			return err
		}
		params.After = &t
	}
	if larger, _ := cmd.Flags().GetString("larger"); larger != "" {
		size, err := parseSize(larger)
		if err != nil {
			return err
		}
		params.MinSize = size
	}
	if smaller, _ := cmd.Flags().GetString("smaller"); smaller != "" {
		size, err := parseSize(smaller)
		if err != nil {
			return err
		}
		params.MaxSize = size
	}
	params.FilesOnly = true // 批量模式只标文件

	// 执行搜索
	s := searcher.New(pool, cfg.Search.SimilarityThreshold)
	result, err := s.Search(ctx, params)
	if err != nil {
		return err
	}

	if result.Total == 0 {
		fmt.Println("\n  未找到匹配的文件\n")
		return nil
	}

	green := color.New(color.FgGreen).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()

	fmt.Printf("\n  将为 %s 个文件添加标签: %s\n",
		bold(fmt.Sprintf("%d", result.Total)),
		bold(strings.Join(tagNames, ", ")))

	// 确认
	autoYes, _ := cmd.Flags().GetBool("yes")
	if !autoYes {
		fmt.Printf("\n  确认? [Y/n] ")
		var input string
		fmt.Scanln(&input)
		input = strings.TrimSpace(strings.ToLower(input))
		if input != "" && input != "y" && input != "yes" {
			fmt.Println("  已取消")
			return nil
		}
	}

	// 确保标签存在
	tagIDs := make(map[string]int)
	for _, name := range tagNames {
		var tagID int
		err := pool.QueryRow(ctx,
			"INSERT INTO tags (name) VALUES ($1) ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name RETURNING id",
			name).Scan(&tagID)
		if err != nil {
			return err
		}
		tagIDs[name] = tagID
	}

	// 批量打标签
	count := 0
	for _, f := range result.Entries {
		for _, tagID := range tagIDs {
			_, err := pool.Exec(ctx,
				"INSERT INTO file_tags (file_id, tag_id, source) VALUES ($1, $2, 'manual') ON CONFLICT DO NOTHING",
				f.ID, tagID)
			if err != nil {
				return err
			}
		}
		count++
	}

	fmt.Printf("\n  %s 已标记 %d 个文件\n\n", green("✓"), count)
	return nil
}
