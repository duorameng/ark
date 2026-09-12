package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// Source 定义单个货舱舱位数据源
type Source struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	FilesOnly bool   `json:"files_only,omitempty"` // 为 true 时仅备份该目录下的同级文件 (不递归子目录)
	Priority  int    `json:"priority,omitempty"`   // 优先级/冷热度 (1-99，数值小排前面)
}

// IsRootFiles 判断该舱位是否为根目录同级文件归集舱位 (精确防冲突)
func (s *Source) IsRootFiles() bool {
	if s.FilesOnly {
		return true
	}
	// 仅当 ID 完全匹配系统专属保留 ID 时识别，杜绝用户同名普通文件夹冲突
	return s.ID == DefaultRootFilesID
}

// Config 航运清运配置
type Config struct {
	Repository        string   `json:"repository"`
	Category          string   `json:"category"`
	RetentionCount    int      `json:"retention_count"`
	Encrypt           bool     `json:"encrypt"`
	TagPrecision      string   `json:"tag_precision,omitempty"`        // 时间标签精度: day(天) / second(秒，默认) / minute(分)
	PushRetry         int      `json:"push_retry,omitempty"`           // docker push 失败重试次数 (默认 3 次)
	CleanAfterPush    *bool    `json:"clean_after_push,omitempty"`     // build/push 完成后是否自动清理本地镜像与构建缓存 (默认 true)
	CleanAllAfterPush *bool    `json:"clean_all_after_push,omitempty"` // build/push 完成后是否彻底清空 cache/tmp 并重置初始状态 (默认 false)
	Sources           []Source `json:"sources"`
}

// ShouldCleanAfterPush 判断构建推送后是否需要清理本地镜像与缓存
func (c *Config) ShouldCleanAfterPush() bool {
	if c.CleanAfterPush != nil {
		return *c.CleanAfterPush
	}
	return true
}

// ShouldCleanAllAfterPush 判断构建推送后是否需要彻底清空本地缓存集装箱与临时数据
func (c *Config) ShouldCleanAllAfterPush() bool {
	if c.CleanAllAfterPush != nil {
		return *c.CleanAllAfterPush
	}
	return false
}

// DefaultConfig 返回预设默认配置
func DefaultConfig() *Config {
	defaultClean := true
	return &Config{
		Repository:     DefaultRepository,
		Category:       DefaultCategory,
		RetentionCount: DefaultRetentionCount,
		Encrypt:        true,
		TagPrecision:   DefaultTagPrecision,
		PushRetry:      DefaultPushRetry,
		CleanAfterPush: &defaultClean,
		Sources:        make([]Source, 0),
	}
}

// Load 从指定路径加载并解析配置
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if cfg.Category == "" {
		cfg.Category = DefaultCategory
	}
	if cfg.RetentionCount <= 0 {
		cfg.RetentionCount = DefaultRetentionCount
	}

	return cfg, nil
}

// Save 将配置保存到文件
func (c *Config) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, append(data, '\n'), 0644)
}

// SortedSources 返回按照优先级升序排列的货舱源 (冷数据在前，热数据在后)
func (c *Config) SortedSources() []Source {
	res := make([]Source, len(c.Sources))
	copy(res, c.Sources)

	sort.SliceStable(res, func(i, j int) bool {
		p1 := res[i].Priority
		if p1 <= 0 {
			p1 = 50
		}
		p2 := res[j].Priority
		if p2 <= 0 {
			p2 = 50
		}
		return p1 < p2
	})

	return res
}
