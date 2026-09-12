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
	useLatest := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--latest" || arg == "-l" || arg == "--fixed":
			useLatest = true
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
		case arg == "--keep" || arg == "--retention":
			if i+1 < len(args) {
				if v, err := strconv.Atoi(args[i+1]); err == nil && v > 0 {
					cfg.RetentionCount = v
				}
				i++
			}
		case strings.HasPrefix(arg, "--keep="):
			if v, err := strconv.Atoi(strings.TrimPrefix(arg, "--keep=")); err == nil && v > 0 {
				cfg.RetentionCount = v
			}
		case strings.HasPrefix(arg, "--retention="):
			if v, err := strconv.Atoi(strings.TrimPrefix(arg, "--retention=")); err == nil && v > 0 {
				cfg.RetentionCount = v
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
		case "latest", "fixed", "固定":
			useLatest = true
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
		case "latest", "fixed", "固定":
			useLatest = true
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

	if useLatest {
		tag = "latest"
		return tag, category, "fixed", retryCount, shouldClean, cleanAll
	}

	if cfg.FixedTag != "" {
		tag = cfg.FixedTag
		return tag, category, "fixed", retryCount, shouldClean, cleanAll
	}

	timeSuffix := timezone.GenerateTagTimeByPrecision(precision)
	tag = fmt.Sprintf("%s-%s", category, timeSuffix)
	return tag, category, precision, retryCount, shouldClean, cleanAll
}

