package cmd

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ManifestEntry 货舱各集装箱指纹与元数据
type ManifestEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	TreeHash    string `json:"tree_hash"`
	LayerFile   string `json:"layer_file"`
	LayerSHA256 string `json:"layer_sha256"`
	UpdatedAt   string `json:"updated_at"`
}

func getWorkspaceRoot() string {
	exePath, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exePath)
		if filepath.Base(dir) == "scripts" || filepath.Base(dir) == "tmp" {
			return filepath.Dir(dir)
		}
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

	cmd := exec.Command("gh", "auth", "token")
	if out, err := cmd.Output(); err == nil {
		tok := strings.TrimSpace(string(out))
		if tok != "" {
			return tok
		}
	}

	return ""
}

// DeriveSealKey 将用户自定义口令转换为 256 位确定性安全密文 (单向哈希派生，跨机器绝对一致，绝不存储明文密码)
func DeriveSealKey(rawPass string) string {
	rawPass = strings.TrimSpace(rawPass)
	h := hmac.New(sha256.New, []byte("ARK_SEAL_KDF_SALT_V1"))
	h.Write([]byte(rawPass))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
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

	return cleaned, strings.TrimSpace(key)
}

// findPersistedKey 查找本地持久化的非明文密钥文件 (多路径智能探测)
func findPersistedKey(ws string) (string, string) {
	candidates := []string{
		filepath.Join(ws, "keys", "seal.key"),
	}

	if cwd, err := os.Getwd(); err == nil && cwd != ws {
		candidates = append(candidates, filepath.Join(cwd, "keys", "seal.key"))
	}

	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".ark", "seal.key"))
	}

	for _, p := range candidates {
		if data, err := os.ReadFile(p); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					// 兼容已有前缀格式
					key := strings.TrimPrefix(line, "ark-v1:")
					return key, p
				}
			}
		}
	}

	return "", ""
}

// resolveSealKey 统一解析用于加密和解密的工作密文 (优先使用本地持久化密文，实现全自动免密码)
func resolveSealKey(ws string, allowGenerate bool, cliKey string) ([]byte, error) {
	// 1. 命令行 --key 显式传参 (若用户临时指定口令，自动派生为密文)
	if cliKey != "" {
		derived := DeriveSealKey(cliKey)
		return []byte(derived), nil
	}

	// 2. 环境变量 ARK_KEY 或 SEAL_KEY (自动派生)
	if k := os.Getenv("ARK_KEY"); k != "" {
		return []byte(DeriveSealKey(k)), nil
	}
	if k := os.Getenv("SEAL_KEY"); k != "" {
		return []byte(DeriveSealKey(k)), nil
	}

	// 3. 工作区 .env 文件
	envPath := filepath.Join(ws, ".env")
	if envData, err := os.ReadFile(envPath); err == nil {
		for _, line := range strings.Split(string(envData), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "ARK_KEY=") || strings.HasPrefix(line, "SEAL_KEY=") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					val := strings.Trim(parts[1], `"' `)
					if val != "" {
						return []byte(DeriveSealKey(val)), nil
					}
				}
			}
		}
	}

	// 4. 优先读取本地持久化密文文件 (ark keygen 生成的非明文文件，实现完全免密操作)
	if key, _ := findPersistedKey(ws); key != "" {
		return []byte(key), nil
	}

	// 5. 若未找到且不允许自动生成，提示终端输入口令
	if !allowGenerate {
		fmt.Print("[安全] 未检测到本地安全封条密钥文件，请输入安全口令: ")
		var input string
		fmt.Scanln(&input)
		input = strings.TrimSpace(input)
		if input != "" {
			return []byte(DeriveSealKey(input)), nil
		}
		return nil, fmt.Errorf("未提供有效封条密钥，无法解密封存货物")
	}

	// 6. 首次装载且允许自动生成：生成随机密文并持久化
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	derivedKey := base64.StdEncoding.EncodeToString(buf)

	keysDir := filepath.Join(ws, "keys")
	keyPath := filepath.Join(keysDir, "seal.key")
	_ = os.MkdirAll(keysDir, 0700)
	_ = os.WriteFile(keyPath, []byte(derivedKey+"\n"), 0600)

	fmt.Printf("[安全] 首次装载，已自动生成非明文安全封条密钥: %s\n", keyPath)
	return []byte(derivedKey), nil
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

// resolveRepository 综合解析镜像仓库名称: 命令行参数 > 环境变量 > 配置文件
func resolveRepository(cfgRepo, cliRepo string) string {
	if strings.TrimSpace(cliRepo) != "" {
		return strings.TrimSpace(cliRepo)
	}
	if envRepo := os.Getenv("ARK_REPOSITORY"); strings.TrimSpace(envRepo) != "" {
		return strings.TrimSpace(envRepo)
	}
	if envImage := os.Getenv("ARK_IMAGE"); strings.TrimSpace(envImage) != "" {
		return strings.TrimSpace(envImage)
	}
	if strings.TrimSpace(cfgRepo) != "" {
		return strings.TrimSpace(cfgRepo)
	}
	return "ghcr.io/duorameng/ark"
}
