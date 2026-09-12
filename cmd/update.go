package cmd

import (
	"fmt"
	"os"

	"ark/pkg/update"
)

func runUpdate(args []string) {
	ws := getWorkspaceRoot()
	token := loadToken(ws)
	target := ""
	if len(args) > 0 {
		target = args[0]
	}

	fmt.Println("================================================================")
	fmt.Println("          🚀 Ark 班轮自我升级系统 (Self-Update System)          ")
	fmt.Println("================================================================")

	if err := update.SelfUpdate(AppVersion, target, token); err != nil {
		fmt.Fprintf(os.Stderr, "[-] 升级失败: %v\n", err)
		os.Exit(1)
	}
}
