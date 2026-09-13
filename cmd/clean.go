package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runClean(args []string) {
	cleanedArgs, cliTarget := extractTargetFlag(args)
	cleanedArgs, cliRepo := extractRepoFlag(cleanedArgs)
	cleanedArgs, _ = extractTagFlag(cleanedArgs)
	cleanedArgs, _ = extractCategoryFlag(cleanedArgs)
	args = cleanedArgs

	ws := getWorkspaceRoot()
	loadEnvFile(ws)
	cfg, _, _ := LoadAppConfig(ws, cliRepo)

	includeAll := false
	cleanUntagged := false

	for _, arg := range args {
		switch arg {
		case "--all", "-a":
			includeAll = true
			cleanUntagged = true
		case "--docker", "-d":
			// 保持参数兼容性
		case "--untagged", "-u", "--remote", "-r", "--remote-untagged":
			cleanUntagged = true
		case "--help", "-h":
			printCleanHelp()
			return
		}
	}

	PrintBanner("🧹 Ark 本地工作区与环境重置系统 (Workspace Reset)")
	fmt.Printf("[重置] 工作区根目录: %s\n\n", ws)

	// 1. 清理 cache/ 目录
	cacheDir := filepath.Join(ws, "cache")
	cleanedCacheFiles := 0
	if entries, err := os.ReadDir(cacheDir); err == nil {
		for _, e := range entries {
			p := filepath.Join(cacheDir, e.Name())
			_ = os.RemoveAll(p)
			cleanedCacheFiles++
		}
	}
	_ = os.MkdirAll(cacheDir, 0755)
	fmt.Printf("✓ [集装箱缓存] 已清空 cache/ 目录 (清理了 %d 个数据缓存与清单项)\n", cleanedCacheFiles)

	// 2. 清理 tmp/ 目录
	tmpDir := filepath.Join(ws, "tmp")
	cleanedTmpFiles := 0
	if entries, err := os.ReadDir(tmpDir); err == nil {
		for _, e := range entries {
			p := filepath.Join(tmpDir, e.Name())
			_ = os.RemoveAll(p)
			cleanedTmpFiles++
		}
	}
	_ = os.MkdirAll(tmpDir, 0755)
	fmt.Printf("✓ [临时工作层] 已清空 tmp/ 目录 (清理了 %d 个临时构建文件与目录)\n", cleanedTmpFiles)

	// 3. 清理工作区历史导出的货物目录 (cargo_landed_* 与 cargo_restored_*)
	cleanedCargos := 0
	if entries, err := os.ReadDir(ws); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, "cargo_landed_") || strings.HasPrefix(name, "cargo_restored_") {
				p := filepath.Join(ws, name)
				_ = os.RemoveAll(p)
				fmt.Printf("   -> 已移除历史交付货物目录: %s\n", name)
				cleanedCargos++
			}
		}
	}
	if cleanedCargos > 0 {
		fmt.Printf("✓ [历史交付物] 已清理 %d 个解压归位的落地货物目录\n", cleanedCargos)
	} else {
		fmt.Println("✓ [历史交付物] 工作区无历史残留的货物导出目录")
	}

	fmt.Println("✓ [纯净环境] Ark 全链路采用原生 OCI 流式引擎，本地零 Docker 垃圾残留")

	// 4. 如果指定了 --untagged 或 --all，扫描并清理远端孤立未打标版本
	if cleanUntagged {
		targets := resolveRegistryTargets(ws, cliTarget, cliRepo, cfg.Repository)
		if len(targets) == 0 {
			fmt.Println("\n[-] 未检测到有效的目标港位，跳过远端未打标版本清理。")
		} else {
			ctx := context.Background()
			for _, target := range targets {
				fmt.Printf("\n------------------- 正在维护远端港位: %s (%s) -------------------\n", target.DisplayName, target.Repository)
				provider := target.Provider()
				deletedCount, err := provider.PruneUntagged(ctx)
				if err != nil {
					fmt.Printf("[-] 维护远端港位遇到异常: %v\n", err)
				} else if deletedCount > 0 {
					fmt.Printf("✓ 远端港位 [%s] 维护完毕，已清理 %d 个孤立悬空版本！\n", target.DisplayName, deletedCount)
				}
			}
		}
	}

	if includeAll {
		fmt.Println("\n[!] 提示: 已根据 --all 参数完成本地工作区与远端注册表环境的重置维护！")
	} else if !cleanUntagged {
		fmt.Println("\n💡 提示: 若需扫描并清理远端港口悬空的孤立未打标版本，可执行: ark clean --untagged (或 ark clean --all)")
	}

	fmt.Println("\n================================================================")
	fmt.Println("          🎉 工作区重置完毕，已恢复至 Ark 初始干净状态！          ")
	fmt.Println("================================================================")
}

func printCleanHelp() {
	fmt.Println("用法 (Usage):")
	fmt.Println("  ark clean [options...]")
	fmt.Println()
	fmt.Println("选项 (Options):")
	fmt.Println("  -u, --untagged        自动扫描并清理远端注册表孤立/悬空的未打标 (untagged) 版本")
	fmt.Println("  -r, --remote          清理远端资源 (等同于 --untagged)")
	fmt.Println("  -t, --target <alias>  指定维护特定远端注册表通道 (如 aliyun, github)")
	fmt.Println("  -a, --all             全量彻底重置: 本地缓存、临时文件与远端注册表 untagged 版本")
	fmt.Println("      --repo <repo>     临时指定目标仓库地址")
	fmt.Println("  -h, --help            显示本清理指南")
}
