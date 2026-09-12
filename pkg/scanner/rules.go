package scanner

import (
	"path/filepath"
	"strings"
)

// 系统预置与忽略规则集中定义
var (
	// IgnoredRootEntries 扫描根目录时默认忽略的系统文件或运行临时目录
	IgnoredRootEntries = map[string]bool{
		"ark":     true,
		"ark.exe": true,
		"tmp":     true,
		"cache":   true,
		".git":    true,
	}

	// DatabaseDirNames 具备数据库/高频变动特征的常见目录名
	DatabaseDirNames = map[string]bool{
		"data":     true,
		"db":       true,
		"database": true,
		"postgres": true,
		"mysql":    true,
		"sqlite":   true,
		"redis":    true,
		"mongodb":  true,
	}

	// DatabaseExtensions 具备数据库或数据快照特征的常见文件扩展名
	DatabaseExtensions = map[string]bool{
		".db":      true,
		".sqlite":  true,
		".sqlite3": true,
		".sql":     true,
		".rdb":     true,
		".dump":    true,
		".bak":     true,
		".db3":     true,
	}
)

// 扫描排序评分策略权重与阈值常量
const (
	PriorityRootFiles    = 10 // 根级文件优先级 (置于最前作为底座缓存)
	PriorityBase         = 30 // 普通业务目录基准分
	PriorityDBBonus      = 40 // 包含数据库特征的额外加分
	PriorityRecentBonus  = 20 // 24小时内有文件写入的额外加分
	PriorityColdDiscount = 20 // 极少文件且尺寸极小的冷目录降分
	PriorityMin          = 10 // 最低优先级 (最高缓存权重)
	PriorityMax          = 90 // 最高优先级 (最热数据，最后打包)

	ScoreColdThreshold = 20 // 冷数据分界线 (<= 20)
	ScoreHotThreshold  = 60 // 热数据分界线 (>= 60)

	ColdDataLabel      = "极少变动 (冷数据)"
	MediumDataLabel    = "中频变动"
	HotDataLabel       = "频繁变动 (热数据)"
	RootFilesDataLabel = "极少变动 (根级同级文件)"
)

// IsIgnoredRootEntry 判断是否为根目录扫描时应当忽略的项
func IsIgnoredRootEntry(name string) bool {
	lower := strings.ToLower(name)
	if IgnoredRootEntries[lower] {
		return true
	}
	if strings.HasPrefix(lower, ".git") {
		return true
	}
	return false
}

// IsDatabaseDir 判断目录名是否符合数据库特征
func IsDatabaseDir(name string) bool {
	return DatabaseDirNames[strings.ToLower(name)]
}

// IsDatabaseExt 判断文件扩展名是否符合数据库特征
func IsDatabaseExt(fileName string) bool {
	ext := strings.ToLower(filepath.Ext(fileName))
	return DatabaseExtensions[ext]
}
