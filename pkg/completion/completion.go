package completion

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CommandInfo 命令元数据定义
type CommandInfo struct {
	Name string
	Desc string
}

// AllCommands 注册的全部英文一级指令与描述
var AllCommands = []CommandInfo{
	{Name: "check", Desc: "全面测试所有配置是否正确 (语法、货舱路径、加密封条与云端凭据)"},
	{Name: "board", Desc: "打包各舱位目录并推送到云端班轮 (BuildKit COPY --link)"},
	{Name: "land", Desc: "从港口调取镜像快照并解密还原归位货物"},
	{Name: "unpack", Desc: "无需 Docker 引擎，单二进制独立解密还原货物"},
	{Name: "scan", Desc: "自动扫描父目录，按冷热变动率智能排序"},
	{Name: "list", Desc: "查询远端港口停泊的所有航次记录与日期"},
	{Name: "keygen", Desc: "配置或生成专属安全封条加密密码"},
	{Name: "update", Desc: "检查最新发布版本并就地自我升级 (含国内源容灾)"},
	{Name: "dry", Desc: "模拟登船全流程，不执行实际上传 (DRY RUN)"},
	{Name: "completion", Desc: "生成或安装 Shell 自动补全脚本"},
	{Name: "version", Desc: "查看当前编译版本及环境信息"},
	{Name: "help", Desc: "显示帮助指南信息"},
}

// GenBash 生成 Bash 自动补全脚本
func GenBash() string {
	cmdNames := make([]string, 0, len(AllCommands))
	for _, c := range AllCommands {
		cmdNames = append(cmdNames, c.Name)
	}

	return fmt.Sprintf(`#!/usr/bin/env bash
# Bash completion for ark

_ark_completion() {
    local cur prev words cword
    if declare -F _init_completion >/dev/null 2>&1; then
        _init_completion || return
    else
        COMPREPLY=()
        cur="${COMP_WORDS[COMP_CWORD]}"
        prev=""
        if [ "$COMP_CWORD" -ge 1 ]; then
            prev="${COMP_WORDS[COMP_CWORD-1]}"
        fi
        cword=$COMP_CWORD
    fi

    local commands="%s"

    if [ "$cword" -eq 1 ]; then
        COMPREPLY=( $(compgen -W "${commands}" -- "$cur") )
        return 0
    fi

    case "${prev}" in
        board|land|list)
            # Suggest categories or historical tags
            COMPREPLY=( $(compgen -W "vps db default" -- "$cur") )
            return 0
            ;;
        unpack|scan)
            # Complete directory or file paths
            if declare -F _filedir >/dev/null 2>&1; then
                _filedir
            else
                COMPREPLY=( $(compgen -f -- "$cur") )
            fi
            return 0
            ;;
        completion)
            COMPREPLY=( $(compgen -W "bash zsh powershell fish install" -- "$cur") )
            return 0
            ;;
        *)
            ;;
    esac
}

complete -F _ark_completion ark
complete -F _ark_completion ./ark
`, strings.Join(cmdNames, " "))
}

// GenZsh 生成 Zsh 自动补全脚本 (带命令描述)
func GenZsh() string {
	var sb strings.Builder
	sb.WriteString(`#compdef ark
# Zsh completion for ark

_ark() {
    local -a commands
    commands=(
`)
	for _, c := range AllCommands {
		sb.WriteString(fmt.Sprintf("        '%s:%s'\n", c.Name, strings.ReplaceAll(c.Desc, "'", "")))
	}
	sb.WriteString(`    )

    _arguments -C \
        '1: :->command' \
        '*: :->args'

    case $state in
        command)
            _describe -t commands 'ark command' commands
            ;;
        args)
            case $line[1] in
                unpack|scan)
                    _files
                    ;;
                completion)
                    local -a shells
                    shells=('bash:Generate bash completion' 'zsh:Generate zsh completion' 'powershell:Generate powershell completion' 'fish:Generate fish completion' 'install:Auto install completion to shell profile')
                    _describe -t shells 'shell type' shells
                    ;;
                *)
                    ;;
            esac
            ;;
    esac
}

_ark "$@"
`)
	return sb.String()
}

