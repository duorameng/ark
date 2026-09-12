package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ark/pkg/archive"
	"ark/pkg/config"
	"ark/pkg/docker"
	"ark/pkg/github"
	"ark/pkg/oci"
)

func runLand(args []string) {
	cleanedArgs, cliEngine := extractEngineFlag(args)
	cleanedArgs, cliKey := extractKeyFlag(cleanedArgs)
	cleanedArgs, cliRepo := extractRepoFlag(cleanedArgs)
	args = cleanedArgs

	ws := getWorkspaceRoot()
	cfg, _, _ := LoadAppConfig(ws, cliRepo)

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

	token := loadToken(ws)

	// 取消固定 latest 标签：未显式指定具体时间戳航次时，自动通过 API 检索该分类下最新航次
	if tag == "" {
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
	engineDesc := "纯 Go 原生 OCI 流式直取 (Zero-Docker Pipeline, 无需 Docker 引擎)"
	if cliEngine == "docker" {
		engineDesc = "Docker 引擎调取与导出"
	}
	fmt.Printf("[引擎] 调取引擎: %s (%s)\n", strings.ToUpper(cliEngine), engineDesc)
	fmt.Printf("[卸货] 交付目的地: %s\n", destDir)
	fmt.Printf("[安全] 封条状态: %v\n", cfg.Encrypt)

	var sealPass []byte
	pass, _ := resolveSealKey(ws, false, cliKey)
	sealPass = pass

	tmpLandDir := filepath.Join(ws, "tmp", fmt.Sprintf("land_%d", time.Now().Unix()))
	_ = os.MkdirAll(tmpLandDir, 0755)
	defer os.RemoveAll(tmpLandDir)

	extracted := false
	if cliEngine == "oci" {
		parts := strings.Split(cfg.Repository, "/")
		regUser := "duorameng"
		if len(parts) >= 2 {
			regUser = parts[1]
		}

		ociClient, err := oci.NewClient(cfg.Repository, regUser, token)
		if err == nil {
			ctx := context.Background()
			fmt.Printf("\n==> 正在通过原生 OCI 协议调取班轮清单: %s...\n", fullImage)
			mf, err := ociClient.GetManifest(ctx, tag)
			if err == nil && mf != nil && len(mf.Layers) > 0 {
				fmt.Printf("✓ 成功获取清单，包含 %d 个货舱集装箱分层，开始流式调取与提取...\n", len(mf.Layers))
				allOk := true
				for idx, l := range mf.Layers {
					fmt.Printf("-> 正在流式提取货舱分层 [%d/%d] (指纹: %s...)\n", idx+1, len(mf.Layers), l.Digest[:19])
					if err := ociClient.DownloadBlobAndExtractCargo(ctx, l.Digest, tmpLandDir, nil); err != nil {
						fmt.Printf("   [-] 提取分层失败: %v\n", err)
						allOk = false
						break
					}
				}
				if allOk {
					extracted = true
					fmt.Println("✓ 原生 OCI 集装箱卸载提取成功 (零 Docker 守护进程依赖)！")
				}
			} else {
				fmt.Printf("[!] 提示: 原生 OCI 清单解析未完成 (%v)，尝试降级至 Docker 引擎...\n", err)
			}
		}
	}

	if !extracted {
		fmt.Printf("\n==> 正在靠岸进港，使用 Docker 调取班轮快照 %s...\n", fullImage)
		if err := docker.Pull(fullImage); err != nil {
			fmt.Printf("[!] 提示: 远端调取未成功，尝试使用本地停泊快照: %v\n", err)
		}

		fmt.Println("==> 正在吊装卸载集装箱...")
		if err := docker.ExtractCargoFromImage(fullImage, tmpLandDir); err != nil {
			fmt.Fprintf(os.Stderr, "[-] 导出集装箱失败: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println("\n------------------- 正在开封集装箱并归位货物 -------------------")
	folderNameMap := make(map[string]string)
	sourceMap := make(map[string]config.Source)
	for _, src := range cfg.Sources {
		folderNameMap[src.ID] = filepath.Base(src.Path)
		sourceMap[src.ID] = src
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

		// 精准判定：以 Source 配置中的 IsRootFiles() 为准，无配置时仅匹配系统专属 ID，杜绝同名常规文件夹冲突
		isRootFiles := false
		if src, exists := sourceMap[modName]; exists {
			isRootFiles = src.IsRootFiles()
		} else {
			isRootFiles = (modName == config.DefaultRootFilesID)
		}

		targetSubDir := filepath.Join(destDir, folderName)
		if isRootFiles {
			targetSubDir = destDir
		} else {
			_ = os.MkdirAll(targetSubDir, 0755)
		}

		fullFile := filepath.Join(tmpLandDir, fname)

		if archive.IsEncryptedArchive(fname) {
			if len(sealPass) == 0 {
				fmt.Fprintf(os.Stderr, "[-] 货舱 [%s] 包含 AES-256 安全密闭封条，但当前未配置解密口令！\n", fname)
				fmt.Fprintln(os.Stderr, "    提示: 请在命令行传入 --key \"<您的口令>\"，或在 .env 中配置 ARK_SEAL_KEY")
				os.Exit(1)
			}
			if isRootFiles {
				fmt.Printf("-> 正在开封并原位展开根级同级文件 (%s -> %s)...\n", fname, destDir)
			} else {
				fmt.Printf("-> 正在开封并流式还原 (%s -> %s, 零中间解密文件落盘)...\n", fname, targetSubDir)
			}
			if err := archive.UnsealAndUnpackStream(fullFile, targetSubDir, sealPass); err != nil {
				fmt.Fprintf(os.Stderr, "[-] 解封还原失败: %v\n", err)
				os.Exit(1)
			}
		} else {
			if isRootFiles {
				fmt.Printf("-> 正在解包并原位展开根级同级文件至 %s...\n", destDir)
			} else {
				fmt.Printf("-> 正在解包还原舱位货物: %s 到 %s...\n", folderName, targetSubDir)
			}
			if err := archive.UnpackTar(fullFile, targetSubDir); err != nil {
				fmt.Fprintf(os.Stderr, "[-] 还原解包失败: %v\n", err)
				os.Exit(1)
			}
		}
		if isRootFiles {
			fmt.Println("   ✓ 根级同级配置文件与脚本已成功原位展开归位！")
		} else {
			fmt.Printf("   ✓ 舱位 [%s] 货物已完整归位！\n", folderName)
		}
	}

	fmt.Println("\n================================================================")
	fmt.Printf("          🎉 下船清关完毕，所有货物已交付至: %s\n", destDir)
	fmt.Println("================================================================")
}
