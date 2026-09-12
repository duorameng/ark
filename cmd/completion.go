package cmd

import (
	"fmt"
	"os"

	"ark/pkg/completion"
)

func runCompletion(args []string) {
	if len(args) == 0 {
		printCompletionHelp()
		return
	}

	target := args[0]
	switch target {
	case "bash":
		fmt.Print(completion.GenBash())
	case "zsh":
		fmt.Print(completion.GenZsh())
	case "powershell", "pwsh":
		fmt.Print(completion.GenPowerShell())
	case "fish":
		fmt.Print(completion.GenFish())
	case "install":
		if err := completion.InstallShellCompletion(); err != nil {
			fmt.Fprintf(os.Stderr, "[-] 安装自动补全失败: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "[-] 未知 Shell 类型: %s\n\n", target)
		printCompletionHelp()
		os.Exit(1)
	}
}

func printCompletionHelp() {
	fmt.Println("Ark Shell 自动补全脚本配置工具")
	fmt.Println()
	fmt.Println("用法 (Usage):")
	fmt.Println("  ark completion [bash|zsh|powershell|fish|install]")
	fmt.Println()
	fmt.Println("示例 (Examples):")
	fmt.Println("  # 一键自动安装补全到当前用户的 Shell 配置文件 (~/.bashrc 或 ~/.zshrc):")
	fmt.Println("  ark completion install")
	fmt.Println()
	fmt.Println("  # 在当前 Bash 会话临时启用补全:")
	fmt.Println("  source <(ark completion bash)")
	fmt.Println()
	fmt.Println("  # 在当前 Zsh 会话临时启用补全:")
	fmt.Println("  source <(ark completion zsh)")
	fmt.Println()
	fmt.Println("  # 在 PowerShell (pwsh) 中立即启用补全:")
	fmt.Println("  ark completion powershell | Out-String | Invoke-Expression")
}
