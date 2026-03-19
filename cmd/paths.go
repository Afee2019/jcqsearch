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
)

var pathsCmd = &cobra.Command{
	Use:   "paths",
	Short: "管理扫描路径",
}

var pathsListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有扫描路径",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		rows, err := pool.Query(ctx, `
			SELECT sp.id, sp.path, sp.label, sp.enabled, sp.max_depth,
				COUNT(f.id) AS file_count,
				COALESCE(SUM(f.size), 0) AS total_size
			FROM scan_paths sp
			LEFT JOIN files f ON f.scan_path_id = sp.id
			GROUP BY sp.id ORDER BY sp.id`)
		if err != nil {
			return err
		}
		defer rows.Close()

		dim := color.New(color.Faint).SprintFunc()
		green := color.New(color.FgGreen).SprintFunc()
		red := color.New(color.FgRed).SprintFunc()

		fmt.Printf("\n  %s  %-35s %-12s %-6s %-10s %-10s %s\n",
			dim("ID"), dim("路径"), dim("备注"), dim("启用"), dim("深度"), dim("文件数"), dim("总大小"))
		fmt.Println()

		for rows.Next() {
			var id, maxDepth int
			var path, label string
			var enabled bool
			var fileCount int64
			var totalSize int64
			if err := rows.Scan(&id, &path, &label, &enabled, &maxDepth, &fileCount, &totalSize); err != nil {
				return err
			}

			enabledStr := green("✓")
			if !enabled {
				enabledStr = red("✗")
			}
			depthStr := "无限制"
			if maxDepth > 0 {
				depthStr = fmt.Sprintf("%d", maxDepth)
			}

			fmt.Printf("  %-3d  %-35s %-12s %-6s %-10s %-10s %s\n",
				id, path, label, enabledStr, depthStr,
				humanize.Comma(fileCount), humanize.IBytes(uint64(totalSize)))
		}
		fmt.Println()
		return rows.Err()
	},
}

var pathsAddCmd = &cobra.Command{
	Use:   "add <path> [label]",
	Short: "添加扫描路径",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		absPath, err := filepath.Abs(args[0])
		if err != nil {
			return fmt.Errorf("解析路径失败: %w", err)
		}

		info, err := os.Stat(absPath)
		if err != nil {
			return fmt.Errorf("路径不存在: %s", absPath)
		}
		if !info.IsDir() {
			return fmt.Errorf("路径不是目录: %s", absPath)
		}

		label := ""
		if len(args) > 1 {
			label = args[1]
		}

		_, err = pool.Exec(ctx,
			"INSERT INTO scan_paths (path, label) VALUES ($1, $2) ON CONFLICT (path) DO UPDATE SET label = $2, updated_at = $3",
			absPath, label, time.Now())
		if err != nil {
			return fmt.Errorf("添加失败: %w", err)
		}

		fmt.Printf("已添加扫描路径: %s", absPath)
		if label != "" {
			fmt.Printf(" (%s)", label)
		}
		fmt.Println()
		return nil
	},
}

var pathsRemoveCmd = &cobra.Command{
	Use:   "remove <path>",
	Short: "移除扫描路径（同时清理关联索引）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		absPath, err := filepath.Abs(args[0])
		if err != nil {
			return fmt.Errorf("解析路径失败: %w", err)
		}

		tag, err := pool.Exec(ctx, "DELETE FROM scan_paths WHERE path = $1", absPath)
		if err != nil {
			return fmt.Errorf("删除失败: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("未找到路径: %s", absPath)
		}

		fmt.Printf("已移除扫描路径: %s（关联索引已清理）\n", absPath)
		return nil
	},
}

func init() {
	pathsCmd.AddCommand(pathsListCmd)
	pathsCmd.AddCommand(pathsAddCmd)
	pathsCmd.AddCommand(pathsRemoveCmd)
	rootCmd.AddCommand(pathsCmd)
}
