package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"ark/pkg/config"
	"ark/pkg/scanner"
)

func runScan(args []string) {
	ws := getWorkspaceRoot()
	configPath := filepath.Join(ws, "config.json")
	scanRoot := "/root/workspace"
	if len(args) > 0 {
		scanRoot = args[0]
	}

	fmt.Println("================================================================")
	fmt.Println("        🔍 Ark 货舱全自动扫描探测与变动率排序工具 (Golang)      ")
	fmt.Println("================================================================")
	fmt.Printf("[扫描目标] 总目录: %s\n", scanRoot)

	results, err := scanner.ScanRoot(scanRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 扫描失败: %v\n", err)
		os.Exit(1)
	}

	scanner.PrintScanSummary(results)

	cfg, _ := config.Load(configPath)
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	cfg.Sources = make([]config.Source, 0, len(results))
	for _, r := range results {
		cfg.Sources = append(cfg.Sources, r.Source)
	}

	if err := cfg.Save(configPath); err != nil {
		fmt.Fprintf(os.Stderr, "[-] 保存配置失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✓ 已成功自动更新清单配置文件: %s\n", configPath)
}
