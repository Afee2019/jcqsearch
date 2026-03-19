package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"jcqsearch/internal/searcher"
)

var findCmd = &cobra.Command{
	Use:   "find [keyword]",
	Short: "搜索文件",
	Long: `模糊搜索文件名，支持组合筛选。

示例:
  jcqsearch find 报告
  jcqsearch find 报告 -t pdf
  jcqsearch find 报告 -t pdf --after 2024-01
  jcqsearch find -t mkv --larger 100M
  jcqsearch find --recent 7d`,
	RunE: runFind,
}

func init() {
	f := findCmd.Flags()
	f.StringP("type", "t", "", "文件类型（扩展名），多个用逗号分隔")
	f.String("after", "", "修改时间起始（如 2024-01, 2024-01-15）")
	f.String("before", "", "修改时间截止")
	f.StringP("dir", "d", "", "限定目录范围")
	f.String("larger", "", "最小文件大小（如 10M, 1G）")
	f.String("smaller", "", "最大文件大小")
	f.StringP("recent", "r", "", "最近 N 天/小时（如 7d, 24h）")
	f.StringP("tag", "g", "", "按标签筛选，多个用逗号分隔（AND）")
	f.Bool("dirs-only", false, "仅搜索目录")
	f.Bool("files-only", false, "仅搜索文件")
	f.IntP("limit", "n", 0, "返回条数（默认 20）")
	f.IntP("open", "o", 0, "用系统默认程序打开第 N 个结果")
	f.Int("reveal", 0, "在 Finder 中显示第 N 个结果")

	rootCmd.AddCommand(findCmd)
}

func runFind(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	params := searcher.SearchParams{}

	// 关键词
	if len(args) > 0 {
		params.Keyword = strings.Join(args, " ")
	}

	// 扩展名
	if t, _ := cmd.Flags().GetString("type"); t != "" {
		for _, ext := range strings.Split(t, ",") {
			ext = strings.TrimSpace(strings.ToLower(ext))
			ext = strings.TrimPrefix(ext, ".")
			if ext != "" {
				params.Exts = append(params.Exts, ext)
			}
		}
	}

	// 时间范围
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

	// 目录
	if d, _ := cmd.Flags().GetString("dir"); d != "" {
		params.Dir = d
	}

	// 文件大小
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

	// 标签筛选
	if tagStr, _ := cmd.Flags().GetString("tag"); tagStr != "" {
		for _, t := range strings.Split(tagStr, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				params.Tags = append(params.Tags, t)
			}
		}
	}

	// 文件/目录筛选
	params.DirsOnly, _ = cmd.Flags().GetBool("dirs-only")
	params.FilesOnly, _ = cmd.Flags().GetBool("files-only")

	// Limit
	params.Limit, _ = cmd.Flags().GetInt("limit")
	if params.Limit <= 0 {
		params.Limit = cfg.Search.DefaultLimit
	}

	// 必须有至少一个搜索条件
	if params.Keyword == "" && len(params.Exts) == 0 && params.After == nil &&
		params.Before == nil && params.MinSize == 0 && params.MaxSize == 0 &&
		params.Dir == "" && !params.DirsOnly && len(params.Tags) == 0 {
		return fmt.Errorf("请提供搜索关键词或筛选条件")
	}

	// 执行搜索
	s := searcher.New(pool, cfg.Search.SimilarityThreshold)
	result, err := s.Search(ctx, params)
	if err != nil {
		return err
	}

	// 输出结果
	printResults(result, params.Keyword)

	// --open / --reveal
	openIdx, _ := cmd.Flags().GetInt("open")
	revealIdx, _ := cmd.Flags().GetInt("reveal")

	if openIdx > 0 && openIdx <= result.Total {
		path := result.Entries[openIdx-1].Path
		return exec.Command("open", path).Run()
	}
	if revealIdx > 0 && revealIdx <= result.Total {
		path := result.Entries[revealIdx-1].Path
		return exec.Command("open", "-R", path).Run()
	}

	return nil
}

