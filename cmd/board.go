package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ark/pkg/archive"
	"ark/pkg/config"
	"ark/pkg/docker"
	"ark/pkg/github"
	"ark/pkg/hash"
)

func runBoard(args []string, dryRun bool) {
	ws := getWorkspaceRoot()
	configPath := filepath.Join(ws, "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 加载配置失败: %v\n", err)
		os.Exit(1)
	}

	category := cfg.Category
	tagParam := ""
	if len(args) > 0 {
		tagParam = args[0]
	}

	var tag string
	if tagParam != "" {
		if strings.Contains(tagParam, "-") && len(strings.Split(tagParam, "-")) >= 3 {
			tag = tagParam
			category = strings.SplitN(tagParam, "-", 2)[0]
		} else {
			category = tagParam
			tag = fmt.Sprintf("%s-%s", category, time.Now().Format("20060102-150405"))
		}
	} else {
		tag = fmt.Sprintf("%s-%s", category, time.Now().Format("20060102-150405"))
	}

	categoryLatest := fmt.Sprintf("%s-latest", category)

	fmt.Println("================================================================")
	fmt.Println("          🚢 Ark 班轮装载登船系统 (Golang Engine)               ")
	fmt.Println("================================================================")
	fmt.Printf("[航次] 目的港位: %s\n", cfg.Repository)
	fmt.Printf("[场景] 所属分类: %s\n", category)
	fmt.Printf("[航次] 班次编号: %s 与 %s\n", tag, categoryLatest)
	fmt.Printf("[航次] 舱位配额: 该分类下保留最新 %d 个航次\n", cfg.RetentionCount)
	fmt.Printf("[安全] 货运封条: %v\n", cfg.Encrypt)

	cacheDir := filepath.Join(ws, "cache")
	tmpDir := filepath.Join(ws, "tmp")
	_ = os.MkdirAll(cacheDir, 0755)
	_ = os.MkdirAll(tmpDir, 0755)

	var sealPass []byte
	if cfg.Encrypt {
		pass, err := resolveSealKey(ws, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[-] 获取安全封条密钥失败: %v\n", err)
			os.Exit(1)
		}
		sealPass = pass
	}

	manifestPath := filepath.Join(cacheDir, "manifest.json")
	manifest := make(map[string]ManifestEntry)
	if mData, err := os.ReadFile(manifestPath); err == nil {
		_ = json.Unmarshal(mData, &manifest)
	}

	sources := cfg.SortedSources()
	fmt.Println("\n------------------- 正在清点各货舱集装箱 (按变动频率排序) -------------------")

	layerFiles := make([]string, 0, len(sources))
	newManifest := make(map[string]ManifestEntry)

	for _, src := range sources {
		srcPath := src.Path
		if !filepath.IsAbs(srcPath) {
			srcPath = filepath.Join(ws, srcPath)
		}

		if _, err := os.Stat(srcPath); err != nil {
			fmt.Printf("[!] 警告: 货源路径不存在，跳过: %s (%s)\n", srcPath, src.Name)
			continue
		}

		fmt.Printf("-> 正在清点舱位: [%s] %s (%s)\n", src.ID, src.Name, srcPath)
		dirInfo, err := hash.ComputeDirTreeHash(srcPath)
		if err != nil {
			fmt.Printf("[-] 扫描目录哈希失败: %v\n", err)
			continue
		}

		tarFile := filepath.Join(cacheDir, src.ID+".tar")
		layerFile := tarFile
		if cfg.Encrypt {
			layerFile = filepath.Join(cacheDir, src.ID+".dat")
		}

		isCached := false
		cached, exists := manifest[src.ID]
		if exists && cached.TreeHash == dirInfo.Hash {
			if _, err := os.Stat(layerFile); err == nil {
				isCached = true
			}
		}

		if isCached {
			fmt.Printf("   [封条完好 ✓] 舱位货物无变化，直接复用已有集装箱 (指纹: %s...)\n", dirInfo.Hash[:12])
		} else {
			fmt.Println("   [重新装箱 ⚡] 舱位货物有变动或首次装载，开始打包加封...")
			if err := archive.PackTar(srcPath, tarFile); err != nil {
				fmt.Fprintf(os.Stderr, "[-] 打包 tar 失败: %v\n", err)
				os.Exit(1)
			}

			if cfg.Encrypt {
				fmt.Println("   正在施加安全密封 (AES-256 密闭处理)...")
				if err := archive.SealFile(tarFile, layerFile, sealPass); err != nil {
					fmt.Fprintf(os.Stderr, "[-] 安全加密失败: %v\n", err)
					os.Exit(1)
				}
				_ = os.Remove(tarFile)
			}

			fi, _ := os.Stat(layerFile)
			sizeStr := fmt.Sprintf("%d KB", fi.Size()/1024)
			if fi.Size() > 1024*1024 {
				sizeStr = fmt.Sprintf("%.1f MB", float64(fi.Size())/1024/1024)
			}
			fmt.Printf("   装箱完毕: %s (%s)\n", filepath.Base(layerFile), sizeStr)
		}

		layerFiles = append(layerFiles, layerFile)
		newManifest[src.ID] = ManifestEntry{
			Name:      src.Name,
			Path:      srcPath,
			TreeHash:  dirInfo.Hash,
			LayerFile: filepath.Base(layerFile),
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		}
	}

	if mData, err := json.MarshalIndent(newManifest, "", "  "); err == nil {
		_ = os.WriteFile(manifestPath, mData, 0644)
	}

	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := docker.GenerateDockerfile(dockerfilePath, layerFiles); err != nil {
		fmt.Fprintf(os.Stderr, "[-] 生成 Dockerfile 失败: %v\n", err)
		os.Exit(1)
	}

	dfContent, _ := os.ReadFile(dockerfilePath)
	fmt.Println("\n------------------- 装载构型 (Dockerfile) -------------------")
	fmt.Println(string(dfContent))
	fmt.Println("-------------------------------------------------------------")

	if dryRun {
		fmt.Println("[DRY RUN] 模拟登船完毕，跳过实际航行与推送。")
		return
	}

	fmt.Println("\n==> 正在使用 BuildKit 进行班轮集装箱独立分层快照构建...")
	fullTag := fmt.Sprintf("%s:%s", cfg.Repository, tag)
	latestTag := fmt.Sprintf("%s:%s", cfg.Repository, categoryLatest)

	if err := docker.Build(dockerfilePath, ws, fullTag, latestTag); err != nil {
		fmt.Fprintf(os.Stderr, "[-] Docker 构建失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ 班轮快照封装成功！")

	token := loadToken(ws)
	parts := strings.Split(cfg.Repository, "/")
	regHost := "ghcr.io"
	regUser := "duorameng"
	if len(parts) >= 2 {
		regHost = parts[0]
		regUser = parts[1]
	}

	if token != "" {
		fmt.Printf("正在校验港口通行证 %s (用户: %s)...\n", regHost, regUser)
		_ = docker.Login(regHost, regUser, token)
	} else {
		fmt.Println("[!] 提示: 未检测到通行凭据。如需启航，请在 .env 中配置 GH_TOKEN。")
	}

	fmt.Printf("\n==> 班轮正在出港登船: %s 与 %s...\n", fullTag, latestTag)
	fmt.Println("【免复传机制】：封条未变动的集装箱将显示 'Layer already exists'，0 流量瞬间交付！")

	if err := docker.Push(fullTag); err != nil {
		fmt.Fprintf(os.Stderr, "[-] 推送 %s 失败: %v\n", fullTag, err)
		os.Exit(1)
	}
	_ = docker.Push(latestTag)
	fmt.Println("✓ 航次交付登船成功！")

	fmt.Printf("\n------------------- 正在维护 [%s] 分类的历史航次配额 -------------------\n", category)
	if token != "" && cfg.RetentionCount > 0 {
		ghClient := github.NewClient(cfg.Repository, token)
		_ = ghClient.PruneCategoryVersions(category, cfg.RetentionCount)
	} else {
		fmt.Println("未提供通行凭据，跳过远端航次轮转维护。")
	}

	fmt.Println("\n================================================================")
	fmt.Println("          ⚓ 登船航次全流程圆满完成 (Ark Voyage Ready)            ")
	fmt.Println("================================================================")
}
