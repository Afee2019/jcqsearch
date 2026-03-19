package cmd

import (
	"context"
	"fmt"

	humanize "github.com/dustin/go-humanize"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var tagsCmd = &cobra.Command{
	Use:   "tags",
	Short: "管理标签",
	RunE:  runTags,
}

var tagsRenameCmd = &cobra.Command{
	Use:   "rename <old> <new>",
	Short: "重命名标签",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		oldName, newName := args[0], args[1]

		tag, err := pool.Exec(ctx, "UPDATE tags SET name = $1 WHERE name = $2", newName, oldName)
		if err != nil {
			// 可能新名称已存在
			return fmt.Errorf("重命名失败（目标标签名可能已存在）: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("未找到标签: %s", oldName)
		}
		fmt.Printf("已重命名: %s → %s\n", oldName, newName)
		return nil
	},
}

var tagsMergeCmd = &cobra.Command{
	Use:   "merge <source> <target>",
	Short: "合并标签（将 source 合并到 target）",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		sourceName, targetName := args[0], args[1]

		var sourceID, targetID int
		err := pool.QueryRow(ctx, "SELECT id FROM tags WHERE name = $1", sourceName).Scan(&sourceID)
		if err != nil {
			return fmt.Errorf("未找到源标签: %s", sourceName)
		}
		err = pool.QueryRow(ctx, "SELECT id FROM tags WHERE name = $1", targetName).Scan(&targetID)
		if err != nil {
			return fmt.Errorf("未找到目标标签: %s", targetName)
		}

		// 将 source 的关联转移到 target（忽略冲突）
		_, err = pool.Exec(ctx, `
			INSERT INTO file_tags (file_id, tag_id, tagged_at, source)
			SELECT file_id, $1, tagged_at, source FROM file_tags WHERE tag_id = $2
			ON CONFLICT DO NOTHING`, targetID, sourceID)
		if err != nil {
			return err
		}

		// 删除 source 标签（级联删除其 file_tags）
		_, err = pool.Exec(ctx, "DELETE FROM tags WHERE id = $1", sourceID)
		if err != nil {
			return err
		}

		fmt.Printf("已合并: %s → %s\n", sourceName, targetName)
		return nil
	},
}

var tagsDeleteCmd = &cobra.Command{
	Use:   "delete <tag>",
	Short: "删除标签（从所有文件移除）",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		name := args[0]

		tag, err := pool.Exec(ctx, "DELETE FROM tags WHERE name = $1", name)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("未找到标签: %s", name)
		}
		fmt.Printf("已删除标签: %s\n", name)
		return nil
	},
}

var tagsCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "清理无文件关联的空标签",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		tag, err := pool.Exec(ctx, `
			DELETE FROM tags WHERE id NOT IN (
				SELECT DISTINCT tag_id FROM file_tags
			)`)
		if err != nil {
			return err
		}
		fmt.Printf("已清理 %d 个空标签\n", tag.RowsAffected())
		return nil
	},
}

func init() {
	tagsCmd.AddCommand(tagsRenameCmd)
	tagsCmd.AddCommand(tagsMergeCmd)
	tagsCmd.AddCommand(tagsDeleteCmd)
	tagsCmd.AddCommand(tagsCleanCmd)
	rootCmd.AddCommand(tagsCmd)
}

func runTags(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	dim := color.New(color.Faint).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()

	rows, err := pool.Query(ctx, `
		SELECT t.id, t.name, t.color,
			COUNT(ft.file_id) AS file_count,
			COUNT(ft.file_id) FILTER (WHERE ft.source = 'manual') AS manual_count,
			COUNT(ft.file_id) FILTER (WHERE ft.source = 'rule') AS rule_count,
			t.created_at
		FROM tags t
		LEFT JOIN file_tags ft ON ft.tag_id = t.id
		GROUP BY t.id
		ORDER BY file_count DESC, t.name`)
	if err != nil {
		return err
	}
	defer rows.Close()

	fmt.Printf("\n  %-25s %8s  %-20s  %s\n",
		dim("标签"), dim("文件数"), dim("来源分布"), dim("创建时间"))
	fmt.Println()

	var totalTags int
	var totalFiles int64

	for rows.Next() {
		var id int
		var name, clr string
		var fileCount, manualCount, ruleCount int64
		var createdAt interface{}
		if err := rows.Scan(&id, &name, &clr, &fileCount, &manualCount, &ruleCount, &createdAt); err != nil {
			return err
		}

		// 来源分布
		var sources []string
		if manualCount > 0 {
			sources = append(sources, fmt.Sprintf("手动:%s", humanize.Comma(manualCount)))
		}
		if ruleCount > 0 {
			sources = append(sources, fmt.Sprintf("规则:%s", humanize.Comma(ruleCount)))
		}
		otherCount := fileCount - manualCount - ruleCount
		if otherCount > 0 {
			sources = append(sources, fmt.Sprintf("其他:%s", humanize.Comma(otherCount)))
		}
		sourceStr := ""
		if len(sources) > 0 {
			sourceStr = fmt.Sprintf("%s", sources[0])
			for _, s := range sources[1:] {
				sourceStr += " " + s
			}
		}

		fmt.Printf("  %-25s %8s  %-20s\n",
			cyan(name),
			humanize.Comma(fileCount),
			dim(sourceStr))

		totalTags++
		totalFiles += fileCount
	}

	if totalTags == 0 {
		fmt.Printf("  %s\n", dim("（暂无标签）"))
	}

	fmt.Printf("\n  共 %d 个标签，关联 %s 个文件\n\n",
		totalTags, humanize.Comma(totalFiles))

	return rows.Err()
}
