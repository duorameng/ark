package cmd

import (
	"fmt"
	"os"

	"ark/pkg/update"
)

// AppVersion 当前应用程序版本号
var AppVersion = "v1.0.0"

// Execute 统一命令分发与调度入口
func Execute(version string) {
	if version != "" {
		AppVersion = version
	}

	update.CleanOldExecutable()

	if len(os.Args) < 2 {
		PrintHelp()
		return
	}

	action := os.Args[1]
	args := os.Args[2:]

	switch action {
	case "board", "登船":
		runBoard(args, false)
	case "dry", "试航":
		runBoard(args, true)
	case "land", "下船":
		runLand(args)
	case "unpack", "解封", "解包":
		runUnpack(args)
	case "scan", "扫描":
		runScan(args)
	case "list", "查验":
		runList(args)
	case "keygen", "密钥":
		runKeygen(args)
	case "update", "升级", "self-update":
		runUpdate(args)
	case "completion", "补全":
		runCompletion(args)
	case "version", "-v", "--version", "版本":
		fmt.Printf("ark version %s (%s/%s)\n", AppVersion, os.Getenv("GOOS"), os.Getenv("GOARCH"))
	case "help", "--help", "-h", "帮助":
		PrintHelp()
	default:
		fmt.Fprintf(os.Stderr, "[-] Unknown command: %s\nRun 'ark help' for usage.\n\n", action)
		PrintHelp()
		os.Exit(1)
	}
}

// PrintHelp 输出终端使用帮助指南
func PrintHelp() {
	fmt.Printf("🚢 Ark 班轮货运管理终端 (Vessel Logistics CLI - Golang Engine) %s\n", AppVersion)
	fmt.Println()
	fmt.Println("用法 (Usage):")
	fmt.Println("  ark <command> [arguments...]")
	fmt.Println()
	fmt.Println("可用指令 (Available Commands):")
	fmt.Println("  board       打包各舱位目录，施加 AES-256 安全密闭封条并推送到云端班轮 (BuildKit COPY --link)")
	fmt.Println("  land        从港口调取班轮镜像快照，解封解密并完整归位货物")
	fmt.Println("  unpack      无需 Docker 引擎，单二进制直接从本地快照包 (.dat/.tar) 独立解封还原货物")
	fmt.Println("  scan        全自动扫描父目录，按冷热变动率智能排序生成 sources 清单")
	fmt.Println("  list        查询远端港口已停泊的所有航次班次与创建日期")
	fmt.Println("  keygen      配置或生成专属安全封条加密密码 (支持自定义密码或自动生成)")
	fmt.Println("  update      从发布源检查最新版本并就地自我升级 (内置国内多镜像源自动容灾重试)")
	fmt.Println("  completion  生成或安装 Shell 自动补全脚本 (支持 bash/zsh/powershell/fish)")
	fmt.Println("  dry         模拟清点货舱与快照封装，打印 Dockerfile 构型，不执行实际推送 (DRY RUN)")
	fmt.Println("  version     显示当前程序版本及编译信息")
	fmt.Println("  help        显示本帮助指南")
	fmt.Println()
	fmt.Println("示例 (Examples):")
	fmt.Println("  ark scan /root/workspace      # 自动探测子工程，分析冷热变动率并智能排序")
	fmt.Println("  ark board                     # 使用 config.json 默认分类执行装载登船")
	fmt.Println("  ark board db                  # 临时指定分类为 db 范围执行登船")
	fmt.Println("  ark list                      # 检索港口所有航次记录")
	fmt.Println("  ark land vps                  # 靠岸卸载还原 vps 分类的最新航次 (vps-latest)")
	fmt.Println("  ark land vps-20260912-140000  # 靠岸卸载还原指定历史日期的航次")
	fmt.Println("  ark unpack cache/postgres.dat # 零 Docker 依赖，直接解密封并恢复 postgres 数据")
	fmt.Println("  ark update                    # 检查最新版本并自动通过国内镜像源极速升级自身")
	fmt.Println("  ark completion install        # 一键自动将补全挂载至 ~/.bashrc 或 ~/.zshrc")
}
