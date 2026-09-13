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
	_ = cliEngine

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
	fmt.Println("          🚢 Ark 班轮装载登船 (Zero-Docker OCI Engine)          ")
	fmt.Println("================================================================")
	if len(targets) == 1 {
		fmt.Printf("[港位] 目标仓库: %s (%s)\n", targets[0].Repository, targets[0].DisplayName)
	} else {
		fmt.Printf("[港位] 目标仓库: 开启异地多活交付 (共 %d 个云端港口)\n", len(targets))
		for idx, t := range targets {
			fmt.Printf("       -> 港口 %d: %s (%s)\n", idx+1, t.Repository, t.DisplayName)
		}
	}
	modeStr := fmt.Sprintf("分类: %s", category)
	if precision == "fixed" {
		modeStr += " | 固定 Tag 覆盖"
	} else if precision != "custom" {
		modeStr += fmt.Sprintf(" | 精度: %s", precision)
	}
	encStr := "未启用"
	if cfg.Encrypt {
		encStr = "AES-256 (安全封条)"
	}
	fmt.Printf("[航次] 班次标签: %s (%s) | 安全加密: %s\n", tag, modeStr, encStr)

	cacheDir := filepath.Join(ws, "cache")
	tmpDir := filepath.Join(ws, "tmp")
	_ = os.MkdirAll(cacheDir, 0755)
	_ = os.MkdirAll(tmpDir, 0755)

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
	fmt.Println("\n📦 正在清点货舱集装箱:")

	layerFiles := make([]string, 0, len(sources))
	tarLayers := make([]*oci.TarLayer, 0, len(sources))
	newManifest := make(map[string]ManifestEntry)

	for _, src := range sources {
		srcPath := src.Path
		if !filepath.IsAbs(srcPath) {
			srcPath = filepath.Join(ws, srcPath)
		}

		if _, err := os.Stat(srcPath); err != nil {
			fmt.Printf("  [!] 警告: 货源路径不存在，跳过: %s (%s)\n", srcPath, src.Name)
			continue
		}

		dirInfo, err := hash.ComputeSourceTreeHash(srcPath, src.IsRootFiles())
		if err != nil {
			fmt.Printf("  [-] 扫描目录哈希失败: %v\n", err)
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

		displayName := src.Name
		if isCached {
			fi, _ := os.Stat(layerFile)
			fmt.Printf("  ✓ [%s] 复用已有集装箱 (%s, 指纹: %s...)\n", displayName, formatBytes(fi.Size()), dirInfo.Hash[:10])
		} else {
			fmt.Printf("  ⚡ [%s] 货物有变动，开始流式打包加封...\n", displayName)
			if cfg.Encrypt {
				if err := archive.PackAndSealSourceStream(srcPath, layerFile, sealPass, src.IsRootFiles()); err != nil {
					fmt.Fprintf(os.Stderr, "[-] 安全流式打包加密失败: %v\n", err)
					os.Exit(1)
				}
			} else {
				if err := archive.PackSourceTarGz(srcPath, layerFile, src.IsRootFiles()); err != nil {
					fmt.Fprintf(os.Stderr, "[-] 打包压缩失败: %v\n", err)
					os.Exit(1)
				}
			}

			fi, _ := os.Stat(layerFile)
			fmt.Printf("    └─ 装箱完毕: %s (%s)\n", filepath.Base(layerFile), formatBytes(fi.Size()))
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
	indexBytes, _, _, err := oci.GenerateMultiArchIndex(manifestDescriptors, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 生成多架构索引失败: %v\n", err)
		os.Exit(1)
	}

	totCargoSize := int64(0)
	for _, l := range tarLayers {
		totCargoSize += l.TotalSize
	}
	fmt.Printf("  • 装载构型就绪: 共 %d 个分层 (总载重: %s | 支持 linux/amd64 + linux/arm64 双架构)\n", len(tarLayers), formatBytes(totCargoSize))

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
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[-] 交付至港口 [%s] 失败: %v\n", target.DisplayName, err)
			os.Exit(1)
		}

		// 统一面向 Provider 接口调用配额与生命周期维护
		provider := target.Provider()
		if err := provider.MaintainQuota(ctx, category, cfg.RetentionCount, tag, precision == "fixed"); err != nil {
			fmt.Printf("  [-] 配额维护提示: %v\n", err)
		}
	}

	if cleanAll {
		fmt.Println("\n🧹 正在执行全量环境重置 (--clean-all):")
		cleaned := cleanLocalWorkspaceDirs(ws)
		fmt.Printf("  ✓ 已彻底清空 cache/ 与 tmp/ 临时构建缓存 (清理 %d 项，0 字节本地残留)\n", cleaned)
	} else if shouldClean {
		cleanOrphanCacheFiles(cacheDir, sources)
	}

	fmt.Println("\n================================================================")
	if len(targets) > 1 {
		fmt.Printf("          ⚓ 异地多活交付全部圆满完成 (已同步交付 %d 个云端港口)            \n", len(targets))
	} else {
		fmt.Println("          ⚓ 航次交付登船全流程圆满完成 (Ark Voyage Ready)            ")
	}
	fmt.Println("================================================================")
}

