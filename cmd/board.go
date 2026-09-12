package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ark/pkg/archive"
	"ark/pkg/config"
	"ark/pkg/docker"
	"ark/pkg/github"
	"ark/pkg/hash"
	"ark/pkg/oci"
	"ark/pkg/timezone"
)

func parseBoardFlags(cfg *config.Config, args []string) (tag, category, precision string, retryCount int, shouldClean bool, cleanAll bool) {
	category = cfg.Category
	precision = cfg.TagPrecision
	if precision == "" {
		precision = "second"
	}
	retryCount = cfg.PushRetry
	if retryCount <= 0 {
		retryCount = 3
	}
	shouldClean = cfg.ShouldCleanAfterPush()
	cleanAll = cfg.ShouldCleanAllAfterPush()

	if envRetry := os.Getenv("ARK_PUSH_RETRY"); envRetry != "" {
		if val, err := strconv.Atoi(envRetry); err == nil && val > 0 {
			retryCount = val
		}
	}
	if envClean := os.Getenv("ARK_CLEAN_AFTER_PUSH"); envClean != "" {
		shouldClean = envClean == "1" || strings.ToLower(envClean) == "true"
	}
	if envCleanAll := os.Getenv("ARK_CLEAN_ALL_AFTER_PUSH"); envCleanAll != "" {
		cleanAll = envCleanAll == "1" || strings.ToLower(envCleanAll) == "true"
		if cleanAll {
			shouldClean = true
		}
	}

	var positional []string
	explicitTag := ""

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--day" || arg == "-d":
			precision = "day"
		case arg == "--second" || arg == "-s":
			precision = "second"
		case arg == "--minute" || arg == "-m":
			precision = "minute"
		case arg == "--hour":
			precision = "hour"
		case arg == "--clean-all" || arg == "--purge" || arg == "--clean-cache" || arg == "--reset":
			cleanAll = true
			shouldClean = true
		case arg == "--clean":
			shouldClean = true
		case arg == "--no-clean" || arg == "--keep-cache":
			shouldClean = false
			cleanAll = false
		case arg == "--tag":
			if i+1 < len(args) {
				explicitTag = args[i+1]
				i++
			}
		case strings.HasPrefix(arg, "--tag="):
			explicitTag = strings.TrimPrefix(arg, "--tag=")
		case arg == "--precision" || arg == "-p" || arg == "--time-format" || arg == "-t":
			if i+1 < len(args) {
				precision = args[i+1]
				i++
			}
		case strings.HasPrefix(arg, "--precision="):
			precision = strings.TrimPrefix(arg, "--precision=")
		case strings.HasPrefix(arg, "-p="):
			precision = strings.TrimPrefix(arg, "-p=")
		case strings.HasPrefix(arg, "--time-format="):
			precision = strings.TrimPrefix(arg, "--time-format=")
		case arg == "--retry" || arg == "-r":
			if i+1 < len(args) {
				if v, err := strconv.Atoi(args[i+1]); err == nil && v > 0 {
					retryCount = v
				}
				i++
			}
		case strings.HasPrefix(arg, "--retry="):
			if v, err := strconv.Atoi(strings.TrimPrefix(arg, "--retry=")); err == nil && v > 0 {
				retryCount = v
			}
		case strings.HasPrefix(arg, "-r="):
			if v, err := strconv.Atoi(strings.TrimPrefix(arg, "-r=")); err == nil && v > 0 {
				retryCount = v
			}
		default:
			positional = append(positional, arg)
		}
	}

	if explicitTag != "" {
		if strings.Contains(explicitTag, ":") {
			parts := strings.SplitN(explicitTag, ":", 2)
			cfg.Repository = parts[0]
			explicitTag = parts[1]
		}
		tag = explicitTag
		if strings.Contains(tag, "-") {
			category = strings.SplitN(tag, "-", 2)[0]
		}
		return tag, category, "custom", retryCount, shouldClean, cleanAll
	}

	// 智能位置参数分析
	if len(positional) == 1 {
		p0 := positional[0]
		if strings.Contains(p0, ":") {
			parts := strings.SplitN(p0, ":", 2)
			cfg.Repository = parts[0]
			p0 = parts[1]
		}
		switch strings.ToLower(p0) {
		case "day", "d", "date", "天", "日":
			precision = "day"
		case "second", "sec", "s", "秒":
			precision = "second"
		case "minute", "min", "m", "分":
			precision = "minute"
		case "hour", "h", "时":
			precision = "hour"
		default:
			if strings.Contains(p0, "-") {
				tag = p0
				category = strings.SplitN(p0, "-", 2)[0]
				return tag, category, "custom", retryCount, shouldClean, cleanAll
			}
			category = p0
		}
	} else if len(positional) >= 2 {
		category = positional[0]
		p1 := positional[1]
		switch strings.ToLower(p1) {
		case "day", "d", "date", "天", "日":
			precision = "day"
		case "second", "sec", "s", "秒":
			precision = "second"
		case "minute", "min", "m", "分":
			precision = "minute"
		case "hour", "h", "时":
			precision = "hour"
		default:
			tag = fmt.Sprintf("%s-%s", category, p1)
			return tag, category, "custom", retryCount, shouldClean, cleanAll
		}
	}

	timeSuffix := timezone.GenerateTagTimeByPrecision(precision)
	tag = fmt.Sprintf("%s-%s", category, timeSuffix)
	return tag, category, precision, retryCount, shouldClean, cleanAll
}

