package cmd

import (
	"crypto/rand"
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

func resolveSealKey(ws string, allowGenerate bool) ([]byte, error) {
	if k := os.Getenv("ARK_KEY"); k != "" {
		return []byte(strings.TrimSpace(k)), nil
	}
	if k := os.Getenv("SEAL_KEY"); k != "" {
		return []byte(strings.TrimSpace(k)), nil
	}

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

	keysDir := filepath.Join(ws, "keys")
	keyPath := filepath.Join(keysDir, "seal.key")
	if data, err := os.ReadFile(keyPath); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		return []byte(strings.TrimSpace(string(data))), nil
	}

	if !allowGenerate {
		fmt.Print("[安全] 未检测到密钥文件，请输入安全封条密码 (Seal Key): ")
		var input string
		fmt.Scanln(&input)
		input = strings.TrimSpace(input)
		if input != "" {
			return []byte(input), nil
		}
		return nil, fmt.Errorf("未提供有效封条密钥，无法开启密闭集装箱")
	}

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
