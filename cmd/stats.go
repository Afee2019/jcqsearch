package cmd

import (
	"context"
	"fmt"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "显示索引统计信息",
	RunE:  runStats,
}

func init() {
	rootCmd.AddCommand(statsCmd)
}

func runStats(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	bold := color.New(color.Bold).SprintFunc()
	dim := color.New(color.Faint).SprintFunc()

	// 总体统计
	var fileCount, dirCount, totalSize int64
	var oldestScan, latestScan *time.Time

	err := pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE NOT is_dir),
			COUNT(*) FILTER (WHERE is_dir),
			COALESCE(SUM(size) FILTER (WHERE NOT is_dir), 0),
			MIN(scanned_at),
			MAX(scanned_at)
		FROM files`).Scan(&fileCount, &dirCount, &totalSize, &oldestScan, &latestScan)
	if err != nil {
		return err
	}

	fmt.Printf("\n%s\n", bold("索引概览:"))
	fmt.Printf("  总文件数:   %s\n", humanize.Comma(fileCount))
	fmt.Printf("  总目录数:   %s\n", humanize.Comma(dirCount))
	fmt.Printf("  总大小:     %s\n", humanize.IBytes(uint64(totalSize)))
	if latestScan != nil {
		fmt.Printf("  最近扫描:   %s\n", latestScan.Format("2006-01-02 15:04:05"))
	}

	// 按扫描路径统计
	fmt.Printf("\n%s\n", bold("扫描路径:"))
	rows, err := pool.Query(ctx, `
		SELECT sp.path, sp.label,
			COUNT(f.id) AS file_count,
			COALESCE(SUM(f.size), 0) AS total_size
		FROM scan_paths sp
		LEFT JOIN files f ON f.scan_path_id = sp.id
		WHERE sp.enabled = true
		GROUP BY sp.id, sp.path, sp.label
		ORDER BY file_count DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var path, label string
		var fc, ts int64
		if err := rows.Scan(&path, &label, &fc, &ts); err != nil {
			return err
		}
		labelStr := ""
		if label != "" {
			labelStr = fmt.Sprintf(" (%s)", label)
		}
		fmt.Printf("  %-40s %s 文件  %s%s\n",
			path, humanize.Comma(fc), humanize.IBytes(uint64(ts)), dim(labelStr))
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// 文件类型 TOP 10
	fmt.Printf("\n%s\n", bold("文件类型 TOP 10:"))
	rows2, err := pool.Query(ctx, `
		SELECT ext, COUNT(*) AS cnt, SUM(size) AS total_size
		FROM files
		WHERE NOT is_dir AND ext <> ''
		GROUP BY ext
		ORDER BY cnt DESC
		LIMIT 10`)
	if err != nil {
		return err
	}
	defer rows2.Close()

	for rows2.Next() {
		var ext string
		var cnt, ts int64
		if err := rows2.Scan(&ext, &cnt, &ts); err != nil {
			return err
		}
		fmt.Printf("  %-10s %6s 文件  %s\n",
			ext, humanize.Comma(cnt), humanize.IBytes(uint64(ts)))
	}
	if err := rows2.Err(); err != nil {
		return err
	}

	// 忽略规则数
	var ruleCount int64
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM ignore_rules WHERE enabled = true").Scan(&ruleCount)
	fmt.Printf("\n忽略规则: %d 条启用中\n\n", ruleCount)

	return nil
}
