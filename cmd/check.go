package cmd

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ark/pkg/archive"
	"ark/pkg/config"
)

func runCheck(args []string) {
	cleanedArgs, cliKey := extractKeyFlag(args)
	cleanedArgs, cliTarget := extractTargetFlag(cleanedArgs)
	cleanedArgs, cliRepo := extractRepoFlag(cleanedArgs)
	_ = cleanedArgs

	ws := getWorkspaceRoot()
	loadEnvFile(ws)

	PrintBanner("🩺 Ark 航运配置与系统健康体检 (Diagnostic Check)")
	fmt.Printf("[体检] 正在扫描工作区: %s\n\n", ws)

	passCount := 0
	warnCount := 0
	failCount := 0

	printPass := func(title, msg string) {
		passCount++
		fmt.Printf("  ✓ [%s] %s\n", title, msg)
	}
	printWarn := func(title, msg string) {
		warnCount++
		fmt.Printf("  ⚠ [%s] %s\n", title, msg)
	}
	printFail := func(title, msg string) {
		failCount++
		fmt.Printf("  ✗ [%s] %s\n", title, msg)
	}

	// 1. 检查工作区与写权限
	fmt.Println("1. 工作区与环境基底 (Workspace & Filesystem):")
	if stat, err := os.Stat(ws); err != nil || !stat.IsDir() {
		printFail("工作区", fmt.Sprintf("无法访问工作区目录: %s", ws))
	} else {
		printPass("工作区", fmt.Sprintf("根路径已就绪: %s", ws))
	}

	tmpDir := filepath.Join(ws, config.TmpDirName)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		printFail("临时目录", fmt.Sprintf("创建或访问 tmp 目录失败: %v", err))
	} else {
		testFile := filepath.Join(tmpDir, ".check_probe")
		if err := os.WriteFile(testFile, []byte("ok"), 0600); err != nil {
			printFail("磁盘权限", fmt.Sprintf("工作区写入权限探测失败: %v", err))
		} else {
			_ = os.Remove(testFile)
			printPass("磁盘权限", "工作区读写权限正常 (tmp/ 目录可写入)")
		}
	}

	// 2. 检查主配置文件 config.json
	fmt.Println("\n2. 主配置文件校验 (config.json):")
	configPath := filepath.Join(ws, config.ConfigFileName)
	var cfg *config.Config
	if _, err := os.Stat(configPath); err != nil {
		printFail("配置文件", fmt.Sprintf("未找到主配置文件: %s (提示: 若仅执行货物下船还原 land，无需此文件，可直接运行 ark land)", configPath))
	} else {
		cfgData, readErr := os.ReadFile(configPath)
		if readErr != nil {
			printFail("读取配置", fmt.Sprintf("读取配置文件失败: %v", readErr))
		} else {
			var rawMap map[string]interface{}
			if jsonErr := json.Unmarshal(cfgData, &rawMap); jsonErr != nil {
				printFail("JSON语法", fmt.Sprintf("config.json 格式错误: %v", jsonErr))
			} else {
				printPass("JSON语法", "config.json 语法有效，解析成功")
			}

			loadedCfg, _, loadErr := LoadAppConfig(ws, cliRepo)
			if loadErr != nil {
				printFail("解析配置", fmt.Sprintf("加载配置失败: %v", loadErr))
			} else {
				cfg = loadedCfg
				// 校验 repository
				if strings.TrimSpace(cfg.Repository) == "" {
					printFail("镜像港位", "未配置目标镜像仓库 (repository 为空)")
				} else {
					printPass("镜像港位", fmt.Sprintf("目标仓库: %s", cfg.Repository))
				}

				// 校验 category
				if strings.TrimSpace(cfg.Category) == "" {
					printWarn("航次分类", "未显式指定分类 (category)，默认回退至 'vps'")
				} else {
					printPass("航次分类", fmt.Sprintf("默认航次分类: %s", cfg.Category))
				}

				// 校验 retention_count
				if cfg.RetentionCount <= 0 {
					printWarn("配额保留", fmt.Sprintf("保留数配置为 %d，建议设置为 >= 1", cfg.RetentionCount))
				} else {
					printPass("配额保留", fmt.Sprintf("历史航次保留配额: %d 个", cfg.RetentionCount))
				}

				// 校验加密开关
				if cfg.Encrypt {
					printPass("安全封条", "已启用 AES-256 安全密闭加密 (encrypt: true)")
				} else {
					printWarn("安全封条", "未启用加密封条 (encrypt: false)，备份将以明文快照流传输")
				}
			}
		}
	}

	// 3. 检查货舱舱位数据源 (Sources)
	fmt.Println("\n3. 货舱舱位源审计 (Cargo Sources Audit):")
	activeSources := make([]config.Source, 0)
	if cfg != nil && len(cfg.Sources) > 0 {
		activeSources = cfg.Sources
	} else {
		// 尝试读取来自 .env 的动态配置
		if envSources := resolveBackupSourcesFromEnv(ws); len(envSources) > 0 {
			activeSources = envSources
			printPass("环境源", "已检测到 .env 中配置的备份数据源 (ARK_BACKUP_DIR)，免 config.json 即可直接运行")
		}
	}

	if len(activeSources) == 0 {
		if cfg == nil {
			printWarn("货舱清单", "未找到 config.json 且 .env 未配置 ARK_BACKUP_DIR (提示: 可在 .env 中设置 ARK_BACKUP_DIR 或运行 'ark scan')")
		} else {
			printWarn("货舱清单", "尚未配置任何 sources 货舱！可在 .env 中设置 ARK_BACKUP_DIR 或使用 'ark scan <目录>' 自动生成")
		}
	} else {
		seenIDs := make(map[string]bool)
		for i, src := range activeSources {
			tagPrefix := fmt.Sprintf("源#%d[%s]", i+1, src.ID)
			if src.ID == "" {
				printFail(tagPrefix, "货舱 ID 为空，请配置唯一英文标识符")
			} else if seenIDs[src.ID] {
				printFail(tagPrefix, fmt.Sprintf("货舱 ID 重复: '%s'，ID 必须保持全局唯一", src.ID))
			} else {
				seenIDs[src.ID] = true
			}

			if src.Path == "" {
				printFail(tagPrefix, "舱位路径 (path) 为空")
				continue
			}

			absPath := src.Path
			if !filepath.IsAbs(absPath) {
				absPath = filepath.Join(ws, absPath)
			}

			stat, err := os.Stat(absPath)
			if err != nil {
				printFail(tagPrefix, fmt.Sprintf("物理路径不存在或无权访问: %s", absPath))
			} else {
				fileCount := 0
				var totalSize int64
				_ = filepath.WalkDir(absPath, func(path string, d fs.DirEntry, err error) error {
					if err == nil && !d.IsDir() {
						fileCount++
						if info, err := d.Info(); err == nil {
							totalSize += info.Size()
						}
					}
					return nil
				})

				typeStr := "目录"
				if !stat.IsDir() {
					typeStr = "单文件"
				}
				sizeMB := float64(totalSize) / 1024 / 1024
				printPass(tagPrefix, fmt.Sprintf("物理存在 (%s) | 优先级:%d | 包含约 %d 项 (%.2f MB) -> %s",
					typeStr, src.Priority, fileCount, sizeMB, src.Path))
			}
		}
	}

	// 4. 检查安全封条密钥与闭环加密自检
	fmt.Println("\n4. 安全封条与加密链路自检 (Security & Seal Key):")
	if cfg != nil && cfg.Encrypt {
		keySource := "未检测到"
		if cliKey != "" {
			keySource = "命令行 --key 参数 (已确定性派生工作密文)"
		} else if envKey := getSealPassphraseFromEnv(ws); envKey != "" {
			keySource = "配置文件 .env / 环境变量中的 ARK_SEAL_KEY (已确定性派生工作密文)"
			printPass("存储安全", "已自动通过 ARK_SEAL_KEY 派生密文并同步至本地 keys/seal.key，全流程免输密码")
		} else if _, p := findPersistedKey(ws); p != "" {
			keySource = fmt.Sprintf("本地非明文密钥文件 (%s)", p)
			printPass("存储安全", "密钥采用 [非明文加密密文] 安全落盘，无明文泄露风险，全流程免输密码")
		}

		passphrase, err := resolveSealKey(ws, false, cliKey)
		if err != nil {
			printFail("封条密钥", fmt.Sprintf("密钥未装配: %v (可运行 'ark keygen <口令>' 生成非明文密文)", err))
		} else {
			printPass("封条密钥", fmt.Sprintf("已装载有效工作密文 (来源: %s)", keySource))

			// 端到端闭环加密解密自测
			dummyPlain := make([]byte, 1024)
			_, _ = rand.Read(dummyPlain)
			origSHA := sha256.Sum256(dummyPlain)

			dummySrc := filepath.Join(tmpDir, ".seal_test_src.tmp")
			dummyEnc := filepath.Join(tmpDir, ".seal_test_enc.tmp")
			dummyDec := filepath.Join(tmpDir, ".seal_test_dec.tmp")
			defer func() {
				_ = os.Remove(dummySrc)
				_ = os.Remove(dummyEnc)
				_ = os.Remove(dummyDec)
			}()

			_ = os.WriteFile(dummySrc, dummyPlain, 0600)
			if sealErr := archive.SealFile(dummySrc, dummyEnc, passphrase); sealErr != nil {
				printFail("加密自测", fmt.Sprintf("AES-256 流式加密测试失败: %v", sealErr))
			} else if unsealErr := archive.UnsealFile(dummyEnc, dummyDec, passphrase); unsealErr != nil {
				printFail("解密自测", fmt.Sprintf("流式解密封还原自测失败: %v", unsealErr))
			} else {
				decData, _ := os.ReadFile(dummyDec)
				decSHA := sha256.Sum256(decData)
				if origSHA == decSHA {
					printPass("加密闭环", "AES-256-CBC + PBKDF2 密闭封装与解封往返测试 100% 校验通过！")
				} else {
					printFail("加密校验", "解密后数据 SHA-256 指纹不匹配！")
				}
			}
		}
	} else {
		printPass("安全封条", "当前配置未开启加密，无需装载封条密钥")
	}

	// 5. 检查交付引擎与多通道云端港位鉴权连通性
	fmt.Println("\n5. 交付引擎与多通道云端港位鉴权体检 (Engine & Registry Targets):")
	printPass("交付引擎", "纯 Go 原生 OCI 流式直推引擎已就绪 (Zero-Docker Pipeline, 零 Docker 宿主机依赖)")

	defaultRepo := ""
	if cfg != nil {
		defaultRepo = cfg.Repository
	}
	targets := resolveRegistryTargets(ws, cliTarget, cliRepo, defaultRepo)
	if len(targets) == 0 {
		printFail("云端港位", "未识别到任何有效的远端镜像仓库 (请在 .env 中配置 ALIYUN_REPOSITORY 或 GITHUB_REPOSITORY)")
	} else {
		for _, target := range targets {
			targetTitle := fmt.Sprintf("港位[%s]", target.DisplayName)
			printPass(targetTitle, fmt.Sprintf("目标仓库: %s", target.Repository))

			// 调用 Target 的 RegistryProvider 进行标准化自检
			provider := target.Provider()
			probeCtx, probeCancel := context.WithTimeout(context.Background(), 8*time.Second)
			if err := provider.Check(probeCtx); err != nil {
				printFail(targetTitle, fmt.Sprintf("远端港位握手/鉴权未通过: %v (请核对账号密码或网络连通性)", err))
			} else {
				printPass(targetTitle, fmt.Sprintf("通行凭据有效: 用户 [%s] | 密码/Token [%s]", provider.MaskedUsername(), provider.MaskedPassword()))
				printPass(targetTitle, "远端港位握手成功，通行认证校验有效 (Bearer/Basic 协商成功)！")
			}
			probeCancel()
		}
	}

	// 6. 体检总结
	fmt.Println("\n================================================================")
	fmt.Printf("📋 体检结果汇总: ✓ 校验通过: %d 项 | ⚠ 潜在提示: %d 项 | ✗ 异常错误: %d 项\n", passCount, warnCount, failCount)
	fmt.Println("================================================================")

	if failCount == 0 {
		fmt.Println("✓ 恭喜！Ark 核心航运配置与加密引擎健康健全，随时可执行登船装载:")
		fmt.Println("    ark board")
	} else {
		fmt.Printf("[-] 检测到 %d 处阻碍航运的配置错误，请根据上方标注为 ✗ 的条目进行修复后再试。\n", failCount)
		os.Exit(1)
	}
}
