package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ark/pkg/config"
)

// ScanResult 包含扫描评估信息
type ScanResult struct {
	Source     config.Source
	FileCount  int
	TotalSize  int64
	Volatility string
}

// ScanRoot 深度扫描目标总目录，自动评估子目录冷热度并排序 (默认配置)
func ScanRoot(scanRoot string) ([]ScanResult, error) {
	return ScanRootWithOptions(scanRoot, ScanOptions{})
}

// ScanRootWithOptions 深度扫描目标总目录，支持自定义排除过滤规则与忽略文件
func ScanRootWithOptions(scanRoot string, opts ScanOptions) ([]ScanResult, error) {
	entries, err := os.ReadDir(scanRoot)
	if err != nil {
		return nil, err
	}

	// 汇总所有排除模式 (外部指定 + .arkignore / .gitignore 文件)
	filePatterns := LoadIgnorePatterns(scanRoot, opts.IgnoreFile)
	allExcludes := make([]string, 0, len(opts.Excludes)+len(filePatterns))
	allExcludes = append(allExcludes, opts.Excludes...)
	allExcludes = append(allExcludes, filePatterns...)

	results := make([]ScanResult, 0)
	usedIDs := make(map[string]bool)
	rootFileCount := 0
	var rootFileSize int64

	for _, entry := range entries {
		name := entry.Name()
		fullPath := filepath.Join(scanRoot, name)

		// 1. 系统内置黑名单检查
		if IsIgnoredRootEntry(name) {
			continue
		}

		// 2. 自定义排除规则检查 (目录与文件均生效)
		if matched, pattern := MatchExcludePattern(name, fullPath, scanRoot, allExcludes); matched {
			if !opts.Silent {
				fmt.Printf("   [过滤排除] 忽略项: %s (匹配规则: %s)\n", name, pattern)
			}
			continue
		}

		if !entry.IsDir() {
			if info, err := entry.Info(); err == nil {
				rootFileCount++
				rootFileSize += info.Size()
			}
			continue
		}

		id := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				return r
			}
			return '_'
		}, name)

		// 防冲突机制：如果用户子目录的名字与系统专属保留舱位 ID 碰撞，自动调整 ID
		if id == config.DefaultRootFilesID {
			id = "dir_" + id
		}
		originalID := id
		seq := 1
		for usedIDs[id] {
			id = fmt.Sprintf("%s_%d", originalID, seq)
			seq++
		}
		usedIDs[id] = true

		score, count, size := evaluateDirectory(fullPath)

		vol := MediumDataLabel
		if score <= ScoreColdThreshold {
			vol = ColdDataLabel
		} else if score >= ScoreHotThreshold {
			vol = HotDataLabel
		}

		results = append(results, ScanResult{
			Source: config.Source{
				ID:       id,
				Name:     name,
				Path:     fullPath,
				Priority: score,
			},
			FileCount:  count,
			TotalSize:  size,
			Volatility: vol,
		})
	}

	if rootFileCount > 0 {
		results = append(results, ScanResult{
			Source: config.Source{
				ID:        config.DefaultRootFilesID,
				Name:      config.RootFilesCargoName,
				Path:      scanRoot,
				FilesOnly: true,
				Priority:  PriorityRootFiles, // 配置文件变动少，置于底层最优先复用
			},
			FileCount:  rootFileCount,
			TotalSize:  rootFileSize,
			Volatility: RootFilesDataLabel,
		})
	}

	// 关键：按照 Priority 升序排序 (冷数据在上，热数据在下)
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Source.Priority < results[j].Source.Priority
	})

	return results, nil
}

func evaluateDirectory(dirPath string) (score int, count int, size int64) {
	score = PriorityBase
	hasDB := false
	hasRecent := false
	now := time.Now()

	_ = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if IsDatabaseDir(info.Name()) {
				hasDB = true
			}
			return nil
		}

		count++
		size += info.Size()

		if IsDatabaseExt(info.Name()) {
			hasDB = true
		}

		if now.Sub(info.ModTime()) < 24*time.Hour {
			hasRecent = true
		}

		return nil
	})

	if hasDB {
		score += PriorityDBBonus
	}
	if hasRecent {
		score += PriorityRecentBonus
	}
	if count <= 5 && size <= 100*1024 {
		score -= PriorityColdDiscount
	}

	if score < PriorityMin {
		score = PriorityMin
	}
	if score > PriorityMax {
		score = PriorityMax
	}

	return score, count, size
}

// FormatBytes 将字节数转换为人类友好的可读字符串
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// PrintScanSummary 美化打印扫描结果表格
func PrintScanSummary(results []ScanResult) {
	fmt.Printf("%-20s %-12s %-22s %-8s %-10s %s\n", "舱位 ID", "装载优先级", "冷热变动特征", "文件数", "预估大小", "路径")
	fmt.Println(strings.Repeat("-", 95))
	for _, r := range results {
		displayID := fmt.Sprintf("[%s]", r.Source.ID)
		if r.Source.IsRootFiles() || r.Source.ID == config.DefaultRootFilesID {
			displayID = "[root_files]"
		} else if len(displayID) > 20 {
			displayID = displayID[:17] + "...]"
		}

		fmt.Printf("%-20s %-12s %-22s %-8d %-10s %s\n",
			displayID,
			fmt.Sprintf("优先级: %d", r.Source.Priority),
			r.Volatility,
			r.FileCount,
			FormatBytes(r.TotalSize),
			r.Source.Path,
		)
	}
	fmt.Println(strings.Repeat("-", 95))
	fmt.Println("【排序策略说明】：数值小的静态舱位排前优先复用，数值大的高频变动舱位排后，最大化 OCI / Docker 缓存命中率！")
}
