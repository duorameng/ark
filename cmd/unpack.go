package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ark/pkg/archive"
	"ark/pkg/config"
)

func runUnpack(args []string) {
	cleanedArgs, cliKey := extractKeyFlag(args)
	cleanedArgs, cliDest := extractDestFlag(cleanedArgs)
	args = cleanedArgs

	ws := getWorkspaceRoot()
	loadEnvFile(ws)
	targetPath := "cache"
	destDir := ""

	if len(args) > 0 {
		targetPath = args[0]
	}
	if cliDest != "" {
		destDir = cliDest
	} else if len(args) > 1 {
		destDir = args[1]
	} else if envRestore := os.Getenv(config.EnvArkRestoreDir); envRestore != "" {
		destDir = envRestore
	}

	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(ws, targetPath)
	}

	PrintBanner("📦 Ark 本地货物独立解封系统 (Zero-Docker Engine)")

	var sealPass []byte
	if pass, err := resolveSealKey(ws, false, cliKey); err == nil {
		sealPass = pass
	}

	stat, err := os.Stat(targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] 找不到目标货物文件或目录: %s\n", targetPath)
		os.Exit(1)
	}

	tmpDir := filepath.Join(ws, "tmp", fmt.Sprintf("unpack_%d", time.Now().Unix()))
	_ = os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	if !stat.IsDir() {
		// 单文件解封
		fname := filepath.Base(targetPath)
		modName := strings.TrimSuffix(fname, filepath.Ext(fname))
		modName = strings.TrimSuffix(modName, ".tar")
		if destDir == "" {
			destDir = filepath.Join(ws, fmt.Sprintf("cargo_restored_%s", modName))
		}

		fmt.Printf("[目标] 单集装箱: %s\n", targetPath)
		fmt.Printf("[交付] 恢复目录: %s\n", destDir)

		if archive.IsEncryptedArchive(fname) {
			if len(sealPass) == 0 {
				fmt.Fprintln(os.Stderr, "[-] 未提供有效安全密钥，无法开启密闭集装箱！")
				os.Exit(1)
			}
			fmt.Printf("-> 正在开启安全封条并流式还原 (%s -> %s, 零中间解密文件落盘)...\n", fname, destDir)
			if err := archive.UnsealAndUnpackStream(targetPath, destDir, sealPass); err != nil {
				fmt.Fprintf(os.Stderr, "[-] 解封还原失败: %v\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Printf("-> 正在还原舱位货物到 %s (保留 UID/GID 数字所有者与权限)...\n", destDir)
			if err := archive.UnpackTar(targetPath, destDir); err != nil {
				fmt.Fprintf(os.Stderr, "[-] 还原解包失败: %v\n", err)
				os.Exit(1)
			}
		}
		fmt.Printf("✓ 舱位 [%s] 货物已完整归位！\n", modName)
	} else {
		// 整个 cache 目录解封
		if destDir == "" {
			destDir = filepath.Join(ws, "cargo_restored_all")
		}
		fmt.Printf("[目标] 集装箱仓库: %s\n", targetPath)
		fmt.Printf("[交付] 批量恢复目录: %s\n", destDir)

		folderNameMap := make(map[string]string)
		sourceMap := make(map[string]config.Source)
		if cfg, _, err := LoadAppConfig(ws, ""); err == nil && cfg != nil {
			for _, src := range cfg.Sources {
				folderNameMap[src.ID] = filepath.Base(src.Path)
				sourceMap[src.ID] = src
			}
		}

		entries, _ := os.ReadDir(targetPath)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			fname := e.Name()
			if !archive.IsSupportedArchive(fname) {
				continue
			}

			modName := strings.TrimSuffix(fname, filepath.Ext(fname))
			modName = strings.TrimSuffix(modName, ".tar")
			folderName := modName
			if realName, ok := folderNameMap[modName]; ok && realName != "" && realName != "." && realName != "/" {
				folderName = realName
			}

			// 精准判定：以 Source 中的 IsRootFiles() 为准，无配置时仅匹配系统专属 ID，杜绝同名常规文件夹冲突
			isRootFiles := false
			if src, exists := sourceMap[modName]; exists {
				isRootFiles = src.IsRootFiles()
			} else {
				isRootFiles = (modName == config.DefaultRootFilesID)
			}

			targetSubDir := filepath.Join(destDir, folderName)
			if isRootFiles {
				targetSubDir = destDir
			}
			fullFile := filepath.Join(targetPath, fname)

			if archive.IsEncryptedArchive(fname) {
				if len(sealPass) == 0 {
					fmt.Printf("[!] 跳过密闭集装箱 %s (缺少安全密钥)\n", fname)
					continue
				}
				if isRootFiles {
					fmt.Printf("-> 正在开启安全封条并原位展开根级同级文件 (%s -> %s)...\n", fname, destDir)
				} else {
					fmt.Printf("-> 正在开启安全封条并流式还原 (%s -> %s, 零中间解密文件落盘)...\n", fname, targetSubDir)
				}
				if err := archive.UnsealAndUnpackStream(fullFile, targetSubDir, sealPass); err != nil {
					fmt.Printf("[-] 解封 %s 失败: %v\n", fname, err)
					continue
				}
			} else {
				if isRootFiles {
					fmt.Printf("-> 正在解包并原位展开根级同级文件至 %s...\n", destDir)
				} else {
					fmt.Printf("-> 正在还原舱位货物 [%s] 到 %s...\n", folderName, targetSubDir)
				}
				if err := archive.UnpackTar(fullFile, targetSubDir); err != nil {
					fmt.Printf("[-] 还原 %s 失败: %v\n", folderName, err)
					continue
				}
			}
			if isRootFiles {
				fmt.Println("   ✓ 根级同级配置文件与脚本已成功原位展开归位！")
			} else {
				fmt.Printf("   ✓ 舱位 [%s] 货物已完整归位！\n", folderName)
			}
		}
	}

	fmt.Println("\n================================================================")
	fmt.Printf("          🎉 解封还原圆满完成，全部货物已归位: %s\n", destDir)
	fmt.Println("================================================================")
}
