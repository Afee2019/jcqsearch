package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"jcqsearch/internal/model"
	"jcqsearch/internal/scanner"
)

var scanCmd = &cobra.Command{
	Use:   "scan [path]",
	Short: "扫描目录建立文件索引",
	Long: `扫描所有已配置的路径，更新文件索引。
可指定路径进行临时扫描（需先通过 paths add 添加）。

示例:
  jcqsearch scan                        # 扫描所有已配置路径
  jcqsearch scan /Users/shawn/dev      # 仅扫描指定路径`,
	RunE: runScan,
}

func init() {
	rootCmd.AddCommand(scanCmd)
}

func runScan(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	s := scanner.New(pool, cfg.Scan.BatchSize, cfg.Scan.Concurrency, cfg.Ignore)

	bold := color.New(color.Bold).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()

	start := time.Now()

	if len(args) > 0 {
		// 扫描指定路径
		targetPath, err := filepath.Abs(args[0])
		if err != nil {
			return fmt.Errorf("解析路径失败: %w", err)
		}

		info, err := os.Stat(targetPath)
		if err != nil {
			return fmt.Errorf("路径不存在: %s", targetPath)
		}
		if !info.IsDir() {
			return fmt.Errorf("路径不是目录: %s", targetPath)
		}

		// 查找对应的 scan_path
		var scanPathID int
		err = pool.QueryRow(ctx,
			"SELECT id FROM scan_paths WHERE path = $1 AND enabled = true", targetPath).Scan(&scanPathID)
		if err != nil {
			return fmt.Errorf("路径 %s 未配置或未启用，请先运行: jcqsearch paths add %s", targetPath, targetPath)
		}

		fmt.Fprintf(os.Stderr, "\n扫描路径: %s\n\n", bold(targetPath))

		result, err := s.ScanOne(ctx, scanPathFromDB(ctx, scanPathID))
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "\r  %s %s  %s 个文件  (%s)\n",
			green("✓"), result.Path,
			humanize.Comma(result.FileCount),
			result.Duration.Round(time.Millisecond))

	} else {
		// 扫描所有路径
		fmt.Fprintf(os.Stderr, "\n%s\n\n", bold("扫描所有已配置路径:"))

		results, err := s.ScanAll(ctx)
		if err != nil {
			return err
		}

		var totalFiles int64
		for _, r := range results {
			fmt.Fprintf(os.Stderr, "\r  %s %s  %s 个文件  (%s)\n",
				green("✓"), r.Path,
				humanize.Comma(r.FileCount),
				r.Duration.Round(time.Millisecond))
			totalFiles += r.FileCount
		}

		fmt.Fprintf(os.Stderr, "\n%s 扫描完成  共 %s 个文件  耗时 %s\n\n",
			green("✓"),
			humanize.Comma(totalFiles),
			time.Since(start).Round(time.Millisecond))
	}

	return nil
}

func scanPathFromDB(ctx context.Context, id int) model.ScanPath {
	var sp model.ScanPath
	pool.QueryRow(ctx,
		"SELECT id, path, label, enabled, max_depth FROM scan_paths WHERE id = $1", id).
		Scan(&sp.ID, &sp.Path, &sp.Label, &sp.Enabled, &sp.MaxDepth)
	return sp
}
