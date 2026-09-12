package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ark/pkg/config"
	"ark/pkg/docker"
)

func runClean(args []string) {
	ws := getWorkspaceRoot()
	loadEnvFile(ws)

	includeAll := false
	includeDockerImages := false
	for _, arg := range args {
		switch arg {
		case "--all", "-a":
			includeAll = true
			includeDockerImages = true
		case "--docker", "-d":
			includeDockerImages = true
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
			configPath := filepath.Join(ws, "config.json")
			if cfg, err := config.Load(configPath); err == nil && cfg.Repository != "" {
				fmt.Printf("-> 正在检索并清理本地关联镜像: %s...\n", cfg.Repository)
				out, err := exec.Command("docker", "images", "--filter=reference="+cfg.Repository+"*", "-q").Output()
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

	if includeAll {
		fmt.Println("\n[!] 提示: 已根据 --all 参数完成工作区与容器层的彻底重置！")
	}

	fmt.Println("\n================================================================")
	fmt.Println("          🎉 本地工作区重置完毕，已恢复至 Ark 初始干净状态！      ")
	fmt.Println("================================================================")
}
