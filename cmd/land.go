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
	"ark/pkg/oci"
)

func runLand(args []string) {
	cleanedArgs, cliEngine := extractEngineFlag(args)
	cleanedArgs, cliKey := extractKeyFlag(cleanedArgs)
	cleanedArgs, cliTarget := extractTargetFlag(cleanedArgs)
	cleanedArgs, cliRepo := extractRepoFlag(cleanedArgs)
	cleanedArgs, cliDest := extractDestFlag(cleanedArgs)
	cleanedArgs, cliTag := extractTagFlag(cleanedArgs)
	cleanedArgs, cliCategory := extractCategoryFlag(cleanedArgs)
	args = cleanedArgs

	ws := getWorkspaceRoot()
	loadEnvFile(ws)
	cfg, _, _ := LoadAppConfig(ws, cliRepo)

	targets := resolveRegistryTargets(ws, cliTarget, cliRepo, cfg.Repository)
	if len(targets) == 0 {
		fmt.Fprintf(os.Stderr, "[-] 靠岸失败，未能识别到有效的目标港位\n")
		os.Exit(1)
	}
	activeTarget := targets[0]
	cfg.Repository = activeTarget.Repository

	category := cfg.Category
	if cliCategory != "" {
		category = cliCategory
	}
	var tag string
	destDir := ""

	for _, arg := range args {
		if arg == "--latest" || arg == "-l" || arg == "--fixed" {
			tag = "latest"
			break
		}
	}

	if cliTag != "" {
		if strings.Contains(cliTag, ":") {
			parts := strings.SplitN(cliTag, ":", 2)
			if parts[0] != "" {
				cfg.Repository = parts[0]
			}
			cliTag = parts[1]
		}
		cliTag = strings.TrimPrefix(cliTag, ":")
		tag = cliTag
		if cliCategory == "" {
			if strings.Contains(tag, "-") {
				category = strings.SplitN(tag, "-", 2)[0]
			} else {
				category = tag
			}
		}
	}

	explicitCustomTag := ""
	if tag == "" && len(args) > 0 {
		param := args[0]
		// 支持直接输入完整镜像名+标签 (例如 ghcr.io/org/repo:category-20260912-120000 或 :latest)
		if strings.Contains(param, ":") {
			parts := strings.SplitN(param, ":", 2)
			if parts[0] != "" {
				cfg.Repository = parts[0]
			}
			param = parts[1]
		}
		if strings.EqualFold(param, "latest") || strings.EqualFold(param, "fixed") {
			tag = "latest"
		} else if strings.Contains(param, "-") && len(strings.Split(param, "-")) >= 3 {
			tag = param
			category = strings.SplitN(param, "-", 2)[0]
		} else if strings.Contains(param, "/") || strings.Contains(param, "\\") {
			// 若单参数包含路径分隔符，智能识别为用户希望恢复到的目标路径，分类自动取默认
			if cliDest == "" {
				cliDest = param
			}
		} else if !strings.HasPrefix(param, "-") {
			category = param
			explicitCustomTag = param
		}
	}

	token := activeTarget.Password
	if token == "" {
		token = loadToken(ws)
	}

	if tag == "" && cfg.FixedTag != "" {
		tag = cfg.FixedTag
		fmt.Printf("[航次] 匹配配置中固定航次标签: %s\n", tag)
	}

	provider := activeTarget.Provider()
	ctx := context.Background()

	// 若未显式指定具体时间戳航次时，委托 Provider 智能解析最新航次：
	if tag == "" {
		fmt.Printf("[航次] 正在查询分类 [%s] 在远端港口的目标航次...\n", category)
		latestTag, err := provider.ResolveLatestTag(ctx, category)
		if err == nil && latestTag != "" && latestTag != "latest" {
			tag = latestTag
			fmt.Printf("[航次] 自动定位最新航次: %s\n", tag)
		} else if explicitCustomTag != "" {
			tag = explicitCustomTag
			fmt.Printf("[航次] 匹配指定航次标签: %s\n", tag)
		} else if latestTag != "" {
			tag = latestTag
			fmt.Printf("[航次] 调取最新航次标签: %s\n", tag)
		} else {
			tag = "latest"
			fmt.Printf("[航次] 未检索到时间戳历史航次，调取固定最新标签: %s\n", tag)
		}
	}

	// 综合确定目标落地部署目录：命令行 -o/--dest > 命令行第 2 参数 > 环境变量 (ARK_DEST_DIR / ARK_DEPLOY_DIR / ARK_RESTORE_DIR) > 默认落地目录
	if cliDest != "" {
		destDir = cliDest
	} else if len(args) > 1 {
		destDir = args[1]
	} else if envDest := os.Getenv("ARK_DEST_DIR"); envDest != "" {
		destDir = envDest
	} else if envDeploy := os.Getenv("ARK_DEPLOY_DIR"); envDeploy != "" {
		destDir = envDeploy
	} else if envRestore := os.Getenv(config.EnvArkRestoreDir); envRestore != "" {
		destDir = envRestore
	} else {
		destDir = filepath.Join(ws, fmt.Sprintf("cargo_landed_%s", category))
	}
	if !filepath.IsAbs(destDir) {
		destDir = filepath.Join(ws, destDir)
	}

	fullImage := fmt.Sprintf("%s:%s", cfg.Repository, tag)

	PrintBanner("⚓ Ark 班轮靠岸下船系统 (Landing System)")
	fmt.Printf("[港位] 来源港位: %s (%s)\n", provider.Repository(), provider.DisplayName())
	fmt.Printf("[主机] 港口主机: %s\n", provider.Host())
	fmt.Printf("[场景] 所属分类: %s\n", category)
	fmt.Printf("[航次] 检索标签: %s\n", tag)
	_ = cliEngine // 保留参数兼容性
	fmt.Println("[引擎] 调取引擎: 原生 OCI 流式直取 (Zero-Docker Pipeline, 无需 Docker)")
	fmt.Printf("[卸货] 交付目的地: %s\n", destDir)
	fmt.Printf("[安全] 封条状态: %v\n", cfg.Encrypt)

	var sealPass []byte
	pass, _ := resolveSealKey(ws, false, cliKey)
	sealPass = pass

	tmpLandDir := filepath.Join(ws, "tmp", fmt.Sprintf("land_%d", time.Now().Unix()))
	_ = os.MkdirAll(tmpLandDir, 0755)
	defer os.RemoveAll(tmpLandDir)

	ociClient, err := provider.GetOCIClient(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 无法初始化 OCI 客户端: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n==> 正在通过原生 OCI 协议调取班轮清单: %s...\n", fullImage)
	mf, err := ociClient.GetManifest(ctx, tag)
	if err != nil || mf == nil || len(mf.Layers) == 0 {
		fmt.Fprintf(os.Stderr, "[-] 调取班轮清单失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ 成功获取清单，包含 %d 个分层，开始流式调取与提取...\n", len(mf.Layers))
	for idx, l := range mf.Layers {
		if oci.IsCamouflageDigest(l.Digest) {
			fmt.Printf("-> 识别到微服务伪装运行底座 [%d/%d] (指纹: %s...)，自动跳过还原\n", idx+1, len(mf.Layers), l.Digest[:19])
			continue
		}
		fmt.Printf("-> 正在流式提取货舱分层 [%d/%d] (指纹: %s...)\n", idx+1, len(mf.Layers), l.Digest[:19])
		if err := ociClient.DownloadBlobAndExtractCargo(ctx, l.Digest, tmpLandDir, nil); err != nil {
			fmt.Fprintf(os.Stderr, "[-] 提取货舱分层失败: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Println("✓ 原生 OCI 集装箱卸载提取成功 (零 Docker 守护进程依赖)！")

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
