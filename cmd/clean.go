package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ark/pkg/docker"
	"ark/pkg/github"
)

func runClean(args []string) {
	cleanedArgs, cliRepo := extractRepoFlag(args)
	args = cleanedArgs

	ws := getWorkspaceRoot()
	cfg, _, _ := LoadAppConfig(ws, cliRepo)

	includeAll := false
	includeDockerImages := false
	cleanUntagged := false

	for _, arg := range args {
		switch arg {
		case "--all", "-a":
			includeAll = true
			includeDockerImages = true
			cleanUntagged = true
		case "--docker", "-d":
			includeDockerImages = true
		case "--untagged", "-u", "--remote", "-r", "--remote-untagged":
			cleanUntagged = true
		case "--help", "-h":
			printCleanHelp()
			return
		}
	}

	fmt.Println("================================================================")
	fmt.Println("          🧹 Ark 本地工作区与环境重置系统 (Workspace Reset)       ")
	fmt.Println("================================================================")
	fmt.Printf("[重置] 工作区根目录: %s\n\n", ws)

	// 1. 清理 cache/ 目录
	cacheDir := filepath.Join(ws, "cache")
	cleanedCacheFiles := 0
	if entries, err := os.ReadDir(cacheDir); err == nil {
		for _, e := range entries {
			p := filepath.Join(cacheDir, e.Name())
			_ = os.RemoveAll(p)
			cleanedCacheFiles++
		}
	}
	_ = os.MkdirAll(cacheDir, 0755)
	fmt.Printf("✓ [集装箱缓存] 已清空 cache/ 目录 (清理了 %d 个数据缓存与清单项)\n", cleanedCacheFiles)

	// 2. 清理 tmp/ 目录
	tmpDir := filepath.Join(ws, "tmp")
	cleanedTmpFiles := 0
	if entries, err := os.ReadDir(tmpDir); err == nil {
		for _, e := range entries {
			p := filepath.Join(tmpDir, e.Name())
			_ = os.RemoveAll(p)
			cleanedTmpFiles++
		}
	}
	_ = os.MkdirAll(tmpDir, 0755)
	fmt.Printf("✓ [临时工作层] 已清空 tmp/ 目录 (清理了 %d 个临时构建文件与目录)\n", cleanedTmpFiles)

	// 3. 清理工作区历史导出的货物目录 (cargo_landed_* 与 cargo_restored_*)
	cleanedCargos := 0
	if entries, err := os.ReadDir(ws); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, "cargo_landed_") || strings.HasPrefix(name, "cargo_restored_") {
				p := filepath.Join(ws, name)
				_ = os.RemoveAll(p)
				fmt.Printf("   -> 已移除历史交付货物目录: %s\n", name)
				cleanedCargos++
			}
		}
	}
	if cleanedCargos > 0 {
		fmt.Printf("✓ [历史交付物] 已清理 %d 个解压归位的落地货物目录\n", cleanedCargos)
	} else {
		fmt.Println("✓ [历史交付物] 工作区无历史残留的货物导出目录")
	}

	// 4. 清理 Docker 悬空镜像与 BuildKit 构建缓存
	dockerActive := docker.IsDaemonRunning()
	if dockerActive {
		fmt.Println("\n-> 正在清理 Docker 悬空镜像与 BuildKit 构建缓存...")
		if err := docker.PruneDanglingImages(); err == nil {
			fmt.Println("   ✓ 已清理 Docker 悬空镜像 (<none>:<none>)")
		}
		if err := docker.PruneBuildCache(); err == nil {
			fmt.Println("   ✓ 已深度释放 Docker BuildKit 编译构建缓存")
		}
	} else {
		fmt.Println("\n✓ [容器环境] 本地未运行 Docker 守护进程 (Zero-Docker 原生模式，0 容器垃圾残留)")
	}

	// 5. 如果指定了 --docker 或 --all，清理本地的仓库镜像
	if includeDockerImages {
		if !dockerActive {
			fmt.Println("   [!] 提示: 本地未运行 Docker 守护进程，跳过指定镜像清理。")
		} else {
			if cfg.Repository != "" {
				repoToClean := cfg.Repository
				fmt.Printf("-> 正在检索并清理本地关联镜像: %s...\n", repoToClean)
				out, err := exec.Command("docker", "images", "--filter=reference="+repoToClean+"*", "-q").Output()
				if err == nil && len(out) > 0 {
					ids := strings.Fields(string(out))
					cleanedImgCount := 0
					for _, id := range ids {
						if err := docker.RemoveImage(id); err == nil {
							cleanedImgCount++
						}
					}
					fmt.Printf("   ✓ 已删除 %d 个本地相关 Docker 镜像\n", cleanedImgCount)
				} else {
					fmt.Println("   ✓ 本地无残留的相关 Docker 镜像")
				}
			}
		}
	}

	// 6. 如果指定了 --untagged 或 --all，扫描并清理远端孤立未打标版本
	if cleanUntagged {
		fmt.Println("\n------------------- 正在扫描远端 GitHub 镜像港口 -------------------")
		repo := cfg.Repository
		token := loadToken(ws)

		if repo == "" {
			fmt.Println("[-] 未配置远端仓库地址 (Repository)，跳过远端未打标版本清理。")
		} else if token == "" {
			fmt.Println("[-] 未检测到通行凭据 GH_TOKEN，无法清理远端 GitHub Packages 悬空版本。")
		} else {
			fmt.Printf("-> 正在扫描远端仓库 [%s] 孤立未打标版本 (Untagged Versions)...\n", repo)
			ghClient := github.NewClient(repo, token)
			deletedCount, err := ghClient.PruneUntaggedVersions()
			if err != nil {
				fmt.Printf("[-] 扫描清理远端悬空版本遇到问题: %v\n", err)
			} else if deletedCount == 0 {
				fmt.Println("✓ 远端港口非常干净，未发现任何孤立 untagged 悬空版本。")
			} else {
				fmt.Printf("✓ 远端港口维护完毕，已成功清理 %d 个孤立悬空 untagged 版本！\n", deletedCount)
			}
		}
	}

	if includeAll {
		fmt.Println("\n[!] 提示: 已根据 --all 参数完成工作区、容器层与远端孤立悬空版本的彻底重置！")
	} else if !cleanUntagged {
		fmt.Println("\n💡 提示: 若需扫描并清理远端 GitHub Packages 悬空的孤立未打标版本，可执行: ark clean --untagged (或 ark clean --all)")
	}

	fmt.Println("\n================================================================")
	fmt.Println("          🎉 工作区重置完毕，已恢复至 Ark 初始干净状态！          ")
	fmt.Println("================================================================")
}

func printCleanHelp() {
	fmt.Println("用法 (Usage):")
	fmt.Println("  ark clean [options...]")
	fmt.Println()
	fmt.Println("选项 (Options):")
	fmt.Println("  -u, --untagged        自动扫描并清理远端 GitHub Packages 孤立/悬空的未打标 (untagged) 版本")
	fmt.Println("  -r, --remote          清理远端资源 (等同于 --untagged)")
	fmt.Println("  -d, --docker          同时清理本地 Docker 历史构建镜像")
	fmt.Println("  -a, --all             全量彻底重置: 本地缓存、临时文件、Docker 镜像与远端 untagged 版本")
	fmt.Println("      --repo <owner/pkg> 临时指定目标 GitHub 仓库地址")
	fmt.Println("  -h, --help            显示本清理指南")
}
