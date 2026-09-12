package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"ark/pkg/config"
	"ark/pkg/scanner"
)

// getWorkspaceRoot 获取工作区根目录路径
func getWorkspaceRoot() string {
	exePath, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exePath)
		if filepath.Base(dir) == "scripts" || filepath.Base(dir) == config.TmpDirName {
			return filepath.Dir(dir)
		}
		if _, err := os.Stat(filepath.Join(dir, config.ConfigFileName)); err == nil {
			return dir
		}
	}
	cwd, _ := os.Getwd()
	return cwd
}

// loadEnvFile 从工作区加载 .env 文件，并将其中未定义的配置注入至进程环境变量
func loadEnvFile(workspaceRoot string) {
	envPath := filepath.Join(workspaceRoot, config.EnvFileName)
	data, err := os.ReadFile(envPath)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.Trim(strings.TrimSpace(parts[1]), `"' `)
			if os.Getenv(k) == "" && v != "" {
				_ = os.Setenv(k, v)
			}
		}
	}

	// 若配置了 ARK_PROXY 别名，自动同步注入至标准网络代理环境变量
	if p := strings.TrimSpace(os.Getenv(config.EnvArkProxy)); p != "" {
		if os.Getenv("HTTPS_PROXY") == "" {
			_ = os.Setenv("HTTPS_PROXY", p)
		}
		if os.Getenv("HTTP_PROXY") == "" {
			_ = os.Setenv("HTTP_PROXY", p)
		}
		if os.Getenv("ALL_PROXY") == "" {
			_ = os.Setenv("ALL_PROXY", p)
		}
	}
}

// loadToken 获取镜像仓库访问凭据 (按 ARK_TOKEN -> ARK_PASSWORD -> GH_TOKEN -> GITHUB_TOKEN -> DOCKER_PASSWORD -> gh CLI 顺序检索)
func loadToken(workspaceRoot string) string {
	loadEnvFile(workspaceRoot)

	if token := os.Getenv(config.EnvArkToken); token != "" {
		return strings.TrimSpace(token)
	}
	if token := os.Getenv(config.EnvArkPassword); token != "" {
		return strings.TrimSpace(token)
	}
	if token := os.Getenv(config.EnvGhToken); token != "" {
		return strings.TrimSpace(token)
	}
	if token := os.Getenv(config.EnvGithubToken); token != "" {
		return strings.TrimSpace(token)
	}
	if token := os.Getenv("DOCKER_PASSWORD"); token != "" {
		return strings.TrimSpace(token)
	}

	cmd := exec.Command("gh", "auth", "token")
	if out, err := cmd.Output(); err == nil {
		tok := strings.TrimSpace(string(out))
		if tok != "" {
			return tok
		}
	}

	return ""
}

// resolveRegistryUser 解析登录镜像注册表的用户名: ARK_USERNAME > DOCKER_USER > 仓库第二段路径 > oauth2
func resolveRegistryUser(repo string) string {
	if u := strings.TrimSpace(os.Getenv(config.EnvArkUsername)); u != "" {
		return u
	}
	if u := strings.TrimSpace(os.Getenv("DOCKER_USER")); u != "" {
		return u
	}
	if u := strings.TrimSpace(os.Getenv("DOCKER_USERNAME")); u != "" {
		return u
	}
	parts := strings.Split(repo, "/")
	if len(parts) >= 2 && parts[1] != "" {
		return parts[1]
	}
	return "oauth2"
}

// resolveRepository 综合解析镜像仓库名称: 命令行参数 > 环境变量 > 配置文件
func resolveRepository(cfgRepo, cliRepo string) string {
	if strings.TrimSpace(cliRepo) != "" {
		return strings.TrimSpace(cliRepo)
	}
	if envRepo := os.Getenv(config.EnvArkRepository); strings.TrimSpace(envRepo) != "" {
		return strings.TrimSpace(envRepo)
	}
	if envImage := os.Getenv(config.EnvArkImage); strings.TrimSpace(envImage) != "" {
		return strings.TrimSpace(envImage)
	}
	if strings.TrimSpace(cfgRepo) != "" {
		return strings.TrimSpace(cfgRepo)
	}
	return config.DefaultRepository
}

// RegistryTarget 镜像港口目标结构定义 (支持多云与别名快速选路)
type RegistryTarget struct {
	Key         string // "aliyun", "github", "custom", "default"
	DisplayName string // 友好展示名称
	Repository  string // 完整仓库地址 (如 registry.cn-hangzhou.aliyuncs.com/xxx/ark)
	Username    string // 登录用户名
	Password    string // 密码或访问令牌
	IsGHCR      bool   // 是否为 GitHub Packages (支持专属 REST API 轮转)
}

