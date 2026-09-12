package cmd

import (
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
	"ark/pkg/timezone"
)

func parseBoardFlags(cfg *config.Config, args []string) (tag, category, precision string, retryCount int) {
	category = cfg.Category
	precision = cfg.TagPrecision
	if precision == "" {
		precision = "second"
	}
	retryCount = cfg.PushRetry
	if retryCount <= 0 {
		retryCount = 3
	}

	if envRetry := os.Getenv("ARK_PUSH_RETRY"); envRetry != "" {
		if val, err := strconv.Atoi(envRetry); err == nil && val > 0 {
			retryCount = val
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
		tag = explicitTag
		if strings.Contains(tag, "-") {
			category = strings.SplitN(tag, "-", 2)[0]
		}
		return tag, category, "custom", retryCount
	}

	// 智能位置参数分析
	if len(positional) == 1 {
		p0 := positional[0]
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
				return tag, category, "custom", retryCount
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
			return tag, category, "custom", retryCount
		}
	}

	timeSuffix := timezone.GenerateTagTimeByPrecision(precision)
	tag = fmt.Sprintf("%s-%s", category, timeSuffix)
	return tag, category, precision, retryCount
}

func runBoard(args []string, dryRun bool) {
	cleanedArgs, cliKey := extractKeyFlag(args)
	args = cleanedArgs

	ws := getWorkspaceRoot()
	configPath := filepath.Join(ws, "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 加载配置失败: %v\n", err)
		os.Exit(1)
	}

	tag, category, precision, retryCount := parseBoardFlags(cfg, args)

	fmt.Println("================================================================")
	fmt.Println("          🚢 Ark 班轮装载登船系统 (Golang Engine)               ")
	fmt.Println("================================================================")
	fmt.Printf("[航次] 目的港位: %s\n", cfg.Repository)
	fmt.Printf("[场景] 所属分类: %s\n", category)
	fmt.Printf("[航次] 班次编号: %s (时间精度: %s)\n", tag, precision)
	fmt.Printf("[航次] 舱位配额: 该分类下保留最新 %d 个航次\n", cfg.RetentionCount)
	fmt.Printf("[容灾] 推送重试配额: 失败自动重试 %d 次 (指数退避)\n", retryCount)
	fmt.Printf("[安全] 货运封条: %v\n", cfg.Encrypt)

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
			UpdatedAt: timezone.FormatDefault(timezone.Now()),
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

	if err := docker.Build(dockerfilePath, ws, fullTag); err != nil {
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

	fmt.Printf("\n==> 班轮正在出港登船: %s...\n", fullTag)
	fmt.Println("【免复传机制】：封条未变动的集装箱将显示 'Layer already exists'，0 流量瞬间交付！")

	if err := docker.PushWithRetry(fullTag, retryCount, 3*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "[-] 航次推送失败 (已尝试 %d 次): %v\n", retryCount, err)
		os.Exit(1)
	}
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
