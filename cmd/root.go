package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"jcqsearch/internal/config"
	"jcqsearch/internal/database"
)

var (
	cfg  *config.Config
	pool *pgxpool.Pool
)

var rootCmd = &cobra.Command{
	Use:   "jcqsearch",
	Short: "本地文件快速搜索工具",
	Long: `jcqsearch — 基于 PostgreSQL pg_trgm 的本地文件搜索工具。

支持中文文件名模糊搜索，关键词 + 文件类型 + 时间范围 + 文件大小的组合筛选。`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// 加载配置
		var err error
		cfg, err = config.Load()
		if err != nil {
			return fmt.Errorf("加载配置失败: %w", err)
		}

		// 建立数据库连接
		pool, err = database.NewPool(context.Background(), &cfg.Database)
		if err != nil {
			return fmt.Errorf("连接数据库失败: %w", err)
		}

		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if pool != nil {
			pool.Close()
		}
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