// isTargetKeyword 判断参数是否为目标别名关键字
func isTargetKeyword(arg string) bool {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "ali", "aliyun", "acr", "阿里", "阿里云",
		"gh", "github", "ghcr",
		"both", "all", "all-targets", "双推", "双向":
		return true
	default:
		return false
	}
}

// extractTargetFlag 从命令行参数中提取目标别名指示: --to, --target, --both, -T 或独立的别名关键词
func extractTargetFlag(args []string) ([]string, string) {
	var cleaned []string
	target := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--to" || arg == "--target" || arg == "-T":
			if i+1 < len(args) {
				target = args[i+1]
				i++
				continue
			}
		case strings.HasPrefix(arg, "--to="):
			target = strings.TrimPrefix(arg, "--to=")
			continue
		case strings.HasPrefix(arg, "--target="):
			target = strings.TrimPrefix(arg, "--target=")
			continue
		case strings.HasPrefix(arg, "-T="):
			target = strings.TrimPrefix(arg, "-T=")
			continue
		case arg == "--both" || arg == "--all-targets":
			target = "both"
			continue
		case isTargetKeyword(arg):
			target = arg
			continue
		default:
			cleaned = append(cleaned, arg)
		}
	}
	return cleaned, target
}

// resolveRegistryTargets 综合解析最终推送目标列表 (支持别名路由、默认回退与双推模式)
func resolveRegistryTargets(ws, cliTarget, cliRepo, defaultRepo string) []RegistryTarget {
	loadEnvFile(ws)

	// 1. 若命令行显式传入了具体 URL 仓库地址 (含 "/" 且非纯别名)，作为自定义单个目标
	if strings.Contains(cliRepo, "/") && !isTargetKeyword(cliRepo) {
		isGHCR := strings.HasPrefix(cliRepo, "ghcr.io") || strings.Contains(cliRepo, "github")
		name := "自定义 OCI 注册表"
		if isGHCR {
			name = "GitHub Packages (GHCR)"
		} else if strings.Contains(cliRepo, "aliyuncs.com") {
			name = "阿里云容器镜像服务 (ACR)"
		}
		return []RegistryTarget{{
			Key:         "custom",
			DisplayName: name,
			Repository:  cliRepo,
			Username:    resolveRegistryUser(cliRepo),
			Password:    loadToken(ws),
			IsGHCR:      isGHCR,
		}}
	}

	targetKey := strings.ToLower(strings.TrimSpace(cliTarget))
	if targetKey == "" && isTargetKeyword(cliRepo) {
		targetKey = strings.ToLower(strings.TrimSpace(cliRepo))
	}
	if targetKey == "" {
		targetKey = strings.ToLower(strings.TrimSpace(os.Getenv(config.EnvArkTarget)))
	}

	getAliyunTarget := func() (RegistryTarget, bool) {
		repo := strings.TrimSpace(os.Getenv(config.EnvAliyunRepository))
		if repo == "" {
			repo = strings.TrimSpace(os.Getenv("ARK_ALIYUN_REPOSITORY"))
		}
		if repo == "" && strings.Contains(defaultRepo, "aliyuncs.com") {
			repo = defaultRepo
		}
		if repo == "" {
			return RegistryTarget{}, false
		}
		user := strings.TrimSpace(os.Getenv(config.EnvAliyunUsername))
		if user == "" {
			user = strings.TrimSpace(os.Getenv("ARK_ALIYUN_USERNAME"))
		}
		if user == "" {
			user = resolveRegistryUser(repo)
		}
		pass := strings.TrimSpace(os.Getenv(config.EnvAliyunPassword))
		if pass == "" {
			pass = strings.TrimSpace(os.Getenv("ARK_ALIYUN_PASSWORD"))
		}
		if pass == "" {
			pass = loadToken(ws)
		}
		return RegistryTarget{
			Key:         "aliyun",
			DisplayName: "阿里云容器镜像服务 (ACR)",
			Repository:  repo,
			Username:    user,
			Password:    pass,
			IsGHCR:      false,
		}, true
	}

	getGithubTarget := func() (RegistryTarget, bool) {
		repo := strings.TrimSpace(os.Getenv(config.EnvGithubRepository))
		if repo == "" {
			repo = strings.TrimSpace(os.Getenv("ARK_GITHUB_REPOSITORY"))
		}
		if repo == "" && (strings.Contains(defaultRepo, "ghcr.io") || defaultRepo == config.DefaultRepository) {
			repo = defaultRepo
		}
		if repo == "" {
			repo = config.DefaultRepository
		}
		user := resolveRegistryUser(repo)
		pass := strings.TrimSpace(os.Getenv(config.EnvGhToken))
		if pass == "" {
			pass = strings.TrimSpace(os.Getenv(config.EnvGithubToken))
		}
		if pass == "" {
			pass = loadToken(ws)
		}
		return RegistryTarget{
			Key:         "github",
			DisplayName: "GitHub Packages (GHCR)",
			Repository:  repo,
			Username:    user,
			Password:    pass,
			IsGHCR:      true,
		}, true
	}

	switch targetKey {
	case "both", "all", "all-targets", "双推", "双向":
		var list []RegistryTarget
		if ali, ok := getAliyunTarget(); ok {
			list = append(list, ali)
		} else {
			fmt.Println("[!] 提示: 双推模式未检测到阿里云配置 (ALIYUN_REPOSITORY)，将跳过阿里云目标。")
		}
		if gh, ok := getGithubTarget(); ok {
			list = append(list, gh)
		} else {
			fmt.Println("[!] 提示: 双推模式未检测到 GitHub 配置 (GITHUB_REPOSITORY)，将跳过 GitHub 目标。")
		}
		if len(list) > 0 {
			return list
		}
		fmt.Fprintf(os.Stderr, "[-] 双推模式失败: 既未配置阿里云 (ALIYUN_REPOSITORY) 也未配置 GitHub (GITHUB_REPOSITORY)\n")
		return nil
	case "ali", "aliyun", "acr", "阿里", "阿里云":
		if ali, ok := getAliyunTarget(); ok {
			return []RegistryTarget{ali}
		}
		fmt.Fprintf(os.Stderr, "[-] 未识别到阿里云 ACR 仓库地址，请在 .env 中配置 ALIYUN_REPOSITORY (例如: registry.cn-hangzhou.aliyuncs.com/your-ns/ark)\n")
		return nil
	case "gh", "github", "ghcr":
		if gh, ok := getGithubTarget(); ok {
			return []RegistryTarget{gh}
		}
		fmt.Fprintf(os.Stderr, "[-] 未识别到 GitHub 仓库地址，请在 .env 中配置 GITHUB_REPOSITORY (例如: ghcr.io/your-user/ark)\n")
		return nil
	}

	// 兜底回退：使用 defaultRepo
	isGHCR := strings.HasPrefix(defaultRepo, "ghcr.io") || strings.Contains(defaultRepo, "github")
	name := "默认镜像注册表"
	if isGHCR {
		name = "GitHub Packages (GHCR)"
	} else if strings.Contains(defaultRepo, "aliyuncs.com") {
		name = "阿里云容器镜像服务 (ACR)"
	}
	return []RegistryTarget{{
		Key:         "default",
		DisplayName: name,
		Repository:  defaultRepo,
		Username:    resolveRegistryUser(defaultRepo),
		Password:    loadToken(ws),
		IsGHCR:      isGHCR,
	}}
}

