package cmd

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var ignoreCmd = &cobra.Command{
	Use:   "ignore",
	Short: "管理忽略规则",
}

var ignoreListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有忽略规则",
	RunE: func(cmd *cobra.Command, args []string) error {
		dim := color.New(color.Faint).SprintFunc()

		fmt.Printf("\n%s\n", dim("配置文件: "+viper.ConfigFileUsed()))

		fmt.Printf("\n  忽略目录 (%d 条):\n", len(cfg.Ignore.Dirs))
		for _, d := range cfg.Ignore.Dirs {
			fmt.Printf("    - %s\n", d)
		}

		fmt.Printf("\n  忽略扩展名 (%d 条):\n", len(cfg.Ignore.Exts))
		for _, e := range cfg.Ignore.Exts {
			fmt.Printf("    - %s\n", e)
		}

		fmt.Printf("\n  忽略 glob 模式 (%d 条):\n", len(cfg.Ignore.Globs))
		for _, g := range cfg.Ignore.Globs {
			fmt.Printf("    - %s\n", g)
		}

		total := len(cfg.Ignore.Dirs) + len(cfg.Ignore.Exts) + len(cfg.Ignore.Globs)
		fmt.Printf("\n  共 %d 条规则\n\n", total)
		fmt.Printf("  %s\n\n", dim("编辑配置文件 config.yaml 的 ignore 段可增删规则，修改后立即生效（下次 scan 时应用）"))
		return nil
	},
}

func init() {
	ignoreCmd.AddCommand(ignoreListCmd)
	rootCmd.AddCommand(ignoreCmd)
}
