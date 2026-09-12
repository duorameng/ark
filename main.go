package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ark/pkg/archive"
	"ark/pkg/config"
	"ark/pkg/docker"
	"ark/pkg/github"
	"ark/pkg/hash"
	"ark/pkg/scanner"
)

type ManifestEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	TreeHash    string `json:"tree_hash"`
	LayerFile   string `json:"layer_file"`
	LayerSHA256 string `json:"layer_sha256"`
	UpdatedAt   string `json:"updated_at"`
}

func main() {
	if len(os.Args) < 2 {
		printHelp()
		return
	}

	action := os.Args[1]
	args := os.Args[2:]

	switch action {
	case "board", "登船":
		runBoard(args, false)
	case "dry", "试航":
		runBoard(args, true)
	case "land", "下船":
		runLand(args)
	case "unpack", "解封", "解包":
		runUnpack(args)
	case "scan", "扫描":
		runScan(args)
	case "list", "查验":
		runList(args)
	case "keygen", "密钥":
		runKeygen(args)
	case "help", "--help", "-h", "帮助":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "[-] 未知指令: %s\n\n", action)
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Println("🚢 Ark 班轮货运管理终端 (Vessel Logistics CLI - Golang Engine)")
	fmt.Println()
	fmt.Println("用法 (Usage):")
	fmt.Println("  ark [指令] [参数...]")
	fmt.Println()
	fmt.Println("可用指令 (Commands):")
	fmt.Println("  board,  登船    打包舱位货物，打上安全封条并推送到云端班轮")
	fmt.Println("  land,   下船    从云端班轮吊装卸载集装箱，解密解包归位货物")
	fmt.Println("  unpack, 解封    无需 Docker，直接从本地快照包 (.dat/.tar) 或 cache 目录解封还原货物")
	fmt.Println("  scan,   扫描    全自动扫描父目录，按变动率(冷热度)智能排序生成 sources 清单")
	fmt.Println("  list,   查验    按分类检索远端港口的所有航次记录与日期")
	fmt.Println("  keygen, 密钥    设置或生成专属安全封条密钥 (支持自定义密码或自动生成)")
	fmt.Println("  dry,    试航    模拟清点货舱与快照封装，不进行实际远程推送 (DRY RUN)")
	fmt.Println("  help,   帮助    查看本帮助信息")
	fmt.Println()
	fmt.Println("示例 (Examples):")
	fmt.Println("  ./ark keygen 我的自定义密码     # 自定义设置安全封条密码")
	fmt.Println("  ./ark scan /root/workspace      # 自动扫描并按冷热度排序更新配置")
	fmt.Println("  ./ark board                     # 使用默认分类执行登船 (生成 category-日期)")
	fmt.Println("  ./ark 登船 db                   # 指定分类为 db 执行登船")
	fmt.Println("  ./ark list                      # 查验港口所有分类的航次")
	fmt.Println("  ./ark land vps                  # 靠岸卸载 vps 分类的最新航次 (vps-latest)")
	fmt.Println("  ./ark 下船 vps-20260912-140000  # 靠岸还原指定日期的历史航次")
	fmt.Println("  ./ark unpack cache/postgres.dat ./pg_restore  # 直接解密封并恢复 postgres 数据")
}

func getWorkspaceRoot() string {
	exePath, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exePath)
		// 如果在 scripts/ 目录下运行
		if filepath.Base(dir) == "scripts" {
			return filepath.Dir(dir)
		}
		// 如果同级有 config.json
		if _, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
			return dir
		}
	}
	cwd, _ := os.Getwd()
	return cwd
}

func loadToken(workspaceRoot string) string {
	if token := os.Getenv("GH_TOKEN"); token != "" {
		return strings.TrimSpace(token)
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return strings.TrimSpace(token)
	}

	envPath := filepath.Join(workspaceRoot, ".env")
	if data, err := os.ReadFile(envPath); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "GH_TOKEN=") || strings.HasPrefix(line, "GITHUB_TOKEN=") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					val := strings.Trim(parts[1], `"' `)
					if val != "" {
						return val
					}
				}
			}
		}
	}

	// 尝试 gh auth token
	cmd := exec.Command("gh", "auth", "token")
	if out, err := cmd.Output(); err == nil {
		tok := strings.TrimSpace(string(out))
		if tok != "" {
			return tok
		}
	}

	return ""
}