// extractRepoFlag 从命令行参数中提取 --repo, --repository, --image, -i 参数
func extractRepoFlag(args []string) ([]string, string) {
	var cleaned []string
	repo := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--repo" || arg == "--repository" || arg == "--image" || arg == "-i" {
			if i+1 < len(args) {
				repo = args[i+1]
				i++
				continue
			}
		} else if strings.HasPrefix(arg, "--repo=") {
			repo = strings.TrimPrefix(arg, "--repo=")
			continue
		} else if strings.HasPrefix(arg, "--repository=") {
			repo = strings.TrimPrefix(arg, "--repository=")
			continue
		} else if strings.HasPrefix(arg, "--image=") {
			repo = strings.TrimPrefix(arg, "--image=")
			continue
		} else if strings.HasPrefix(arg, "-i=") {
			repo = strings.TrimPrefix(arg, "-i=")
			continue
		}
		cleaned = append(cleaned, arg)
	}
	return cleaned, repo
}

// extractKeyFlag 从命令行参数中提取 --key 或 -k 参数
func extractKeyFlag(args []string) ([]string, string) {
	var cleaned []string
	var key string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--key" || arg == "-k" {
			if i+1 < len(args) {
				key = args[i+1]
				i++
			}
		} else if strings.HasPrefix(arg, "--key=") {
			key = strings.TrimPrefix(arg, "--key=")
		} else if strings.HasPrefix(arg, "-k=") {
			key = strings.TrimPrefix(arg, "-k=")
		} else {
			cleaned = append(cleaned, arg)
		}
	}

	return cleaned, key
}

