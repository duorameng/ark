package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"ark/pkg/config"
	"ark/pkg/github"
)

func runList(args []string) {
	cleanedArgs, cliRepo := extractRepoFlag(args)
	args = cleanedArgs

	ws := getWorkspaceRoot()
	loadEnvFile(ws)
	configPath := filepath.Join(ws, "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 加载配置失败: %v\n", err)
		os.Exit(1)
	}

	cfg.Repository = resolveRepository(cfg.Repository, cliRepo)

	categoryFilter := ""
	if len(args) > 0 {
		categoryFilter = args[0]
	}

	token := loadToken(ws)
	if token == "" {
		fmt.Fprintln(os.Stderr, "[-] 未检测到通行凭据 GH_TOKEN，无法检索远端港口。请在 .env 中配置。")
		os.Exit(1)
	}

	fmt.Println("================================================================")
	fmt.Println("          ⚓ Ark 港口航次查询终端 (Golang Engine)               ")
	fmt.Println("================================================================")
	fmt.Printf("[港位] 目标仓库: %s\n", cfg.Repository)
	if categoryFilter != "" {
		fmt.Printf("[筛选] 指定分类: %s\n", categoryFilter)
	}

	ghClient := github.NewClient(cfg.Repository, token)
	if err := ghClient.PrintCategoryVersions(categoryFilter); err != nil {
		fmt.Fprintf(os.Stderr, "[-] 查询失败: %v\n", err)
		os.Exit(1)
	}
}
