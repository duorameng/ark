package cmd

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
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

// DeriveSealKey 将用户自定义口令转换为 256 位确定性安全密文 (单向哈希派生，跨机器绝对一致，绝不存储明文密码)
func DeriveSealKey(rawPass string) string {
	rawPass = strings.TrimSpace(rawPass)
	h := hmac.New(sha256.New, []byte("ARK_SEAL_KDF_SALT_V1"))
	h.Write([]byte(rawPass))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
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

// getSealPassphraseFromEnv 从环境变量或 .env 中按优先级获取用户配置的原始封条口令
func getSealPassphraseFromEnv(ws string) string {
	loadEnvFile(ws)
	for _, k := range []string{"ARK_SEAL_KEY", "ARK_KEY", "SEAL_KEY"} {
		if val := strings.TrimSpace(os.Getenv(k)); val != "" {
			return val
		}
	}
	return ""
}

// syncPersistedKey 确保本地密钥文件与派生出的非明文工作密文 100% 同步
func syncPersistedKey(ws string, derivedSecret string) {
	derivedSecret = strings.TrimSpace(derivedSecret)
	if derivedSecret == "" {
		return
	}

	keysDir := filepath.Join(ws, "keys")
	keyPath := filepath.Join(keysDir, "seal.key")
	_ = os.MkdirAll(keysDir, 0700)

	existing, _ := os.ReadFile(keyPath)
	if strings.TrimSpace(string(existing)) != derivedSecret {
		_ = os.WriteFile(keyPath, []byte(derivedSecret+"\n"), 0600)
	}

	if home, err := os.UserHomeDir(); err == nil {
		userArkDir := filepath.Join(home, ".ark")
		_ = os.MkdirAll(userArkDir, 0700)
		homeKeyPath := filepath.Join(userArkDir, "seal.key")
		existingHome, _ := os.ReadFile(homeKeyPath)
		if strings.TrimSpace(string(existingHome)) != derivedSecret {
			_ = os.WriteFile(homeKeyPath, []byte(derivedSecret+"\n"), 0600)
		}
	}
}

// resolveSealKey 统一解析用于加密和解密的工作密文
// 只要配置了 ARK_SEAL_KEY / ARK_KEY 或通过 --key 指定口令，100% 保证工作密钥由此生成并同步本地 keys/seal.key
func resolveSealKey(ws string, allowGenerate bool, cliKey string) ([]byte, error) {
	rawKey := strings.TrimSpace(cliKey)
	if rawKey == "" {
		rawKey = getSealPassphraseFromEnv(ws)
	}

	// 1. 优先使用显式指定/配置的口令：确定性派生密文，并同步落盘保证本地密钥一致
	if rawKey != "" {
		derived := DeriveSealKey(rawKey)
		syncPersistedKey(ws, derived)
		return []byte(derived), nil
	}

	// 2. 未配置口令时，读取本地已有持久化密文文件 (ark keygen 生成的非明文文件)
	if key, _ := findPersistedKey(ws); key != "" {
		return []byte(key), nil
	}

	// 3. 若未找到且不允许自动生成，提示终端交互输入口令
	if !allowGenerate {
		fmt.Print("[安全] 未检测到本地安全封条密钥，请输入安全口令: ")
		var input string
		fmt.Scanln(&input)
		input = strings.TrimSpace(input)
		if input != "" {
			derived := DeriveSealKey(input)
			syncPersistedKey(ws, derived)
			return []byte(derived), nil
		}
		return nil, fmt.Errorf("未提供有效封条密钥，无法解密封存货物")
	}

	// 4. 首次装载且完全无口令：生成真随机密文并持久化
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	derivedKey := base64.StdEncoding.EncodeToString(buf)
	syncPersistedKey(ws, derivedKey)

	keyPath := filepath.Join(ws, "keys", "seal.key")
	fmt.Printf("[安全] 首次装载，已自动生成非明文安全封条密钥: %s\n", keyPath)
	return []byte(derivedKey), nil
}