// extractEngineFlag 从命令行参数中提取 --engine 或 -e 参数 (默认 oci)
func extractEngineFlag(args []string) ([]string, string) {
	var cleaned []string
	engine := ""

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--engine" || arg == "-e" {
			if i+1 < len(args) {
				engine = strings.ToLower(args[i+1])
				i++
				continue
			}
		} else if strings.HasPrefix(arg, "--engine=") {
			engine = strings.ToLower(strings.TrimPrefix(arg, "--engine="))
			continue
		} else if strings.HasPrefix(arg, "-e=") {
			engine = strings.ToLower(strings.TrimPrefix(arg, "-e="))
			continue
		}
		cleaned = append(cleaned, arg)
	}

	if engine == "" {
		engine = strings.ToLower(strings.TrimSpace(os.Getenv(config.EnvArkEngine)))
	}
	if engine == "" {
		engine = config.DefaultEngine
	}

	return cleaned, engine
}

// extractDestFlag 从命令行参数中提取 --dest, --destination, --output, -o 目标恢复目录参数
func extractDestFlag(args []string) ([]string, string) {
	var cleaned []string
	dest := ""

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--dest" || arg == "--destination" || arg == "--output" || arg == "-o" {
			if i+1 < len(args) {
				dest = args[i+1]
				i++
				continue
			}
		} else if strings.HasPrefix(arg, "--dest=") {
			dest = strings.TrimPrefix(arg, "--dest=")
			continue
		} else if strings.HasPrefix(arg, "--destination=") {
			dest = strings.TrimPrefix(arg, "--destination=")
			continue
		} else if strings.HasPrefix(arg, "--output=") {
			dest = strings.TrimPrefix(arg, "--output=")
			continue
		} else if strings.HasPrefix(arg, "-o=") {
			dest = strings.TrimPrefix(arg, "-o=")
			continue
		}
		cleaned = append(cleaned, arg)
	}

	return cleaned, dest
}

// resolveBackupDir 综合解析待备份目录路径: 命令行参数 > 环境变量 (ARK_BACKUP_DIR / ARK_BACKUP_PATH / ARK_SOURCE_DIR) > 工作区根目录
func resolveBackupDir(ws, cliDir string) string {
	if strings.TrimSpace(cliDir) != "" {
		return strings.TrimSpace(cliDir)
	}
	loadEnvFile(ws)
	for _, key := range []string{config.EnvArkBackupDir, config.EnvArkBackupPath, config.EnvArkSourceDir} {
		if val := strings.TrimSpace(os.Getenv(key)); val != "" {
			parts := strings.Split(val, ",")
			target := strings.TrimSpace(parts[0])
			if !filepath.IsAbs(target) {
				return filepath.Join(ws, target)
			}
			return target
		}
	}
	return ws
}

// resolveBackupSourcesFromEnv 从 .env 环境变量中动态探测并装配备份货舱舱位源:
// 1. 优先检索 ARK_BACKUP_DIR / ARK_BACKUP_PATH / ARK_SOURCE_DIR;
// 2. 支持以逗号/分号分隔的多个目录，如: "/data/web, /data/db";
// 3. 若为单个总目录且包含子结构，自动调用 scanner.ScanRoot 智能分离子模块与根同级文件;
func resolveBackupSourcesFromEnv(ws string) []config.Source {
	loadEnvFile(ws)
	var rawPath string
	for _, key := range []string{config.EnvArkBackupDir, config.EnvArkBackupPath, config.EnvArkSourceDir} {
		if val := strings.TrimSpace(os.Getenv(key)); val != "" {
			rawPath = val
			break
		}
	}
	if rawPath == "" {
		return nil
	}

	rawParts := strings.FieldsFunc(rawPath, func(r rune) bool {
		return r == ',' || r == ';'
	})

	sources := make([]config.Source, 0)
	if len(rawParts) > 1 {
		for i, part := range rawParts {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			if !filepath.IsAbs(p) {
				p = filepath.Join(ws, p)
			}
			baseName := filepath.Base(p)
			id := strings.Map(func(r rune) rune {
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
					return r
				}
				return '_'
			}, baseName)
			if id == "" {
				id = fmt.Sprintf("src_%d", i+1)
			}
			sources = append(sources, config.Source{
				ID:       id,
				Name:     baseName,
				Path:     p,
				Priority: config.DefaultPriorityBase + i*5,
			})
		}
		return sources
	}

	if len(rawParts) == 1 {
		singlePath := strings.TrimSpace(rawParts[0])
		if !filepath.IsAbs(singlePath) {
			singlePath = filepath.Join(ws, singlePath)
		}

		if stat, err := os.Stat(singlePath); err == nil && stat.IsDir() {
			if results, err := scanner.ScanRoot(singlePath); err == nil && len(results) > 0 {
				for _, r := range results {
					sources = append(sources, r.Source)
				}
				return sources
			}
			baseName := filepath.Base(singlePath)
			sources = append(sources, config.Source{
				ID:       baseName,
				Name:     baseName,
				Path:     singlePath,
				Priority: config.DefaultPriorityBase,
			})
			return sources
		}
	}

	return nil
}

