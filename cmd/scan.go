package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ark/pkg/config"
	"ark/pkg/scanner"
)

func runScan(args []string) {
	cleanedArgs, cliExcludes := extractExcludeFlag(args)
	args = cleanedArgs

	ws := getWorkspaceRoot()
	loadEnvFile(ws)
	configPath := filepath.Join(ws, config.ConfigFileName)
	scanRoot := resolveBackupDir(ws, "")
	fromEnv := (scanRoot != ws)
	if len(args) > 0 {
		scanRoot = args[0]
		fromEnv = false
	}

	cfg, _, _ := LoadAppConfig(ws, "")

	// 汇总所有排除模式 (配置文件 + 环境变量 + 命令行 -e/--exclude)
	mergedExcludesMap := make(map[string]bool)
	for _, p := range cfg.GetExcludePatterns() {
		mergedExcludesMap[p] = true
	}
	for _, p := range resolveScanExcludeFromEnv(ws) {
		mergedExcludesMap[p] = true
	}
	for _, p := range cliExcludes {
		mergedExcludesMap[p] = true
	}

	allExcludes := make([]string, 0, len(mergedExcludesMap))
	for p := range mergedExcludesMap {
		allExcludes = append(allExcludes, p)
	}

	PrintBanner("🔍 Ark 货舱全自动扫描探测与变动率排序工具")
	if fromEnv {
		fmt.Printf("[扫描目标] 总目录: %s (自动读取自 .env / ARK_BACKUP_DIR)\n", scanRoot)
	} else {
		fmt.Printf("[扫描目标] 总目录: %s\n", scanRoot)
	}
	if len(allExcludes) > 0 {
		fmt.Printf("[过滤排除] 生效规则: %s\n", strings.Join(allExcludes, ", "))
	}

	gitMatcher := scanner.NewGitIgnoreMatcher(ws, scanRoot)
	gitMatcher.LoadDirRules(ws)
	if scanRoot != ws {
		gitMatcher.LoadDirRules(scanRoot)
	}
	gitMatcher.AddRules(allExcludes)

	opts := scanner.ScanOptions{
		Excludes: allExcludes,
		Matcher:  gitMatcher,
	}
	results, err := scanner.ScanRootWithOptions(scanRoot, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 扫描失败: %v\n", err)
		os.Exit(1)
	}

	scanner.PrintScanSummary(results)

	cfg.Sources = make([]config.Source, 0, len(results))
	for _, r := range results {
		cfg.Sources = append(cfg.Sources, r.Source)
	}
	if len(allExcludes) > 0 {
		cfg.Exclude = allExcludes
	}

	if err := cfg.Save(configPath); err != nil {
		fmt.Fprintf(os.Stderr, "[-] 保存配置失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✓ 已成功自动更新清单配置文件: %s\n", configPath)
}
