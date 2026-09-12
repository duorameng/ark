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

	var derivedSecret string

	if len(args) > 0 {
		rawInput := strings.TrimSpace(args[0])
		if rawInput == "" {
			fmt.Fprintln(os.Stderr, "[-] 口令不能为空")
			os.Exit(1)
		}
		// 使用密码学单向哈希派生生成确定性密文 (跨机器绝对一致，绝无明文)
		derivedSecret = DeriveSealKey(rawInput)
		fmt.Println("[安全] 正在通过确定性 KDF 生成安全工作密文...")
	} else {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			fmt.Fprintf(os.Stderr, "[-] 生成真随机数失败: %v\n", err)
			os.Exit(1)
		}
		derivedSecret = base64.StdEncoding.EncodeToString(buf)
		fmt.Println("[安全] 已生成全新 32 字节随机安全工作密文...")
	}

	// 1. 写入工作区 keys/seal.key (仅存储密文，非明文)
	if err := os.WriteFile(keyPath, []byte(derivedSecret+"\n"), 0600); err != nil {
		fmt.Fprintf(os.Stderr, "[-] 保存密钥失败: %v\n", err)
		os.Exit(1)
	}

	// 2. 同步写入用户家目录 ~/.ark/seal.key (保证全机任意路径执行均全自动免密码)
	if home, err := os.UserHomeDir(); err == nil {
		userArkDir := filepath.Join(home, ".ark")
		_ = os.MkdirAll(userArkDir, 0700)
		_ = os.WriteFile(filepath.Join(userArkDir, "seal.key"), []byte(derivedSecret+"\n"), 0600)
	}

	fmt.Println("================================================================")
	fmt.Printf("✓ 安全封条已配置完成！(存储模式: 非明文加密密文)\n")
	fmt.Printf("  • 本地持久化路径: %s\n", keyPath)
	fmt.Printf("  • 生成的安全密文: %s\n", derivedSecret)
	fmt.Println("================================================================")
	fmt.Println("【全自动免密使用指南】:")
	fmt.Println("  1. 本机免密码：后续所有指令 (ark board / land / unpack / check) 将全自动静默加载该密文，无需再输入密码。")
	if len(args) > 0 {
		fmt.Println("  2. 跨机器一致：在其他机器上同样执行一次: ark keygen \"<您的口令>\"，生成的密文完全一致，即可无缝解封还原！")
	}
	fmt.Println("================================================================")
}