func runBoard(args []string, dryRun bool) {
	cleanedArgs, cliEngine := extractEngineFlag(args)
	cleanedArgs, cliKey := extractKeyFlag(cleanedArgs)
	cleanedArgs, cliRepo := extractRepoFlag(cleanedArgs)
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

	tag, category, precision, retryCount, shouldClean, cleanAll := parseBoardFlags(cfg, args)

	fmt.Println("================================================================")
	fmt.Println("          🚢 Ark 班轮装载登船系统 (Golang Engine)               ")
	fmt.Println("================================================================")
	fmt.Printf("[航次] 目的港位: %s\n", cfg.Repository)
	fmt.Printf("[场景] 所属分类: %s\n", category)
	fmt.Printf("[航次] 班次编号: %s (时间精度: %s)\n", tag, precision)
	engineDesc := "纯 Go 原生 OCI 极速直推 (Zero-Docker Pipeline, 0 额外落盘, 0 无效压缩)"
	if cliEngine == "docker" {
		engineDesc = "Docker BuildKit 构建流水线 (传统容器引擎)"
	}
	fmt.Printf("[引擎] 交付引擎: %s (%s)\n", strings.ToUpper(cliEngine), engineDesc)
	fmt.Printf("[航次] 舱位配额: 该分类下保留最新 %d 个航次\n", cfg.RetentionCount)
	fmt.Printf("[容灾] 推送重试配额: 失败自动重试 %d 次 (指数退避)\n", retryCount)
	fmt.Printf("[安全] 货运封条: %v\n", cfg.Encrypt)
	cleanModeStr := "释放本地构建临时数据 (保留增量缓存)"
	if cleanAll {
		cleanModeStr = "全量自动重置 (推送后彻底清空 cache/ 与临时文件，0 本地残留)"
	} else if !shouldClean {
		cleanModeStr = "保留本地缓存与临时文件"
	}
	fmt.Printf("[存储] 产物清理: %s\n", cleanModeStr)

	cacheDir := filepath.Join(ws, "cache")
	tmpDir := filepath.Join(ws, "tmp")
	_ = os.MkdirAll(cacheDir, 0755)
	_ = os.MkdirAll(tmpDir, 0755)

	if cliEngine == "docker" && shouldClean {
		fmt.Println("-> 正在执行构建前磁盘预清理 (自动释放历史中断或旧版本悬空层与构建缓存)...")
		_ = docker.PruneDanglingImages()
		_ = docker.PruneBuildCache()
	}

	var sealPass []byte
	if cfg.Encrypt {
		pass, err := resolveSealKey(ws, true, cliKey)
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
	cleanOrphanCacheFiles(cacheDir, sources)
	fmt.Println("\n------------------- 正在清点各货舱集装箱 (按变动频率排序) -------------------")

	layerFiles := make([]string, 0, len(sources))
	tarLayers := make([]*oci.TarLayer, 0, len(sources))
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

		layerFile := filepath.Join(cacheDir, src.ID+".tar.gz")
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
			fmt.Println("   [重新装箱 ⚡] 舱位货物有变动或首次装载，开始流式加封...")
			if cfg.Encrypt {
				fmt.Println("   正在施加安全密封 (内存流式 Tar -> Gzip -> AES-256 密闭处理，零中间磁盘文件)...")
				if err := archive.PackAndSealStream(srcPath, layerFile, sealPass); err != nil {
					fmt.Fprintf(os.Stderr, "[-] 安全流式打包加密失败: %v\n", err)
					os.Exit(1)
				}
			} else {
				fmt.Println("   正在流式打包压缩 (Tar -> Gzip)...")
				if err := archive.PackTarGz(srcPath, layerFile); err != nil {
					fmt.Fprintf(os.Stderr, "[-] 打包压缩失败: %v\n", err)
					os.Exit(1)
				}
			}

			fi, _ := os.Stat(layerFile)
			fmt.Printf("   装箱完毕: %s (%s, 压缩加封)\n", filepath.Base(layerFile), formatBytes(fi.Size()))
		}

		tl, err := oci.NewTarLayer(layerFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[-] 封装 OCI 单层 Tar 失败: %v\n", err)
			os.Exit(1)
		}
		layerDigest, err := tl.ComputeDigest()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[-] 计算 OCI 分层哈希失败: %v\n", err)
			os.Exit(1)
		}

		layerFiles = append(layerFiles, layerFile)
		tarLayers = append(tarLayers, tl)
		newManifest[src.ID] = ManifestEntry{
			Name:        src.Name,
			Path:        srcPath,
			TreeHash:    dirInfo.Hash,
			LayerFile:   filepath.Base(layerFile),
			LayerSHA256: layerDigest,
			UpdatedAt:   timezone.FormatDefault(timezone.Now()),
		}
	}

	if mData, err := json.MarshalIndent(newManifest, "", "  "); err == nil {
		_ = os.WriteFile(manifestPath, mData, 0644)
	}

	// 生成双架构 (linux/amd64 + linux/arm64) 构型信息
	cfgBytesAMD, cfgDigestAMD, cfgSizeAMD, err := oci.GenerateArchConfigJSON("amd64", tarLayers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 生成 AMD64 Config 失败: %v\n", err)
		os.Exit(1)
	}
	mfBytesAMD, mfDigestAMD, mfSizeAMD, err := oci.GenerateManifestJSON(cfgDigestAMD, cfgSizeAMD, tarLayers, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 生成 AMD64 Manifest 失败: %v\n", err)
		os.Exit(1)
	}

	cfgBytesARM, cfgDigestARM, cfgSizeARM, err := oci.GenerateArchConfigJSON("arm64", tarLayers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 生成 ARM64 Config 失败: %v\n", err)
		os.Exit(1)
	}
	mfBytesARM, mfDigestARM, mfSizeARM, err := oci.GenerateManifestJSON(cfgDigestARM, cfgSizeARM, tarLayers, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 生成 ARM64 Manifest 失败: %v\n", err)
		os.Exit(1)
	}

	manifestDescriptors := []oci.ManifestDescriptor{
		{
			MediaType: oci.MediaTypeDockerManifestV2,
			Size:      mfSizeAMD,
			Digest:    mfDigestAMD,
			Platform: oci.Platform{
				Architecture: "amd64",
				OS:           "linux",
			},
		},
		{
			MediaType: oci.MediaTypeDockerManifestV2,
			Size:      mfSizeARM,
			Digest:    mfDigestARM,
			Platform: oci.Platform{
				Architecture: "arm64",
				OS:           "linux",
			},
		},
	}
	indexBytes, indexDigest, indexSize, err := oci.GenerateMultiArchIndex(manifestDescriptors, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 生成多架构索引失败: %v\n", err)
		os.Exit(1)
	}

	if cliEngine == "oci" {
		fmt.Println("\n------------------- 装载构型 (Zero-Docker Multi-Arch OCI Index) -------------------")
		fmt.Printf("平台原生支持:  linux/amd64 + linux/arm64 (双架构免编译直通，0 架构警告)\n")
		fmt.Printf("货舱分层:      %d 个独立集装箱 Layer (双架构共享总载重: %s)\n", len(tarLayers), func() string {
			var tot int64
			for _, l := range tarLayers {
				tot += l.TotalSize
			}
			return formatBytes(tot)
		}())
		for idx, tl := range tarLayers {
			fmt.Printf("  [%02d] %s (%s, 路径: /%s)\n", idx+1, tl.Digest[:19]+"...", formatBytes(tl.TotalSize), tl.TargetCargo)
		}
		fmt.Printf("AMD64 清单:    %s (%d 字节)\n", mfDigestAMD, mfSizeAMD)
		fmt.Printf("ARM64 清单:    %s (%d 字节)\n", mfDigestARM, mfSizeARM)
		fmt.Printf("多架构索引:    %s (%d 字节)\n", indexDigest, indexSize)
		fmt.Println("----------------------------------------------------------------------------------")
	} else {
		// 在 cacheDir 中维护专属 .dockerignore，避免 Docker daemon 扫描传输无关上下文
		dockerignorePath := filepath.Join(cacheDir, ".dockerignore")
		_ = os.WriteFile(dockerignorePath, []byte("*\n!*.dat\n!*.tar.gz\n!*.tar\n!*.enc\n"), 0644)

		dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
		if err := docker.GenerateDockerfile(dockerfilePath, layerFiles); err != nil {
			fmt.Fprintf(os.Stderr, "[-] 生成 Dockerfile 失败: %v\n", err)
			os.Exit(1)
		}
		defer os.Remove(dockerfilePath)

		dfContent, _ := os.ReadFile(dockerfilePath)
		fmt.Println("\n------------------- 装载构型 (Dockerfile) -------------------")
		fmt.Println(string(dfContent))
		fmt.Println("-------------------------------------------------------------")
	}

	if dryRun {
		fmt.Println("[DRY RUN] 模拟登船完毕，跳过实际航行与推送。")
		return
	}

	token := loadToken(ws)
	parts := strings.Split(cfg.Repository, "/")
	regHost := "ghcr.io"
	regUser := "duorameng"
	if len(parts) >= 2 {
		regHost = parts[0]
		regUser = parts[1]
	}

	fullTag := fmt.Sprintf("%s:%s", cfg.Repository, tag)

	if cliEngine == "oci" {
		if token == "" {
			fmt.Fprintf(os.Stderr, "[-] 未检测到港口通行凭据。如需启航直推，请在 .env 中配置 GH_TOKEN 或运行 gh auth login。\n")
			os.Exit(1)
		}

		fmt.Printf("\n==> 正在连接港口 %s (用户: %s)...\n", regHost, regUser)
		ociClient, err := oci.NewClient(cfg.Repository, regUser, token)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[-] 初始化 OCI 客户端失败: %v\n", err)
			os.Exit(1)
		}

		ctx := context.Background()
		fmt.Println("==> 启用 Zero-Docker Pipeline 极速直推: 内存流式单通道直推，本地额外磁盘 0 字节，0 无效 CPU 压缩！")

		for idx, tl := range tarLayers {
			fmt.Printf("-> 正在探测货舱分层 [%d/%d]: %s (%s)...\n", idx+1, len(tarLayers), tl.FileName, formatBytes(tl.TotalSize))
			exists, err := ociClient.CheckBlobExists(ctx, tl.Digest)
			if err == nil && exists {
				fmt.Printf("   [远端已就绪 ✓] 0 流量秒传 (指纹: %s...)\n", tl.Digest[:19])
				continue
			}

			fmt.Printf("   [正在直推 ⚡] 建立内存流式通道，直传远端注册表...\n")
			var pushErr error
			startTime := time.Now()
			for attempt := 1; attempt <= retryCount; attempt++ {
				stream, cleanup, sErr := tl.OpenStream()
				if sErr != nil {
					pushErr = sErr
					break
				}

				var lastReport time.Time
				pushErr = ociClient.UploadBlobStream(ctx, tl.Digest, tl.TotalSize, stream, func(written int64) {
					now := time.Now()
					if now.Sub(lastReport) >= 300*time.Millisecond || written == tl.TotalSize {
						lastReport = now
						elapsed := now.Sub(startTime).Seconds()
						speedMB := 0.0
						if elapsed > 0 {
							speedMB = float64(written) / 1024 / 1024 / elapsed
						}
						pct := float64(written) / float64(tl.TotalSize) * 100
						fmt.Printf("\r   -> 已直传: %s / %s (%.1f%%) - %.1f MB/s   ",
							formatBytes(written), formatBytes(tl.TotalSize), pct, speedMB)
					}
				})
				cleanup()

				if pushErr == nil {
					fmt.Printf("\n   ✓ 货舱 [%s] 直推完成 (耗时: %v)！\n", tl.FileName, time.Since(startTime).Round(time.Millisecond))
					break
				}

				if attempt < retryCount {
					fmt.Printf("\n   [!] 提示: 直推遇到网络波动 (%v)，正在进行第 %d/%d 次重试...\n", pushErr, attempt+1, retryCount)
					time.Sleep(2 * time.Second)
				}
			}

			if pushErr != nil {
				fmt.Fprintf(os.Stderr, "[-] 货舱 [%s] 直推失败: %v\n", tl.FileName, pushErr)
				os.Exit(1)
			}
		}

		fmt.Println("-> 正在提交双架构班轮构型 (AMD64 & ARM64 Config JSON)...")
		if err := ociClient.UploadBlobBytes(ctx, cfgDigestAMD, cfgBytesAMD); err != nil {
			fmt.Fprintf(os.Stderr, "[-] 提交 AMD64 Config 失败: %v\n", err)
			os.Exit(1)
		}
		if err := ociClient.UploadBlobBytes(ctx, cfgDigestARM, cfgBytesARM); err != nil {
			fmt.Fprintf(os.Stderr, "[-] 提交 ARM64 Config 失败: %v\n", err)
			os.Exit(1)
		}

		tagAMD := fmt.Sprintf("%s-amd64", tag)
		tagARM := fmt.Sprintf("%s-arm64", tag)
		fmt.Println("-> 正在提交并打标多架构平台清单 (AMD64 & ARM64)...")
		// 先提交子平台并赋予显式 tag (消除 GitHub Packages 网页端 untagged 悬空显示)
		if err := ociClient.PutManifest(ctx, mfDigestAMD, mfBytesAMD, oci.MediaTypeDockerManifestV2); err != nil {
			fmt.Fprintf(os.Stderr, "[-] 提交 AMD64 Manifest 失败: %v\n", err)
			os.Exit(1)
		}
		if err := ociClient.PutManifest(ctx, tagAMD, mfBytesAMD, oci.MediaTypeDockerManifestV2); err != nil {
			fmt.Fprintf(os.Stderr, "[-] 打标 AMD64 Manifest (%s) 失败: %v\n", tagAMD, err)
			os.Exit(1)
		}

		if err := ociClient.PutManifest(ctx, mfDigestARM, mfBytesARM, oci.MediaTypeDockerManifestV2); err != nil {
			fmt.Fprintf(os.Stderr, "[-] 提交 ARM64 Manifest 失败: %v\n", err)
			os.Exit(1)
		}
		if err := ociClient.PutManifest(ctx, tagARM, mfBytesARM, oci.MediaTypeDockerManifestV2); err != nil {
			fmt.Fprintf(os.Stderr, "[-] 打标 ARM64 Manifest (%s) 失败: %v\n", tagARM, err)
			os.Exit(1)
		}

		fmt.Printf("-> 正在绑定多架构班轮总览标签 (ManifestList PUT): %s...\n", fullTag)
		if err := ociClient.PutManifest(ctx, tag, indexBytes, oci.MediaTypeDockerManifestList); err != nil {
			fmt.Fprintf(os.Stderr, "[-] 提交多架构 ManifestList 失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ 航次交付登船成功 (双架构 linux/amd64 + linux/arm64 显式打标: %s, %s, %s)！\n", tag, tagAMD, tagARM)
	} else {
		// Docker CLI 备选引擎链路
		if token != "" {
			fmt.Printf("正在校验港口通行证 %s (用户: %s)...\n", regHost, regUser)
			_ = docker.Login(regHost, regUser, token)
		} else {
			fmt.Println("[!] 提示: 未检测到通行凭据。如需启航，请在 .env 中配置 GH_TOKEN。")
		}

		dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
		_ = docker.GenerateDockerfile(dockerfilePath, layerFiles)
		defer os.Remove(dockerfilePath)

		pushedDirectly := false
		fmt.Println("\n==> 正在使用 BuildKit 进行班轮集装箱独立分层快照构建与交付...")
		if token != "" {
			fmt.Println("==> 启用流式直推模式 (Direct Push): 免本地镜像落盘，分层直推远端...")
			if err := docker.BuildWithDirectPush(dockerfilePath, cacheDir, fullTag); err == nil {
				fmt.Println("✓ 班轮快照流式直推交付成功 (本地 0 磁盘镜像占用)！")
				pushedDirectly = true
			} else {
				fmt.Printf("[!] 提示: 流式直推降级为本地构建推送: %v\n", err)
			}
		}

		if !pushedDirectly {
			if err := docker.Build(dockerfilePath, cacheDir, fullTag); err != nil {
				fmt.Fprintf(os.Stderr, "[-] Docker 构建失败: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("✓ 班轮快照封装成功！")

			fmt.Printf("\n==> 班轮正在出港登船: %s...\n", fullTag)
			fmt.Println("【免复传机制】：封条未变动的集装箱将显示 'Layer already exists'，0 流量瞬间交付！")

			if err := docker.PushWithRetry(fullTag, retryCount, 3*time.Second); err != nil {
				fmt.Fprintf(os.Stderr, "[-] 航次推送失败 (已尝试 %d 次): %v\n", retryCount, err)
				os.Exit(1)
			}
			fmt.Println("✓ 航次交付登船成功！")
		}

		if shouldClean && !cleanAll {
			if !pushedDirectly {
				fmt.Printf("-> 正在移除本地快照镜像: %s...\n", fullTag)
				_ = docker.RemoveImage(fullTag)
			}
			fmt.Println("-> 正在深度清理 Docker 悬空镜像与 BuildKit 构建缓存...")
			_ = docker.PruneDanglingImages()
			_ = docker.PruneBuildCache()
		}
	}

	fmt.Printf("\n------------------- 正在维护 [%s] 分类的历史航次配额 -------------------\n", category)
	if token != "" && cfg.RetentionCount > 0 {
		ghClient := github.NewClient(cfg.Repository, token)
		_ = ghClient.PruneCategoryVersions(category, cfg.RetentionCount)
	} else {
		fmt.Println("未提供通行凭据，跳过远端航次轮转维护。")
	}

	if cleanAll {
		fmt.Println("\n------------------- 正在执行全量环境重置 (--clean-all) -------------------")
		runClean([]string{"--docker"})
	} else if shouldClean {
		cleanOrphanCacheFiles(cacheDir, sources)
	}

	fmt.Println("\n================================================================")
	fmt.Println("          ⚓ 登船航次全流程圆满完成 (Ark Voyage Ready)            ")
	fmt.Println("================================================================")
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func cleanOrphanCacheFiles(cacheDir string, sources []config.Source) {
	validFiles := make(map[string]bool)
	validFiles["manifest.json"] = true
	validFiles[".dockerignore"] = true
	for _, s := range sources {
		validFiles[s.ID+".dat"] = true
		validFiles[s.ID+".tar.gz"] = true
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}

	cleanedCount := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !validFiles[e.Name()] {
			_ = os.Remove(filepath.Join(cacheDir, e.Name()))
			cleanedCount++
		}
	}
	if cleanedCount > 0 {
		fmt.Printf("   ✓ 已清理 %d 个陈旧废弃或未压缩的历史数据缓存\n", cleanedCount)
	}
}