// GenPowerShell 生成 PowerShell 自动补全脚本
func GenPowerShell() string {
	var sb strings.Builder
	sb.WriteString(`# PowerShell completion for ark
Register-ArgumentCompleter -Native -CommandName ark -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)

    $commandElements = $commandAst.CommandElements
    if ($commandElements.Count -le 2) {
        $commands = @(
`)
	for _, c := range AllCommands {
		sb.WriteString(fmt.Sprintf("            [System.Management.Automation.CompletionResult]::new('%s', '%s', 'ParameterValue', '%s')\n",
			c.Name, c.Name, c.Desc))
	}
	sb.WriteString(`        )
        $commands | Where-Object { $_.CompletionText -like "$wordToComplete*" }
        return
    }

    $subCommand = $commandElements[1].Extent.Text
    if ($subCommand -eq "completion") {
        $shells = @("bash", "zsh", "powershell", "fish", "install")
        $shells | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', "Generate completion for $_")
        }
    }
}
`)
	return sb.String()
}

// GenFish 生成 Fish 自动补全脚本
func GenFish() string {
	var sb strings.Builder
	sb.WriteString("# Fish completion for ark\n")
	for _, c := range AllCommands {
		sb.WriteString(fmt.Sprintf("complete -c ark -f -n '__fish_use_subcommand' -a '%s' -d '%s'\n", c.Name, c.Desc))
	}
	sb.WriteString("complete -c ark -f -n '__fish_seen_subcommand_from completion' -a 'bash zsh powershell fish install'\n")
	return sb.String()
}

// InstallShellCompletion 自动检测当前 Shell 并生成独立的本地补全脚本挂载至配置文件
// 即使全局未安装 ark（仅在当前目录下执行 ./ark），也能完美支持自动补全！
func InstallShellCompletion() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	shell := os.Getenv("SHELL")
	var compFile, targetRC, snippet string

	if strings.Contains(shell, "zsh") {
		compFile = filepath.Join(home, ".ark_completion.zsh")
		targetRC = filepath.Join(home, ".zshrc")
		snippet = GenZsh()
	} else if strings.Contains(shell, "fish") {
		fishDir := filepath.Join(home, ".config", "fish", "completions")
		_ = os.MkdirAll(fishDir, 0755)
		targetRC = filepath.Join(fishDir, "ark.fish")
		if err := os.WriteFile(targetRC, []byte(GenFish()), 0644); err != nil {
			return fmt.Errorf("写入 Fish 补全脚本失败: %w", err)
		}
		fmt.Printf("✓ Fish 自动补全脚本已写入: %s\n", targetRC)
		return nil
	} else {
		// 默认按 bash 处理
		compFile = filepath.Join(home, ".ark_completion.bash")
		targetRC = filepath.Join(home, ".bashrc")
		snippet = GenBash()
	}

	// 1. 将完整的自动补全定义写入独立的本地脚本文件，解除对系统全局二进制的依赖
	if err := os.WriteFile(compFile, []byte(snippet), 0644); err != nil {
		return fmt.Errorf("写入自动补全脚本文件 %s 失败: %w", compFile, err)
	}
	fmt.Printf("✓ 独立自动补全脚本已写入: %s\n", compFile)

	// 2. 挂载到终端配置文件 (支持自动清理历史失效配置)
	sourceLine := fmt.Sprintf("[ -f \"%s\" ] && source \"%s\"", compFile, compFile)
	ensureShellRCMount(targetRC, sourceLine)

	fmt.Printf("✓ 自动补全挂载配置已更新: %s\n", targetRC)
	fmt.Println()
	fmt.Println("🎉 安装完成！当前目录下敲 './ark <Tab>' 即可自动补全！")
	fmt.Printf("👉 请运行 'source %s' 或重新打开终端即可立即生效。\n", targetRC)
	return nil
}

// ensureShellRCMount 安全清理旧版本动态配置并写入最新的静态加载指令
func ensureShellRCMount(rcFile, mountLine string) {
	content, err := os.ReadFile(rcFile)
	if err != nil {
		f, createErr := os.OpenFile(rcFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if createErr == nil {
			_, _ = f.WriteString("\n# Ark CLI autocompletion\n" + mountLine + "\n")
			f.Close()
		}
		return
	}

	lines := strings.Split(string(content), "\n")
	var cleaned []string
	hasMount := false

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		// 清理旧版本遗留的动态调用 (如 source <(ark completion ...))
		if strings.Contains(trimmed, "ark completion") && (strings.Contains(trimmed, "source <(") || strings.Contains(trimmed, "eval \"$(")) {
			continue
		}
		if trimmed == mountLine {
			hasMount = true
		}
		cleaned = append(cleaned, l)
	}

	if !hasMount {
		cleaned = append(cleaned, "", "# Ark CLI autocompletion", mountLine)
	}

	_ = os.WriteFile(rcFile, []byte(strings.Join(cleaned, "\n")), 0644)
}