func resolveSealKey(ws string, allowGenerate bool) ([]byte, error) {
	// 1. 优先读取环境变量
	if k := os.Getenv("ARK_KEY"); k != "" {
		return []byte(strings.TrimSpace(k)), nil
	}
	if k := os.Getenv("SEAL_KEY"); k != "" {
		return []byte(strings.TrimSpace(k)), nil
	}

	// 2. 从 .env 文件中读取 ARK_KEY= 或 SEAL_KEY=
	envPath := filepath.Join(ws, ".env")
	if envData, err := os.ReadFile(envPath); err == nil {
		for _, line := range strings.Split(string(envData), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "ARK_KEY=") || strings.HasPrefix(line, "SEAL_KEY=") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					val := strings.Trim(parts[1], `"' `)
					if val != "" {
						return []byte(val), nil
					}
				}
			}
		}
	}

	// 3. 从 keys/seal.key 文件中读取自定义或历史密钥
	keysDir := filepath.Join(ws, "keys")
	keyPath := filepath.Join(keysDir, "seal.key")
	if data, err := os.ReadFile(keyPath); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		return []byte(strings.TrimSpace(string(data))), nil
	}

	if !allowGenerate {
		// 解密场景下未找到密钥：提示用户在终端交互式输入自定义密码
		fmt.Print("[安全] 未检测到密钥文件，请输入安全封条密码 (Seal Key): ")
		var input string
		fmt.Scanln(&input)
		input = strings.TrimSpace(input)
		if input != "" {
			return []byte(input), nil
		}
		return nil, fmt.Errorf("未提供有效封条密钥，无法开启密闭集装箱")
	}

	// 4. 加密且无任何提供时：自动生成 32 字节高强度随机密钥并保存
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	keyStr := base64.StdEncoding.EncodeToString(buf)
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, []byte(keyStr), 0600); err != nil {
		return nil, err
	}
	fmt.Printf("[安全] 首次装载，已自动生成专属安全封条密钥: %s\n", keyPath)
	return []byte(keyStr), nil
}

// -----------------------------------------------------------------------------
// 子命令执行逻辑
// -----------------------------------------------------------------------------

