package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var untagCmd = &cobra.Command{
	Use:   "untag <path> <tags...>",
	Short: "移除文件标签",
	Long: `移除文件的指定标签。

示例:
  jcqsearch untag ~/Desktop/报告.pdf work        # 移除指定标签
  jcqsearch untag ~/Desktop/报告.pdf --all       # 移除所有标签`,
	Args: cobra.MinimumNArgs(1),
	RunE: runUntag,
}

func init() {
	untagCmd.Flags().Bool("all", false, "移除所有标签")
	rootCmd.AddCommand(untagCmd)
}

func runUntag(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	absPath, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("解析路径失败: %w", err)
	}

	var fileID int64
	var fileName string
	err = pool.QueryRow(ctx,
		"SELECT id, name FROM files WHERE path = $1", absPath).Scan(&fileID, &fileName)
	if err != nil {
		return fmt.Errorf("文件未在索引中: %s", absPath)
	}

	removeAll, _ := cmd.Flags().GetBool("all")
	red := color.New(color.FgRed).SprintFunc()

	if removeAll {
		tag, err := pool.Exec(ctx, "DELETE FROM file_tags WHERE file_id = $1", fileID)
		if err != nil {
			return err
		}
		fmt.Printf("\n已移除 %s 的所有标签（%d 个）\n\n", fileName, tag.RowsAffected())
		return nil
	}

	if len(args) < 2 {
		return fmt.Errorf("请指定要移除的标签，或使用 --all 移除所有标签")
	}

	tagNames := args[1:]
	for _, name := range tagNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		tag, err := pool.Exec(ctx, `
			DELETE FROM file_tags
			WHERE file_id = $1
			  AND tag_id = (SELECT id FROM tags WHERE name = $2)`,
			fileID, name)
		if err != nil {
			return err
		}

		if tag.RowsAffected() > 0 {
			fmt.Printf("  %s %s\n", red("-"), name)
		} else {
			fmt.Printf("  %s（未找到）\n", name)
		}
	}

	// 显示剩余标签
	allTags, _ := getFileTags(ctx, fileID)
	if len(allTags) > 0 {
		fmt.Printf("  剩余标签: %s\n", strings.Join(allTags, ", "))
	} else {
		dim := color.New(color.Faint).SprintFunc()
		fmt.Printf("  %s\n", dim("（无标签）"))
	}
	fmt.Println()

	return nil
}
