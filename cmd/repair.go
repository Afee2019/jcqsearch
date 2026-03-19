package cmd

import (
	"context"
	"fmt"

	humanize "github.com/dustin/go-humanize"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var repairCmd = &cobra.Command{
	Use:   "repair",
	Short: "修复移动/重命名后丢失的文件标签",
	Long: `检测标记为"丢失"的有标签文件，通过文件指纹（partial_hash）在索引中查找匹配，
自动重新关联标签。

文件被移动或重命名后，scan 会将有标签的旧记录标记为 missing，
而在新路径创建新记录。repair 通过 (size, partial_hash) 匹配两者，
将标签从旧记录迁移到新记录。

示例:
  jcqsearch repair              # 执行修复
  jcqsearch repair --dry-run    # 仅预览`,
	RunE: runRepair,
}

func init() {
	repairCmd.Flags().Bool("dry-run", false, "仅预览，不实际执行")
	rootCmd.AddCommand(repairCmd)
}

type missingFile struct {
	ID          int64
	Path        string
	Name        string
	Size        int64
	PartialHash string
	TagCount    int
}

func runRepair(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	dim := color.New(color.Faint).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	// 1. 查找所有 missing 的有标签文件
	rows, err := pool.Query(ctx, `
		SELECT f.id, f.path, f.name, f.size, f.partial_hash,
			(SELECT COUNT(*) FROM file_tags ft WHERE ft.file_id = f.id) AS tag_count
		FROM files f
		WHERE f.missing_since IS NOT NULL
		  AND EXISTS (SELECT 1 FROM file_tags ft WHERE ft.file_id = f.id)
		ORDER BY f.missing_since`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var missing []missingFile
	for rows.Next() {
		var mf missingFile
		if err := rows.Scan(&mf.ID, &mf.Path, &mf.Name, &mf.Size, &mf.PartialHash, &mf.TagCount); err != nil {
			return err
		}
		missing = append(missing, mf)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(missing) == 0 {
		fmt.Println("\n  无需修复，所有有标签的文件均正常存在。\n")
		return nil
	}

	fmt.Printf("\n  发现 %s 个丢失的有标签文件\n\n", humanize.Comma(int64(len(missing))))

	repaired := 0
	notFound := 0
	noHash := 0

	for _, mf := range missing {
		if mf.PartialHash == "" {
			// 没有指纹，无法匹配
			fmt.Printf("  %s %s — 无指纹，无法修复\n", yellow("?"), mf.Name)
			noHash++
			continue
		}

		// 2. 先按 hash 精确匹配，再 fallback 到按 size 匹配 + 实时计算 hash
		var candidateID int64
		var candidatePath string
		found := false

		// 2a. 按 (size, partial_hash) 精确匹配
		crows, err := pool.Query(ctx, `
			SELECT id, path FROM files
			WHERE size = $1 AND partial_hash = $2
			  AND id <> $3
			  AND missing_since IS NULL`,
			mf.Size, mf.PartialHash, mf.ID)
		if err != nil {
			return err
		}
		var exactMatches []struct{ id int64; path string }
		for crows.Next() {
			var m struct{ id int64; path string }
			if err := crows.Scan(&m.id, &m.path); err != nil {
				crows.Close()
				return err
			}
			exactMatches = append(exactMatches, m)
		}
		crows.Close()

		if len(exactMatches) == 1 {
			candidateID = exactMatches[0].id
			candidatePath = exactMatches[0].path
			found = true
		}

		// 2b. 没有精确匹配 → 按 size 找候选，实时计算 hash 对比
		if !found {
			srows, err := pool.Query(ctx, `
				SELECT id, path FROM files
				WHERE size = $1 AND partial_hash = ''
				  AND id <> $2
				  AND missing_since IS NULL`,
				mf.Size, mf.ID)
			if err != nil {
				return err
			}
			var sizeMatches []struct{ id int64; path string }
			for srows.Next() {
				var m struct{ id int64; path string }
				if err := srows.Scan(&m.id, &m.path); err != nil {
					srows.Close()
					return err
				}
				sizeMatches = append(sizeMatches, m)
			}
			srows.Close()

			for _, m := range sizeMatches {
				hash, err := computePartialHash(m.path)
				if err != nil || hash == "" {
					continue
				}
				// 更新候选文件的 hash
				pool.Exec(ctx, "UPDATE files SET partial_hash = $1 WHERE id = $2", hash, m.id)

				if hash == mf.PartialHash {
					candidateID = m.id
					candidatePath = m.path
					found = true
					break
				}
			}
		}

		if !found {
			fmt.Printf("  %s %s — 未找到匹配\n", red("✗"), mf.Name)
			notFound++
			continue
		}

		// 唯一匹配
		fmt.Printf("  %s %s → %s (%d 个标签)\n",
			green("✓"), mf.Name, dim(candidatePath), mf.TagCount)

		if !dryRun {
			// 3. 迁移标签：将旧文件的 file_tags 指向新文件
			_, err = pool.Exec(ctx, `
				INSERT INTO file_tags (file_id, tag_id, tagged_at, source)
				SELECT $1, tag_id, tagged_at, source FROM file_tags WHERE file_id = $2
				ON CONFLICT DO NOTHING`,
				candidateID, mf.ID)
			if err != nil {
				return err
			}

			// 删除旧文件的标签关联
			_, err = pool.Exec(ctx, "DELETE FROM file_tags WHERE file_id = $1", mf.ID)
			if err != nil {
				return err
			}

			// 将 partial_hash 复制到新文件（如果新文件还没有）
			_, err = pool.Exec(ctx,
				"UPDATE files SET partial_hash = $1 WHERE id = $2 AND partial_hash = ''",
				mf.PartialHash, candidateID)
			if err != nil {
				return err
			}

			// 删除旧的 missing 记录
			_, err = pool.Exec(ctx, "DELETE FROM files WHERE id = $1", mf.ID)
			if err != nil {
				return err
			}
		}

		repaired++
	}

	fmt.Printf("\n  修复: %d  未找到: %d  无指纹: %d\n",
		repaired, notFound, noHash)

	if dryRun {
		fmt.Printf("  %s\n", dim("（预览模式，未实际执行。去掉 --dry-run 以执行）"))
	}
	fmt.Println()

	return nil
}