// LoadAppConfig 统一配置加载引擎 (全局唯一的配置加载入口):
// 1. 自动载入工作区根目录与 .env 环境配置；
// 2. 尝试读取工作区 config.json 并自动对 sources 路径执行环境变量展开；
// 3. 若物理 config.json 不存在或 sources 为空，自动通过 .env (ARK_BACKUP_DIR) 填充动态备份源；
// 4. 结合命令行参数 (--repo)、环境变量 (ARK_REPOSITORY) 和配置文件确定最终 Repository；
// 5. 返回 (*config.Config, hasConfigFile bool, err error)。
func LoadAppConfig(ws, cliRepo string) (*config.Config, bool, error) {
	loadEnvFile(ws)

	configPath := filepath.Join(ws, config.ConfigFileName)
	cfg, err := config.Load(configPath)
	if err != nil {
		category := os.Getenv(config.EnvArkCategory)
		if category == "" {
			category = config.DefaultCategory
		}
		defaultClean := true
		defaultCleanAll := false
		if val := strings.ToLower(os.Getenv(config.EnvArkCleanAllAfterPush)); val == "true" || val == "1" {
			defaultCleanAll = true
		}

		retentionCount := config.DefaultRetentionCount
		if val := strings.TrimSpace(os.Getenv(config.EnvArkRetentionCount)); val != "" {
			if count, err := strconv.Atoi(val); err == nil && count > 0 {
				retentionCount = count
			}
		}

		cfg = &config.Config{
			Repository:        resolveRepository("", cliRepo),
			Category:          category,
			RetentionCount:    retentionCount,
			Encrypt:           true,
			TagPrecision:      config.DefaultTagPrecision,
			PushRetry:         config.DefaultPushRetry,
			CleanAfterPush:    &defaultClean,
			CleanAllAfterPush: &defaultCleanAll,
			Sources:           make([]config.Source, 0),
		}

		// 核心亮点：免 config.json 场景下，自动装载来自 .env (ARK_BACKUP_DIR) 的备份舱位
		if envSources := resolveBackupSourcesFromEnv(ws); len(envSources) > 0 {
			cfg.Sources = envSources
		}

		fixedTag := strings.TrimSpace(os.Getenv(config.EnvArkTag))
		if fixedTag == "" {
			fixedTag = strings.TrimSpace(os.Getenv(config.EnvArkFixedTag))
		}
		if fixedTag != "" {
			cfg.FixedTag = fixedTag
		}

		return cfg, false, err
	}

	// 若 config.json 存在，但环境变量指定了固定 Tag 或保留个数，则允许环境变量覆盖
	fixedTag := strings.TrimSpace(os.Getenv(config.EnvArkTag))
	if fixedTag == "" {
		fixedTag = strings.TrimSpace(os.Getenv(config.EnvArkFixedTag))
	}
	if fixedTag != "" {
		cfg.FixedTag = fixedTag
	}

	if val := strings.TrimSpace(os.Getenv(config.EnvArkRetentionCount)); val != "" {
		if count, err := strconv.Atoi(val); err == nil && count > 0 {
			cfg.RetentionCount = count
		}
	}

	// 若 config.json 存在但 sources 列表为空，尝试通过 .env 中的 ARK_BACKUP_DIR 自动补全
	if len(cfg.Sources) == 0 {
		if envSources := resolveBackupSourcesFromEnv(ws); len(envSources) > 0 {
			cfg.Sources = envSources
		}
	}

	cfg.Repository = resolveRepository(cfg.Repository, cliRepo)
	return cfg, true, nil
}
