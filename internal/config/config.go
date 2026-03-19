package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Database DatabaseConfig `mapstructure:"database"`
	Scan     ScanConfig     `mapstructure:"scan"`
	Search   SearchConfig   `mapstructure:"search"`
	Ignore   IgnoreConfig   `mapstructure:"ignore"`
	AutoTag  AutoTagConfig  `mapstructure:"auto_tag"`
}

type AutoTagRule struct {
	Match AutoTagMatch `mapstructure:"match"`
	Tags  []string     `mapstructure:"tags"`
}

type AutoTagMatch struct {
	Ext     []string `mapstructure:"ext"`
	Dir     string   `mapstructure:"dir"`
	Name    string   `mapstructure:"name"`
	Larger  string   `mapstructure:"larger"`
	Smaller string   `mapstructure:"smaller"`
}

type AutoTagConfig struct {
	Rules []AutoTagRule `mapstructure:"rules"`
}

type IgnoreConfig struct {
	Dirs  []string `mapstructure:"dirs"`  // 忽略的目录名
	Exts  []string `mapstructure:"exts"`  // 忽略的扩展名（不含点号）
	Globs []string `mapstructure:"globs"` // 忽略的 glob 模式
}

type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	DBName   string `mapstructure:"dbname"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	SSLMode  string `mapstructure:"sslmode"`
}

func (d *DatabaseConfig) ConnString() string {
	return fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		d.Host, d.Port, d.DBName, d.User, d.Password, d.SSLMode)
}

type ScanConfig struct {
	BatchSize   int `mapstructure:"batch_size"`
	Concurrency int `mapstructure:"concurrency"`
}

type SearchConfig struct {
	DefaultLimit        int     `mapstructure:"default_limit"`
	SimilarityThreshold float64 `mapstructure:"similarity_threshold"`
}

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("$HOME/.jcqsearch")
	viper.AddConfigPath(".")

	viper.SetEnvPrefix("JCQSEARCH")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// 默认值
	viper.SetDefault("database.host", "localhost")
	viper.SetDefault("database.port", 5432)
	viper.SetDefault("database.dbname", "jcqsearch")
	viper.SetDefault("database.user", "shawn")
	viper.SetDefault("database.password", "")
	viper.SetDefault("database.sslmode", "disable")
	viper.SetDefault("scan.batch_size", 1000)
	viper.SetDefault("scan.concurrency", 4)
	viper.SetDefault("search.default_limit", 20)
	viper.SetDefault("search.similarity_threshold", 0.1)

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("读取配置文件失败: %w", err)
		}
		// 配置文件不存在时使用默认值
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	return &cfg, nil
}
