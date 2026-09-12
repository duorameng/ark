package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ark/pkg/archive"
	"ark/pkg/config"
	"ark/pkg/docker"
	"ark/pkg/github"
)

func runLand(args []string) {
	cleanedArgs, cliKey := extractKeyFlag(args)
	cleanedArgs, cliRepo := extractRepoFlag(cleanedArgs)
	args = cleanedArgs

	ws := getWorkspaceRoot()
	configPath := filepath.Join(ws, "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 加载配置失败: %v\n", err)
		os.Exit(1)
	}

	cfg.Repository = resolveRepository(cfg.Repository, cliRepo)

	category := cfg.Category
	var tag string
	destDir := ""

	if len(args) > 0 {
		param := args[0]
		// 支持直接输入完整镜像名+标签 (例如 ghcr.io/org/repo:category-20260912-120000)
		if strings.Contains(param, ":") {
			parts := strings.SplitN(param, ":", 2)
			cfg.Repository = parts[0]
			param = parts[1]
		}
		if strings.Contains(param, "-") && len(strings.Split(param, "-")) >= 3 {
			tag = param
			category = strings.SplitN(param, "-", 2)[0]
		} else {
			category = param
		}
	}

	// 取消固定 latest 标签：未显式指定具体时间戳航次时，自动通过 API 检索该分类下最新航次
	if tag == "" {
		token := loadToken(ws)
		if token != "" {
			fmt.Printf("[航次] 正在查询分类 [%s] 在远端港口的最新航次...\n", category)
			ghClient := github.NewClient(cfg.Repository, token)
			if latestTag, err := ghClient.GetLatestTag(category); err == nil {
				tag = latestTag
				fmt.Printf("[航次] 自动定位最新航次: %s\n", tag)
			} else {
				fmt.Fprintf(os.Stderr, "[-] 自动检索最新航次失败: %v\n请通过 'ark land <具体航次标签>' 指定航次\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Fprintf(os.Stderr, "[-] 未配置 GitHub Token 且未指定具体航次标签。\n请使用 'ark land %s-<日期标签>' 指定具体航次，或配置 GH_TOKEN。\n", category)
			os.Exit(1)
		}
	}

	if len(args) > 1 {
		destDir = args[1]
	} else {
		destDir = filepath.Join(ws, fmt.Sprintf("cargo_landed_%s", category))
	}

	fullImage := fmt.Sprintf("%s:%s", cfg.Repository, tag)

	fmt.Println("================================================================")
	fmt.Println("          ⚓ Ark 班轮靠岸下船系统 (Landing System)              ")
	fmt.Println("================================================================")
	fmt.Printf("[港位] 来源港位: %s\n", cfg.Repository)
	fmt.Printf("[场景] 所属分类: %s\n", category)
	fmt.Printf("[航次] 检索标签: %s\n", tag)
	fmt.Printf("[卸货] 交付目的地: %s\n", destDir)
	fmt.Printf("[安全] 封条状态: %v\n", cfg.Encrypt)

	var sealPass []byte
	if cfg.Encrypt {
		pass, err := resolveSealKey(ws, false, cliKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[-] 获取安全封条密钥失败: %v\n", err)
			os.Exit(1)
		}
		sealPass = pass
	}

	tmpLandDir := filepath.Join(ws, "tmp", fmt.Sprintf("land_%d", time.Now().Unix()))
	_ = os.MkdirAll(tmpLandDir, 0755)
	defer os.RemoveAll(tmpLandDir)

	fmt.Printf("\n==> 正在靠岸进港，调取班轮快照 %s...\n", fullImage)
	if err := docker.Pull(fullImage); err != nil {
		fmt.Printf("[!] 提示: 远端调取未成功，尝试使用本地停泊快照: %v\n", err)
	}

	fmt.Println("==> 正在吊装卸载集装箱...")
	if err := docker.ExtractCargoFromImage(fullImage, tmpLandDir); err != nil {
		fmt.Fprintf(os.Stderr, "[-] 导出集装箱失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n------------------- 正在开封集装箱并归位货物 -------------------")
	folderNameMap := make(map[string]string)
	for _, src := range cfg.Sources {
		folderNameMap[src.ID] = filepath.Base(src.Path)
	}

	entries, _ := os.ReadDir(tmpLandDir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fname := e.Name()
		modName := strings.TrimSuffix(fname, filepath.Ext(fname))
		modName = strings.TrimSuffix(modName, ".tar")

		folderName := modName
		if realName, ok := folderNameMap[modName]; ok && realName != "" && realName != "." && realName != "/" {
			folderName = realName
		}

		targetSubDir := filepath.Join(destDir, folderName)
		_ = os.MkdirAll(targetSubDir, 0755)

		fullFile := filepath.Join(tmpLandDir, fname)
		tarPath := fullFile

		if strings.HasSuffix(fname, ".dat") || strings.HasSuffix(fname, ".tar.enc") {
			decryptedTar := filepath.Join(tmpLandDir, modName+".tar")
			fmt.Printf("-> 正在开启安全封条 (%s)...\n", fname)
			if err := archive.UnsealFile(fullFile, decryptedTar, sealPass); err != nil {
				fmt.Fprintf(os.Stderr, "[-] 解封失败: %v\n", err)
				os.Exit(1)
			}
			tarPath = decryptedTar
		}

		fmt.Printf("-> 正在还原舱位货物: %s 到 %s...\n", folderName, targetSubDir)
		if err := archive.UnpackTar(tarPath, targetSubDir); err != nil {
			fmt.Fprintf(os.Stderr, "[-] 还原解包失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("   ✓ 舱位 [%s] 货物已完整归位！\n", folderName)
	}

	fmt.Println("\n================================================================")
	fmt.Printf("          🎉 下船清关完毕，所有货物已交付至: %s\n", destDir)
	fmt.Println("================================================================")
}
