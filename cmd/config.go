package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ark/pkg/config"
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
}

// loadToken 获取 GitHub 访问凭据 (按 .env -> 环境变量 -> gh CLI 顺序检索)
func loadToken(workspaceRoot string) string {
	loadEnvFile(workspaceRoot)

	if token := os.Getenv(config.EnvGhToken); token != "" {
		return strings.TrimSpace(token)
	}
	if token := os.Getenv(config.EnvGithubToken); token != "" {
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

// LoadAppConfig 统一配置加载引擎 (全局唯一的配置加载入口):
// 1. 自动载入工作区根目录与 .env 环境配置；
// 2. 尝试读取工作区 config.json；
// 3. 若物理 config.json 不存在 (灾难恢复/全新部署/只读查验场景)，构建默认配置对象，绝不阻断运行；
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

		cfg = &config.Config{
			Repository:        resolveRepository("", cliRepo),
			Category:          category,
			RetentionCount:    config.DefaultRetentionCount,
			Encrypt:           true,
			TagPrecision:      config.DefaultTagPrecision,
			PushRetry:         config.DefaultPushRetry,
			CleanAfterPush:    &defaultClean,
			CleanAllAfterPush: &defaultCleanAll,
			Sources:           make([]config.Source, 0),
		}
		return cfg, false, err
	}

	cfg.Repository = resolveRepository(cfg.Repository, cliRepo)
	return cfg, true, nil
}
