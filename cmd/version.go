package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示版本信息",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("jcqsearch %s\n", Version)
		fmt.Printf("构建时间: %s\n", BuildTime)
		fmt.Printf("提交: %s\n", CommitSHA)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