func runScan(args []string) {
	ws := getWorkspaceRoot()
	configPath := filepath.Join(ws, "config.json")
	scanRoot := "/root/workspace"
	if len(args) > 0 {
		scanRoot = args[0]
	}

	fmt.Println("================================================================")
	fmt.Println("        🔍 Ark 货舱全自动扫描探测与变动率排序工具 (Golang)      ")
	fmt.Println("================================================================")
	fmt.Printf("[扫描目标] 总目录: %s\n", scanRoot)

	results, err := scanner.ScanRoot(scanRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 扫描失败: %v\n", err)
		os.Exit(1)
	}

	scanner.PrintScanSummary(results)

	cfg, _ := config.Load(configPath)
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	cfg.Sources = make([]config.Source, 0, len(results))
	for _, r := range results {
		cfg.Sources = append(cfg.Sources, r.Source)
	}

	if err := cfg.Save(configPath); err != nil {
		fmt.Fprintf(os.Stderr, "[-] 保存配置失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✓ 已成功自动更新清单配置文件: %s\n", configPath)
}

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

	// 保存最新 Manifest
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

	// 执行 Docker Build
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

	// 轮转清理
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

func runLand(args []string) {
	ws := getWorkspaceRoot()
	configPath := filepath.Join(ws, "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 加载配置失败: %v\n", err)
		os.Exit(1)
	}

	category := cfg.Category
	var tag string
	destDir := ""

	if len(args) > 0 {
		param := args[0]
		if strings.Contains(param, "-") {
			tag = param
			category = strings.SplitN(param, "-", 2)[0]
		} else {
			category = param
			tag = fmt.Sprintf("%s-latest", category)
		}
	} else {
		tag = fmt.Sprintf("%s-latest", category)
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
		pass, err := resolveSealKey(ws, false)
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

		fmt.Printf("-> 正在还原舱位货物: %s 到 %s...\n", modName, targetSubDir)
		if err := archive.UnpackTar(tarPath, targetSubDir); err != nil {
			fmt.Fprintf(os.Stderr, "[-] 还原解包失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("   ✓ 舱位 [%s] 货物已完整归位！\n", modName)
	}

	fmt.Println("\n================================================================")
	fmt.Printf("          🎉 下船清关完毕，所有货物已交付至: %s\n", destDir)
	fmt.Println("================================================================")
}

func runList(args []string) {
	ws := getWorkspaceRoot()
	configPath := filepath.Join(ws, "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 加载配置失败: %v\n", err)
		os.Exit(1)
	}

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

func runUnpack(args []string) {
	ws := getWorkspaceRoot()
	targetPath := "cache"
	destDir := ""

	if len(args) > 0 {
		targetPath = args[0]
	}
	if len(args) > 1 {
		destDir = args[1]
	}

	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(ws, targetPath)
	}

	fmt.Println("================================================================")
	fmt.Println("          📦 Ark 本地货物独立解封系统 (Zero-Docker Engine)      ")
	fmt.Println("================================================================")

	var sealPass []byte
	if pass, err := resolveSealKey(ws, false); err == nil {
		sealPass = pass
	}

	stat, err := os.Stat(targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 找不到目标货物文件或目录: %s\n", targetPath)
		os.Exit(1)
	}

	tmpDir := filepath.Join(ws, "tmp", fmt.Sprintf("unpack_%d", time.Now().Unix()))
	_ = os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	if !stat.IsDir() {
		// 单文件解封
		fname := filepath.Base(targetPath)
		modName := strings.TrimSuffix(fname, filepath.Ext(fname))
		modName = strings.TrimSuffix(modName, ".tar")
		if destDir == "" {
			destDir = filepath.Join(ws, fmt.Sprintf("cargo_restored_%s", modName))
		}

		fmt.Printf("[目标] 单集装箱: %s\n", targetPath)
		fmt.Printf("[交付] 恢复目录: %s\n", destDir)

		tarPath := targetPath
		if strings.HasSuffix(fname, ".dat") || strings.HasSuffix(fname, ".enc") {
			if len(sealPass) == 0 {
				fmt.Fprintln(os.Stderr, "[-] 未提供有效安全密钥，无法开启密闭集装箱！")
				os.Exit(1)
			}
			decryptedTar := filepath.Join(tmpDir, modName+".tar")
			fmt.Printf("-> 正在开启安全封条 (%s)...\n", fname)
			if err := archive.UnsealFile(targetPath, decryptedTar, sealPass); err != nil {
				fmt.Fprintf(os.Stderr, "[-] 解封失败: %v\n", err)
				os.Exit(1)
			}
			tarPath = decryptedTar
		}

		fmt.Printf("-> 正在还原舱位货物到 %s (保留 UID/GID 数字所有者与权限)...\n", destDir)
		if err := archive.UnpackTar(tarPath, destDir); err != nil {
			fmt.Fprintf(os.Stderr, "[-] 还原解包失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ 舱位 [%s] 货物已完整归位！\n", modName)
	} else {
		// 整个 cache 目录解封
		if destDir == "" {
			destDir = filepath.Join(ws, "cargo_restored_all")
		}
		fmt.Printf("[目标] 集装箱仓库: %s\n", targetPath)
		fmt.Printf("[交付] 批量恢复目录: %s\n", destDir)

		folderNameMap := make(map[string]string)
		if cfg, err := config.Load(filepath.Join(ws, "config.json")); err == nil {
			for _, src := range cfg.Sources {
				folderNameMap[src.ID] = filepath.Base(src.Path)
			}
		}

		entries, _ := os.ReadDir(targetPath)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			fname := e.Name()
			if !strings.HasSuffix(fname, ".dat") && !strings.HasSuffix(fname, ".tar") {
				continue
			}

			modName := strings.TrimSuffix(fname, filepath.Ext(fname))
			modName = strings.TrimSuffix(modName, ".tar")
			folderName := modName
			if realName, ok := folderNameMap[modName]; ok && realName != "" && realName != "." && realName != "/" {
				folderName = realName
			}
			targetSubDir := filepath.Join(destDir, folderName)
			fullFile := filepath.Join(targetPath, fname)

			tarPath := fullFile
			if strings.HasSuffix(fname, ".dat") || strings.HasSuffix(fname, ".enc") {
				if len(sealPass) == 0 {
					fmt.Printf("[!] 跳过密闭集装箱 %s (缺少安全密钥)\n", fname)
					continue
				}
				decryptedTar := filepath.Join(tmpDir, modName+".tar")
				fmt.Printf("-> 正在开启安全封条 (%s)...\n", fname)
				if err := archive.UnsealFile(fullFile, decryptedTar, sealPass); err != nil {
					fmt.Printf("[-] 解封 %s 失败: %v\n", fname, err)
					continue
				}
				tarPath = decryptedTar
			}

			fmt.Printf("-> 正在还原舱位货物 [%s] 到 %s...\n", modName, targetSubDir)
			if err := archive.UnpackTar(tarPath, targetSubDir); err != nil {
				fmt.Printf("[-] 还原 %s 失败: %v\n", modName, err)
				continue
			}
			fmt.Printf("   ✓ 舱位 [%s] 货物已完整归位！\n", modName)
		}
	}

	fmt.Println("\n================================================================")
	fmt.Printf("          🎉 解封还原圆满完成，全部货物已归位: %s\n", destDir)
	fmt.Println("================================================================")
}

func runKeygen(args []string) {
	ws := getWorkspaceRoot()
	keysDir := filepath.Join(ws, "keys")
	keyPath := filepath.Join(keysDir, "seal.key")
	_ = os.MkdirAll(keysDir, 0700)

	var keyStr string
	if len(args) > 0 {
		keyStr = strings.TrimSpace(args[0])
		fmt.Printf("[安全] 正在将用户自定义口令保存为安全封条密钥: %s\n", keyPath)
	} else {
		buf := make([]byte, 32)
		_, _ = rand.Read(buf)
		keyStr = base64.StdEncoding.EncodeToString(buf)
		fmt.Printf("[安全] 已生成全新 32 字节高强度安全封条密钥: %s\n", keyPath)
	}

	if err := os.WriteFile(keyPath, []byte(keyStr), 0600); err != nil {
		fmt.Fprintf(os.Stderr, "[-] 保存密钥失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ 密钥配置成功！当前封条密钥: %s\n", keyStr)
	fmt.Println("【重要提醒】：若换机恢复，请将此密码配置在目标机（写入 keys/seal.key 或通过终端输入）。")
}

