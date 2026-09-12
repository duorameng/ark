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
    _init_completion || return

    local commands="%s"

    if [ "$cword" -eq 1 ]; then
        COMPREPLY=( $(compgen -W "${commands}" -- "$cur") )
        return 0
    fi

    case "${prev}" in
        board|land|list)
            # Suggest categories or historical tags
            COMPREPLY=( $(compgen -W "vps db default latest" -- "$cur") )
            return 0
            ;;
        unpack|scan)
            # Complete directory or file paths
            _filedir
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

// InstallShellCompletion 自动检测当前 Shell 并将自动补全挂载至配置文件
func InstallShellCompletion() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	shell := os.Getenv("SHELL")
	targetRC := ""
	snippet := ""

	if strings.Contains(shell, "zsh") {
		targetRC = filepath.Join(home, ".zshrc")
		snippet = "\n# Ark CLI autocompletion\neval \"$(ark completion zsh)\"\n"
	} else if strings.Contains(shell, "fish") {
		fishDir := filepath.Join(home, ".config", "fish", "completions")
		_ = os.MkdirAll(fishDir, 0755)
		targetRC = filepath.Join(fishDir, "ark.fish")
		snippet = GenFish()
		return os.WriteFile(targetRC, []byte(snippet), 0644)
	} else {
		// 默认按 bash 处理
		targetRC = filepath.Join(home, ".bashrc")
		snippet = "\n# Ark CLI autocompletion\nsource <(ark completion bash 2>/dev/null)\n"
	}

	content, _ := os.ReadFile(targetRC)
	if strings.Contains(string(content), "ark completion") {
		fmt.Printf("✓ 自动补全配置已存在于 %s，无需重复安装。\n", targetRC)
		return nil
	}

	f, err := os.OpenFile(targetRC, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("无法写入配置文件 %s: %w", targetRC, err)
	}
	defer f.Close()

	if _, err := f.WriteString(snippet); err != nil {
		return err
	}

	fmt.Printf("✓ 成功将自动补全载入配置文件: %s\n", targetRC)
	fmt.Println("👉 请运行 'source " + targetRC + "' 或重新打开终端，即刻享受 <Tab> 键极速补全！")
	return nil
}