func printResults(result *searcher.SearchResult, keyword string) {
	if result.Total == 0 {
		fmt.Println("\n  未找到匹配的文件\n")
		return
	}

	dim := color.New(color.Faint).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()
	red := color.New(color.FgRed, color.Bold).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	// 检查是否有标签需要展示
	hasTags := len(result.FileTags) > 0

	fmt.Println()
	if hasTags {
		fmt.Printf("  %s  %-38s %-10s %-18s %-18s %s\n",
			dim("#"), dim("名称"), dim("大小"), dim("修改时间"), dim("标签"), dim("路径"))
	} else {
		fmt.Printf("  %s  %-42s %-10s %-18s %s\n",
			dim("#"), dim("名称"), dim("大小"), dim("修改时间"), dim("路径"))
	}
	fmt.Println()

	homeDir, _ := os.UserHomeDir()

	for i, f := range result.Entries {
		// 文件名高亮关键词
		displayName := f.Name
		if keyword != "" {
			displayName = highlightKeyword(f.Name, keyword, red)
		}

		// 文件大小
		sizeStr := humanize.IBytes(uint64(f.Size))
		if f.IsDir {
			sizeStr = bold("<DIR>")
		}

		// 时间
		timeStr := f.ModTime.Format("2006-01-02 15:04")

		// 路径缩短
		dir := f.Dir
		if homeDir != "" && strings.HasPrefix(dir, homeDir) {
			dir = "~" + dir[len(homeDir):]
		}

		if hasTags {
			tagStr := ""
			if tags, ok := result.FileTags[f.ID]; ok {
				tagStr = yellow(strings.Join(tags, " "))
			}
			fmt.Printf("  %-3d  %-38s %-10s %-18s %-18s %s\n",
				i+1, displayName, sizeStr, timeStr, tagStr, dim(dir))
		} else {
			fmt.Printf("  %-3d  %-42s %-10s %-18s %s\n",
				i+1, displayName, sizeStr, timeStr, dim(dir))
		}
	}

	fmt.Printf("\n  共 %s 条结果（耗时 %s）\n\n",
		bold(fmt.Sprintf("%d", result.Total)),
		result.Duration.Round(time.Millisecond))
}

func highlightKeyword(name, keyword string, colorFunc func(a ...interface{}) string) string {
	lower := strings.ToLower(name)
	kwLower := strings.ToLower(keyword)
	idx := strings.Index(lower, kwLower)
	if idx < 0 {
		return name
	}
	return name[:idx] + colorFunc(name[idx:idx+len(keyword)]) + name[idx+len(keyword):]
}

// parseTime 解析时间参数，支持 "2024-01", "2024-01-15", "2024-01-15 10:30"
func parseTime(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02 15:04",
		"2006-01-02",
		"2006-01",
	}
	for _, f := range formats {
		if t, err := time.ParseInLocation(f, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析时间: %s（支持格式: 2024-01, 2024-01-15, 2024-01-15 10:30）", s)
}

// parseRecent 解析最近时间，支持 "7d", "24h", "30m"
func parseRecent(s string) (time.Time, error) {
	if len(s) < 2 {
		return time.Time{}, fmt.Errorf("无效的时间: %s", s)
	}
	unit := s[len(s)-1]
	val, err := strconv.Atoi(s[:len(s)-1])
	if err != nil {
		return time.Time{}, fmt.Errorf("无法解析时间数值: %s", s)
	}
	now := time.Now()
	switch unit {
	case 'd':
		return now.AddDate(0, 0, -val), nil
	case 'h':
		return now.Add(-time.Duration(val) * time.Hour), nil
	case 'm':
		return now.Add(-time.Duration(val) * time.Minute), nil
	default:
		return time.Time{}, fmt.Errorf("不支持的时间单位: %c（支持 d/h/m）", unit)
	}
}

// parseSize 解析文件大小，支持 "100M", "1G", "500K", "1024"
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return 0, nil
	}
	upper := strings.ToUpper(s)
	last := upper[len(upper)-1]
	var multiplier int64 = 1
	numStr := s
	switch last {
	case 'K':
		multiplier = 1024
		numStr = s[:len(s)-1]
	case 'M':
		multiplier = 1024 * 1024
		numStr = s[:len(s)-1]
	case 'G':
		multiplier = 1024 * 1024 * 1024
		numStr = s[:len(s)-1]
	}
	val, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("无法解析大小: %s", s)
	}
	return val * multiplier, nil
}