// cleanLocalWorkspaceDirs 清空本地 cache/ 与 tmp/ 构建缓存
func cleanLocalWorkspaceDirs(ws string) int {
	cleaned := 0
	for _, dirName := range []string{"cache", "tmp"} {
		targetDir := filepath.Join(ws, dirName)
		if entries, err := os.ReadDir(targetDir); err == nil {
			for _, e := range entries {
				_ = os.RemoveAll(filepath.Join(targetDir, e.Name()))
				cleaned++
			}
		}
		_ = os.MkdirAll(targetDir, 0755)
	}
	return cleaned
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
) error {
	fullTag := fmt.Sprintf("%s:%s", target.Repository, tag)
	provider := target.Provider()

	if provider.Password() == "" {
		return fmt.Errorf("未检测到通行凭据，请在 .env 中配置对应目标凭据 (如 GH_TOKEN 或 ALIYUN_PASSWORD)")
	}

	fmt.Printf("\n🚀 正在直推交付远端港位: %s (%s)\n", provider.DisplayName(), provider.Host())
	ociClient, err := provider.GetOCIClient(ctx)
	if err != nil {
		return fmt.Errorf("初始化 OCI 客户端失败: %w", err)
	}

	for idx, tl := range tarLayers {
		exists, err := ociClient.CheckBlobExists(ctx, tl.Digest)
		if err == nil && exists {
			fmt.Printf("  [%d/%d] %s (%s)... ✓ 远端已就绪 (秒传)\n", idx+1, len(tarLayers), tl.FileName, formatBytes(tl.TotalSize))
			continue
		}

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
					fmt.Printf("\r  [%d/%d] 正在直推 %s (%s)... %.1f%% (%.1f MB/s)   ",
						idx+1, len(tarLayers), tl.FileName, formatBytes(tl.TotalSize), pct, speedMB)
				}
			})
			cleanup()

			if pushErr == nil {
				fmt.Printf("\r  [%d/%d] 直推成功: %s (%s) ✓ (耗时: %v)                   \n",
					idx+1, len(tarLayers), tl.FileName, formatBytes(tl.TotalSize), time.Since(startTime).Round(time.Millisecond))
				break
			}

			if attempt < retryCount {
				fmt.Printf("\n  [!] 直推网络波动 (%v)，正在进行第 %d/%d 次重试...\n", pushErr, attempt+1, retryCount)
				time.Sleep(2 * time.Second)
			}
		}

		if pushErr != nil {
			return fmt.Errorf("货舱 [%s] 直推失败: %w", tl.FileName, pushErr)
		}
	}

	if err := ociClient.UploadBlobBytes(ctx, cfgDigestAMD, cfgBytesAMD); err != nil {
		return fmt.Errorf("提交 AMD64 Config 失败: %w", err)
	}
	if err := ociClient.UploadBlobBytes(ctx, cfgDigestARM, cfgBytesARM); err != nil {
		return fmt.Errorf("提交 ARM64 Config 失败: %w", err)
	}

	tagAMD := fmt.Sprintf("%s-amd64", tag)
	tagARM := fmt.Sprintf("%s-arm64", tag)
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

	if err := ociClient.PutManifest(ctx, tag, indexBytes, oci.MediaTypeDockerManifestList); err != nil {
		return fmt.Errorf("提交多架构 ManifestList 失败: %w", err)
	}
	fmt.Printf("  • 绑定多架构标签: %s (双架构显式打标: %s, %s) ✓\n", fullTag, tagAMD, tagARM)
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
