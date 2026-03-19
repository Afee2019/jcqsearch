package cmd

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	humanize "github.com/dustin/go-humanize"
	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"jcqsearch/internal/config"
)

var autotagCmd = &cobra.Command{
	Use:   "autotag",
	Short: "按规则自动打标签",
	Long: `根据 config.yaml 中的 auto_tag.rules 自动给文件打标签。

示例:
  jcqsearch autotag              # 执行自动标签
  jcqsearch autotag --dry-run    # 预览，不实际执行`,
	RunE: runAutotag,
}

func init() {
	autotagCmd.Flags().Bool("dry-run", false, "仅预览，不实际执行")
	rootCmd.AddCommand(autotagCmd)
}

func runAutotag(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	rules := cfg.AutoTag.Rules
	if len(rules) == 0 {
		return fmt.Errorf("未配置自动标签规则，请在 config.yaml 的 auto_tag.rules 中添加")
	}

	dim := color.New(color.Faint).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()

	homeDir, _ := os.UserHomeDir()

	// 预编译正则和解析大小

	var compiled []compiledRule
	for _, r := range rules {
		cr := compiledRule{rule: r}
		if r.Match.Name != "" {
			re, err := regexp.Compile(r.Match.Name)
			if err != nil {
				return fmt.Errorf("无效的文件名正则 %q: %w", r.Match.Name, err)
			}
			cr.nameRe = re
		}
		if r.Match.Larger != "" {
			size, err := parseSize(r.Match.Larger)
			if err != nil {
				return fmt.Errorf("无效的大小 %q: %w", r.Match.Larger, err)
			}
			cr.minSize = size
		}
		if r.Match.Smaller != "" {
			size, err := parseSize(r.Match.Smaller)
			if err != nil {
				return fmt.Errorf("无效的大小 %q: %w", r.Match.Smaller, err)
			}
			cr.maxSize = size
		}
		if r.Match.Dir != "" {
			d := r.Match.Dir
			if strings.HasPrefix(d, "~/") && homeDir != "" {
				d = homeDir + d[1:]
			}
			cr.dir = d
		}
		compiled = append(compiled, cr)
	}

	// 查询所有文件
	rows, err := pool.Query(ctx,
		"SELECT id, path, dir, name, ext, is_dir, size FROM files WHERE is_dir = false AND missing_since IS NULL")
	if err != nil {
		return err
	}
	defer rows.Close()

	// 统计
	type ruleStats struct {
		matchCount int
		desc       string
		tags       []string
	}
	stats := make([]ruleStats, len(compiled))
	for i, cr := range compiled {
		// 构造描述
		var parts []string
		if len(cr.rule.Match.Ext) > 0 {
			parts = append(parts, fmt.Sprintf("ext: [%s]", strings.Join(cr.rule.Match.Ext, ", ")))
		}
		if cr.dir != "" {
			d := cr.rule.Match.Dir
			parts = append(parts, fmt.Sprintf("dir: %s", d))
		}
		if cr.rule.Match.Name != "" {
			parts = append(parts, fmt.Sprintf("name: %s", cr.rule.Match.Name))
		}
		if cr.rule.Match.Larger != "" {
			parts = append(parts, fmt.Sprintf("larger: %s", cr.rule.Match.Larger))
		}
		if cr.rule.Match.Smaller != "" {
			parts = append(parts, fmt.Sprintf("smaller: %s", cr.rule.Match.Smaller))
		}
		stats[i] = ruleStats{desc: strings.Join(parts, ", "), tags: cr.rule.Tags}
	}

	// 收集要打的标签: file_id -> []tagName
	toTag := make(map[int64]map[string]bool)
	totalAssociations := 0

	for rows.Next() {
		var id int64
		var path, dir, name, ext string
		var isDir bool
		var size int64
		if err := rows.Scan(&id, &path, &dir, &name, &ext, &isDir, &size); err != nil {
			return err
		}

		for i, cr := range compiled {
			if matchRule(cr, name, ext, dir, size) {
				if toTag[id] == nil {
					toTag[id] = make(map[string]bool)
				}
				for _, t := range cr.rule.Tags {
					if !toTag[id][t] {
						toTag[id][t] = true
						stats[i].matchCount++
						totalAssociations++
					}
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// 输出报告
	fmt.Println()
	fmt.Printf("  %-40s %8s  %s\n", dim("规则"), dim("匹配文件"), dim("标签"))
	fmt.Println()

	for _, s := range stats {
		if s.matchCount > 0 {
			fmt.Printf("  %-40s %8s  %s\n",
				s.desc, humanize.Comma(int64(s.matchCount)),
				strings.Join(s.tags, ", "))
		}
	}

	fmt.Printf("\n  将为 %s 个文件添加 %s 个标签关联\n",
		bold(fmt.Sprintf("%d", len(toTag))),
		bold(fmt.Sprintf("%d", totalAssociations)))

	if dryRun {
		fmt.Printf("  %s\n\n", dim("（预览模式，未实际执行。去掉 --dry-run 以执行）"))
		return nil
	}

	// 执行打标签
	// 确保标签存在
	tagIDCache := make(map[string]int)
	for fileID, tagMap := range toTag {
		for tagName := range tagMap {
			tagID, ok := tagIDCache[tagName]
			if !ok {
				err := pool.QueryRow(ctx,
					"INSERT INTO tags (name) VALUES ($1) ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name RETURNING id",
					tagName).Scan(&tagID)
				if err != nil {
					return err
				}
				tagIDCache[tagName] = tagID
			}

			_, err := pool.Exec(ctx,
				"INSERT INTO file_tags (file_id, tag_id, source) VALUES ($1, $2, 'rule') ON CONFLICT DO NOTHING",
				fileID, tagID)
			if err != nil {
				return err
			}
		}
	}

	fmt.Printf("\n  %s 已完成自动标签\n\n", green("✓"))
	return nil
}

type compiledRule struct {
	rule    config.AutoTagRule
	nameRe  *regexp.Regexp
	minSize int64
	maxSize int64
	dir     string
}

func matchRule(cr compiledRule, name, ext, dir string, size int64) bool {
	// 扩展名匹配（OR）
	if len(cr.rule.Match.Ext) > 0 {
		matched := false
		for _, e := range cr.rule.Match.Ext {
			if strings.EqualFold(ext, e) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// 目录前缀
	if cr.dir != "" {
		if !strings.HasPrefix(dir, cr.dir) {
			return false
		}
	}

	// 文件名正则
	if cr.nameRe != nil {
		if !cr.nameRe.MatchString(name) {
			return false
		}
	}

	// 大小
	if cr.minSize > 0 && size < cr.minSize {
		return false
	}
	if cr.maxSize > 0 && size > cr.maxSize {
		return false
	}

	return true
}
