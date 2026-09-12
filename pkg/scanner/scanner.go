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

// ScanRoot 深度扫描目标总目录，自动评估子目录冷热度并排序
func ScanRoot(scanRoot string) ([]ScanResult, error) {
	entries, err := os.ReadDir(scanRoot)
	if err != nil {
		return nil, err
	}

	results := make([]ScanResult, 0)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "ark" || strings.HasPrefix(name, ".") || name == "tmp" || name == "cache" {
			continue
		}

		fullPath := filepath.Join(scanRoot, name)

		id := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				return r
			}
			return '_'
		}, name)

		score, count, size := evaluateDirectory(fullPath)

		vol := "中频变动"
		if score <= 20 {
			vol = "极少变动 (冷数据)"
		} else if score >= 60 {
			vol = "频繁变动 (热数据)"
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

	// 关键：按照 Priority 升序排序 (冷数据在上，热数据在下)
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Source.Priority < results[j].Source.Priority
	})

	return results, nil
}

func evaluateDirectory(dirPath string) (score int, count int, size int64) {
	score = 30
	hasDB := false
	hasRecent := false
	now := time.Now()

	_ = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := strings.ToLower(info.Name())
			if base == "data" || base == "db" || base == "database" {
				hasDB = true
			}
			return nil
		}

		count++
		size += info.Size()

		ext := strings.ToLower(filepath.Ext(info.Name()))
		if ext == ".db" || ext == ".sqlite" || ext == ".sqlite3" || ext == ".sql" {
			hasDB = true
		}

		if now.Sub(info.ModTime()) < 24*time.Hour {
			hasRecent = true
		}

		return nil
	})

	if hasDB {
		score += 40
	}
	if hasRecent {
		score += 20
	}
	if count <= 5 && size <= 100*1024 {
		score -= 20
	}

	if score < 10 {
		score = 10
	}
	if score > 90 {
		score = 90
	}

	return score, count, size
}

// PrintScanSummary 美化打印扫描结果表格
func PrintScanSummary(results []ScanResult) {
	fmt.Printf("%-15s %-12s %-20s %-8s %s\n", "舱位 ID", "优先级", "变动特征", "文件数", "路径")
	fmt.Println(strings.Repeat("-", 78))
	for _, r := range results {
		fmt.Printf("%-15s %-12s %-20s %-8d %s\n",
			fmt.Sprintf("[%s]", r.Source.ID),
			fmt.Sprintf("优先级: %d", r.Source.Priority),
			r.Volatility,
			r.FileCount,
			r.Source.Path,
		)
	}
	fmt.Println(strings.Repeat("-", 78))
	fmt.Println("【排序策略说明】：数值小的静态舱位放前面，数值大的高频变动舱位放后面，以最大化 Docker 缓存命中！")
}