func runBoard(args []string, dryRun bool) {
	cleanedArgs, cliEngine := extractEngineFlag(args)
	cleanedArgs, cliKey := extractKeyFlag(cleanedArgs)
	cleanedArgs, cliTarget := extractTargetFlag(cleanedArgs)
	cleanedArgs, cliRepo := extractRepoFlag(cleanedArgs)
	args = cleanedArgs

	ws := getWorkspaceRoot()
	cfg, hasConfigFile, err := LoadAppConfig(ws, cliRepo)
	if !hasConfigFile || err != nil {
		fmt.Fprintf(os.Stderr, "[-] 登船失败，未能加载有效货运清单: %v\n", err)
		fmt.Fprintln(os.Stderr, "    提示: 请在工作区配置 config.json，或先运行 'ark scan <目录>' 自动生成清单。")
		os.Exit(1)
	}

	targets := resolveRegistryTargets(ws, cliTarget, cliRepo, cfg.Repository)
	if len(targets) == 0 {
		fmt.Fprintf(os.Stderr, "[-] 登船失败，未能识别到有效的推送目标港位\n")
		os.Exit(1)
	}

	tag, category, precision, retryCount, shouldClean, cleanAll := parseBoardFlags(cfg, args)

	fmt.Println("================================================================")
	fmt.Println("          🚢 Ark 班轮装载登船系统 (Golang Engine)               ")
	fmt.Println("================================================================")
	if len(targets) == 1 {
		fmt.Printf("[航次] 目的港位: %s (%s)\n", targets[0].Repository, targets[0].DisplayName)
	} else {
		fmt.Printf("[航次] 目的港位: 开启多云/双推异地多活模式 (同时交付 %d 个云端港口)\n", len(targets))
		for idx, t := range targets {
			fmt.Printf("       -> 港口 %d: %s (%s)\n", idx+1, t.Repository, t.DisplayName)
		}
	}
	fmt.Printf("[场景] 所属分类: %s\n", category)
	if precision == "fixed" {
		fmt.Printf("[航次] 班次编号: %s (模式: 固定 Tag 覆盖，自动覆写最新版本)\n", tag)
		fmt.Println("[航次] 舱位配额: 固定 Tag 覆盖模式 (远端自动维持单版本最新，免手动清理)")
	} else {
		fmt.Printf("[航次] 班次编号: %s (时间精度: %s)\n", tag, precision)
		fmt.Printf("[航次] 舱位配额: 该分类下保留最新 %d 个航次\n", cfg.RetentionCount)
	}
	engineDesc := "纯 Go 原生 OCI 极速直推 (Zero-Docker Pipeline, 0 额外落盘, 0 无效压缩)"
	if cliEngine == "docker" {
		engineDesc = "Docker BuildKit 构建流水线 (传统容器引擎)"
	}
	fmt.Printf("[引擎] 交付引擎: %s (%s)\n", strings.ToUpper(cliEngine), engineDesc)
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
		dirInfo, err := hash.ComputeSourceTreeHash(srcPath, src.IsRootFiles())
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
				if err := archive.PackAndSealSourceStream(srcPath, layerFile, sealPass, src.IsRootFiles()); err != nil {
					fmt.Fprintf(os.Stderr, "[-] 安全流式打包加密失败: %v\n", err)
					os.Exit(1)
				}
			} else {
				fmt.Println("   正在流式打包压缩 (Tar -> Gzip)...")
				if err := archive.PackSourceTarGz(srcPath, layerFile, src.IsRootFiles()); err != nil {
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

	ctx := context.Background()
	for tIdx, target := range targets {
		if len(targets) > 1 {
			fmt.Printf("\n==================== 港口 [%d/%d]: %s (%s) ====================\n",
				tIdx+1, len(targets), target.DisplayName, target.Repository)
		}

		err := pushSingleTarget(
			ctx, target, tag, tarLayers,
			cfgDigestAMD, cfgBytesAMD,
			cfgDigestARM, cfgBytesARM,
			mfDigestAMD, mfBytesAMD,
			mfDigestARM, mfBytesARM,
			indexBytes, retryCount,
			cliEngine, tmpDir, cacheDir, layerFiles,
			shouldClean, cleanAll,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[-] 交付至港口 [%s] 失败: %v\n", target.DisplayName, err)
			os.Exit(1)
		}

		if target.IsGHCR {
			fmt.Printf("\n------------------- 正在维护 [%s] 分类的历史航次配额 (%s) -------------------\n", category, target.DisplayName)
			if target.Password != "" {
				ghClient := github.NewClient(target.Repository, target.Password)
				if cfg.RetentionCount > 0 {
					_ = ghClient.PruneCategoryVersions(category, cfg.RetentionCount)
				}
				fmt.Println("-> 正在顺带扫描并清理远端未打标孤立版本 (Untagged Versions)...")
				deletedUntagged, errUntagged := ghClient.PruneUntaggedVersions()
				if errUntagged != nil {
					fmt.Printf("   [-] 清理远端未打标版本提示: %v\n", errUntagged)
				} else if deletedUntagged > 0 {
					fmt.Printf("   ✓ 顺带成功清理了 %d 个远端孤立 untagged 版本！\n", deletedUntagged)
				} else {
					fmt.Println("   ✓ 远端未发现任何孤立 untagged 版本，状态清洁。")
				}
			} else {
				fmt.Println("未提供通行凭据，跳过远端航次轮转与 untagged 清理维护。")
			}
		} else {
			parts := strings.Split(target.Repository, "/")
			regHost := target.Repository
			if len(parts) > 0 {
				regHost = parts[0]
			}
			fmt.Printf("\n------------------- 通用 OCI 注册表配额管理 (%s) -------------------\n", regHost)
			fmt.Printf("✓ 航次已成功推送至 %s (%s)\n", target.DisplayName, target.Repository)
			if precision == "fixed" {
				fmt.Printf("✓ 当前采用固定 Tag 覆盖模式 (%s)，已自动覆写上一航次，远端仓库始终保持最新单版本。\n", tag)
			} else {
				fmt.Println("💡 提示: 针对阿里云 ACR 个人版等无生命周期自动清理的环境，可使用固定 Tag 覆盖模式 (如 ark board --latest 或配置 ARK_TAG=latest) 实现自动覆写，免手动清理。")
			}
		}
	}

	if cleanAll {
		fmt.Println("\n------------------- 正在执行全量环境重置 (--clean-all) -------------------")
		runClean([]string{"--all"})
	} else if shouldClean {
		cleanOrphanCacheFiles(cacheDir, sources)
	}

	fmt.Println("\n================================================================")
	if len(targets) > 1 {
		fmt.Printf("          ⚓ 双推异地多活交付全部圆满完成 (已同步交付 %d 个云端港口)            \n", len(targets))
	} else {
		fmt.Println("          ⚓ 登船航次全流程圆满完成 (Ark Voyage Ready)            ")
	}
	fmt.Println("================================================================")
}

// pushSingleTarget 独立交付单个目标注册表
func pushSingleTarget(
	ctx context.Context,
	target RegistryTarget,
	tag string,
	tarLayers []*oci.TarLayer,
	cfgDigestAMD string, cfgBytesAMD []byte,
	cfgDigestARM string, cfgBytesARM []byte,
	mfDigestAMD string, mfBytesAMD []byte,
	mfDigestARM string, mfBytesARM []byte,
	indexBytes []byte,
	retryCount int,
	cliEngine string,
	tmpDir, cacheDir string,
	layerFiles []string,
	shouldClean, cleanAll bool,
) error {
	fullTag := fmt.Sprintf("%s:%s", target.Repository, tag)
	parts := strings.Split(target.Repository, "/")
	regHost := "ghcr.io"
	if len(parts) >= 1 && parts[0] != "" {
		regHost = parts[0]
	}

	if cliEngine == "oci" {
		if target.Password == "" {
			return fmt.Errorf("未检测到通行凭据，请在 .env 中配置对应目标凭据 (如 GH_TOKEN 或 ALIYUN_PASSWORD)")
		}

		fmt.Printf("\n==> 正在连接港口 %s (用户: %s, 目标: %s)...\n", regHost, target.Username, target.DisplayName)
		ociClient, err := oci.NewClient(target.Repository, target.Username, target.Password)
		if err != nil {
			return fmt.Errorf("初始化 OCI 客户端失败: %w", err)
		}

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
				return fmt.Errorf("货舱 [%s] 直推失败: %w", tl.FileName, pushErr)
			}
		}

		fmt.Println("-> 正在提交双架构班轮构型 (AMD64 & ARM64 Config JSON)...")
		if err := ociClient.UploadBlobBytes(ctx, cfgDigestAMD, cfgBytesAMD); err != nil {
			return fmt.Errorf("提交 AMD64 Config 失败: %w", err)
		}
		if err := ociClient.UploadBlobBytes(ctx, cfgDigestARM, cfgBytesARM); err != nil {
			return fmt.Errorf("提交 ARM64 Config 失败: %w", err)
		}

		tagAMD := fmt.Sprintf("%s-amd64", tag)
		tagARM := fmt.Sprintf("%s-arm64", tag)
		fmt.Println("-> 正在提交并打标多架构平台清单 (AMD64 & ARM64)...")
		if err := ociClient.PutManifest(ctx, mfDigestAMD, mfBytesAMD, oci.MediaTypeDockerManifestV2); err != nil {
			return fmt.Errorf("提交 AMD64 Manifest 失败: %w", err)
		}
		if err := ociClient.PutManifest(ctx, tagAMD, mfBytesAMD, oci.MediaTypeDockerManifestV2); err != nil {
			return fmt.Errorf("打标 AMD64 Manifest (%s) 失败: %w", tagAMD, err)
		}

		if err := ociClient.PutManifest(ctx, mfDigestARM, mfBytesARM, oci.MediaTypeDockerManifestV2); err != nil {
			return fmt.Errorf("提交 ARM64 Manifest 失败: %w", err)
		}
		if err := ociClient.PutManifest(ctx, tagARM, mfBytesARM, oci.MediaTypeDockerManifestV2); err != nil {
			return fmt.Errorf("打标 ARM64 Manifest (%s) 失败: %w", tagARM, err)
		}

		fmt.Printf("-> 正在绑定多架构班轮总览标签 (ManifestList PUT): %s...\n", fullTag)
		if err := ociClient.PutManifest(ctx, tag, indexBytes, oci.MediaTypeDockerManifestList); err != nil {
			return fmt.Errorf("提交多架构 ManifestList 失败: %w", err)
		}
		fmt.Printf("✓ 航次交付登船成功 (双架构 linux/amd64 + linux/arm64 显式打标: %s, %s, %s)！\n", tag, tagAMD, tagARM)
		return nil
	}

	// Docker CLI 备选引擎链路
	if target.Password != "" {
		fmt.Printf("正在校验港口通行证 %s (用户: %s)...\n", regHost, target.Username)
		_ = docker.Login(regHost, target.Username, target.Password)
	}

	dockerfilePath := filepath.Join(tmpDir, fmt.Sprintf("Dockerfile_%s", target.Key))
	_ = docker.GenerateDockerfile(dockerfilePath, layerFiles)
	defer os.Remove(dockerfilePath)

	pushedDirectly := false
	fmt.Printf("\n==> 正在使用 BuildKit 构建交付至: %s...\n", fullTag)
	if target.Password != "" {
		if err := docker.BuildWithDirectPush(dockerfilePath, cacheDir, fullTag); err == nil {
			fmt.Println("✓ 班轮快照流式直推交付成功！")
			pushedDirectly = true
		}
	}

	if !pushedDirectly {
		if err := docker.Build(dockerfilePath, cacheDir, fullTag); err != nil {
			return fmt.Errorf("Docker 构建失败: %w", err)
		}
		if err := docker.PushWithRetry(fullTag, retryCount, 3*time.Second); err != nil {
			return fmt.Errorf("航次推送失败: %w", err)
		}
		fmt.Println("✓ 航次交付登船成功！")
	}

	if shouldClean && !cleanAll && !pushedDirectly {
		_ = docker.RemoveImage(fullTag)
	}
	return nil
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
