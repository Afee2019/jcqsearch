package model

import "time"

type FileEntry struct {
	ID         int64
	Path       string
	Dir        string
	Name       string
	Stem       string
	Ext        string
	IsDir      bool
	Size       int64
	ModTime    time.Time
	ScanPathID int
	ScannedAt  time.Time
}

type ScanPath struct {
	ID        int
	Path      string
	Label     string
	Enabled   bool
	MaxDepth  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type IgnoreRule struct {
	ID        int
	Pattern   string
	RuleType  string // "dir", "ext", "glob"
	Enabled   bool
	IsDefault bool
	CreatedAt time.Time
}
