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
	loadEnvFile(ws)
	configPath := filepath.Join(ws, config.ConfigFileName)
	scanRoot := resolveBackupDir(ws, "")
	fromEnv := (scanRoot != ws)
	if len(args) > 0 {
		scanRoot = args[0]
		fromEnv = false
	}

	fmt.Println("================================================================")
	fmt.Println("        🔍 Ark 货舱全自动扫描探测与变动率排序工具 (Golang)      ")
	fmt.Println("================================================================")
	if fromEnv {
		fmt.Printf("[扫描目标] 总目录: %s (自动读取自 .env / ARK_BACKUP_DIR)\n", scanRoot)
	} else {
		fmt.Printf("[扫描目标] 总目录: %s\n", scanRoot)
	}

	results, err := scanner.ScanRoot(scanRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 扫描失败: %v\n", err)
		os.Exit(1)
	}

	scanner.PrintScanSummary(results)

	cfg, _, _ := LoadAppConfig(ws, "")

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
