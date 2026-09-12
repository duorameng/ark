package cmd

import (
	"context"
	"fmt"
	"os"
)

func runList(args []string) {
	cleanedArgs, cliTarget := extractTargetFlag(args)
	cleanedArgs, cliRepo := extractRepoFlag(cleanedArgs)
	args = cleanedArgs

	ws := getWorkspaceRoot()
	loadEnvFile(ws)
	cfg, _, _ := LoadAppConfig(ws, cliRepo)

	categoryFilter := ""
	if len(args) > 0 {
		categoryFilter = args[0]
	}

	targets := resolveRegistryTargets(ws, cliTarget, cliRepo, cfg.Repository)
	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "[-] 未能识别到有效的镜像仓库港位。请检查 ark.toml 或 .env 配置。")
		os.Exit(1)
	}

	fmt.Println("================================================================")
	fmt.Println("          ⚓ Ark 港口航次查询终端 (Registry Query)             ")
	fmt.Println("================================================================")
	if categoryFilter != "" {
		fmt.Printf("[筛选] 指定分类: %s\n", categoryFilter)
	}

	ctx := context.Background()
	for _, target := range targets {
		fmt.Printf("\n--- 港位: %s (%s) ---\n", target.DisplayName, target.Repository)
		provider := target.Provider()
		if err := provider.ListVersions(ctx, categoryFilter); err != nil {
			fmt.Printf("[-] 查询 [%s] 航次失败: %v\n", target.DisplayName, err)
		}
	}
}

