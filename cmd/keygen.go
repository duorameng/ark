package cmd

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
