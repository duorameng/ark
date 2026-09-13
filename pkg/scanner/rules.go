package scanner

import (
	"os"
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

// ScanOptions 目录扫描与变动评估控制参数
type ScanOptions struct {
	Excludes   []string // 外部传入的排除过滤规则 (例如: "watchover", "cache", "*.tmp")
	IgnoreFile string   // 指定的 ignore 规则文件路径，为空时自动检测 .arkignore / .gitignore
	Silent     bool     // 是否静默输出 (不打印忽略项回显)
}

// IsIgnoredRootEntry 判断是否为根目录扫描时应当默认忽略的项
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

// LoadIgnorePatterns 从指定目录的 .arkignore 或 .gitignore 加载排除模式
func LoadIgnorePatterns(scanRoot string, customIgnoreFile string) []string {
	var filesToTry []string
	if customIgnoreFile != "" {
		filesToTry = append(filesToTry, customIgnoreFile)
	} else {
		filesToTry = append(filesToTry,
			filepath.Join(scanRoot, ".arkignore"),
			filepath.Join(scanRoot, ".gitignore"),
		)
	}

	patterns := make([]string, 0)
	for _, f := range filesToTry {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			patterns = append(patterns, line)
		}
		// 优先找到并加载第一个即可 (.arkignore > .gitignore)
		break
	}
	return patterns
}

// MatchExcludePattern 判断文件或目录是否匹配任意排除规则，若匹配返回 true 及命中的规则文本
func MatchExcludePattern(name, fullPath, scanRoot string, patterns []string) (bool, string) {
	lowerName := strings.ToLower(name)
	relPath := name
	if scanRoot != "" {
		if r, err := filepath.Rel(scanRoot, fullPath); err == nil && r != "." {
			relPath = filepath.ToSlash(r)
		}
	}
	lowerRel := strings.ToLower(relPath)

	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		pClean := filepath.ToSlash(strings.TrimSuffix(strings.TrimSuffix(p, "/"), "\\"))
		lowerP := strings.ToLower(pClean)

		// 1. 精确名称或相对路径匹配 (大小写不敏感)
		if lowerName == lowerP || lowerRel == lowerP {
			return true, p
		}

		// 2. 通配符匹配 (如 *.tmp, *-demo, test_*)
		if matched, _ := filepath.Match(lowerP, lowerName); matched {
			return true, p
		}
		if matched, _ := filepath.Match(lowerP, lowerRel); matched {
			return true, p
		}

		// 3. 目录名作为路径前缀包含 (如 rules 为 "data" 时，匹配 "data/sub")
		if strings.HasPrefix(lowerRel, lowerP+"/") {
			return true, p
		}
	}
	return false, ""
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
