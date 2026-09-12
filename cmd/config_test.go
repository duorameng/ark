package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"ark/pkg/config"
)

func TestLoadAppConfigWithoutFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ark_cfg_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 在完全没有 config.json 的空目录下调用 LoadAppConfig
	cfg, hasFile, err := LoadAppConfig(tempDir, "ghcr.io/test/override")
	if err == nil {
		t.Errorf("expected error when config.json does not exist, got nil")
	}
	if hasFile {
		t.Errorf("expected hasFile=false when config.json does not exist")
	}
	if cfg == nil {
		t.Fatalf("expected non-nil default cfg for disaster recovery")
	}
	if cfg.Repository != "ghcr.io/test/override" {
		t.Errorf("expected repository ghcr.io/test/override, got: %s", cfg.Repository)
	}
	if cfg.Category == "" {
		t.Errorf("expected non-empty default category")
	}
}

func TestLoadAppConfigWithFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ark_cfg_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	configContent := `{
		"repository": "ghcr.io/mock/repo",
		"category": "mycat",
		"retention_count": 8,
		"encrypt": true
	}`
	_ = os.WriteFile(filepath.Join(tempDir, "config.json"), []byte(configContent), 0644)

	cfg, hasFile, err := LoadAppConfig(tempDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasFile {
		t.Errorf("expected hasFile=true")
	}
	if cfg.Repository != "ghcr.io/mock/repo" {
		t.Errorf("expected repository ghcr.io/mock/repo, got: %s", cfg.Repository)
	}
	if cfg.Category != "mycat" {
		t.Errorf("expected category mycat, got: %s", cfg.Category)
	}
	if cfg.RetentionCount != 8 {
		t.Errorf("expected retention_count 8, got: %d", cfg.RetentionCount)
	}

	// 测试带 cliRepo 覆盖
	cfgWithOverride, _, _ := LoadAppConfig(tempDir, "ghcr.io/override/pkg")
	if cfgWithOverride.Repository != "ghcr.io/override/pkg" {
		t.Errorf("expected overridden repository, got: %s", cfgWithOverride.Repository)
	}
}

func TestExtractFlags(t *testing.T) {
	args := []string{"land", "vps", "--repo", "ghcr.io/a/b", "-k", "pass123", "--engine=docker", "dest"}
	cleaned, repo := extractRepoFlag(args)
	if repo != "ghcr.io/a/b" {
		t.Errorf("expected repo ghcr.io/a/b, got: %s", repo)
	}
	cleaned, key := extractKeyFlag(cleaned)
	if key != "pass123" {
		t.Errorf("expected key pass123, got: %s", key)
	}
	cleaned, engine := extractEngineFlag(cleaned)
	if engine != "docker" {
		t.Errorf("expected engine docker, got: %s", engine)
	}
	expectedArgs := []string{"land", "vps", "dest"}
	if len(cleaned) != len(expectedArgs) {
		t.Fatalf("cleaned args length mismatch: got %v", cleaned)
	}
	for i, v := range cleaned {
		if v != expectedArgs[i] {
			t.Errorf("arg[%d] = %s; want %s", i, v, expectedArgs[i])
		}
	}
}

func TestBackupDirFromEnv(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ark_backup_env_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 1. 创建模拟的备份目录，包含子目录和文件
	mockBackupDir := filepath.Join(tempDir, "my_backup_target")
	_ = os.MkdirAll(filepath.Join(mockBackupDir, "db"), 0755)
	_ = os.WriteFile(filepath.Join(mockBackupDir, "db", "data.db"), []byte("db"), 0644)
	_ = os.WriteFile(filepath.Join(mockBackupDir, "docker-compose.yml"), []byte("version: '3'"), 0644)

	// 2. 写入 .env 文件
	envContent := "ARK_BACKUP_DIR=" + mockBackupDir + "\n"
	_ = os.WriteFile(filepath.Join(tempDir, ".env"), []byte(envContent), 0644)

	// 3. 在无 config.json 情况下加载配置
	cfg, _, err := LoadAppConfig(tempDir, "")
	if err == nil {
		t.Errorf("expected non-nil error indicating no physical config.json")
	}
	if len(cfg.Sources) == 0 {
		t.Fatalf("expected sources to be populated automatically from ARK_BACKUP_DIR")
	}

	// 4. 验证是否包含根文件与子模块
	hasRootFiles := false
	hasDB := false
	for _, s := range cfg.Sources {
		if s.IsRootFiles() {
			hasRootFiles = true
		}
		if s.ID == "db" {
			hasDB = true
		}
	}
	if !hasRootFiles {
		t.Errorf("expected root_files to be detected from ARK_BACKUP_DIR")
	}
	if !hasDB {
		t.Errorf("expected db subdir to be detected from ARK_BACKUP_DIR")
	}

	// 5. 验证 resolveBackupDir
	resolved := resolveBackupDir(tempDir, "")
	if resolved != mockBackupDir {
		t.Errorf("expected resolved backup dir %s, got %s", mockBackupDir, resolved)
	}
}

func TestRetentionCountFromEnv(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ark_retention_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 写入 .env 文件包含 ARK_RETENTION_COUNT=9
	envContent := "ARK_RETENTION_COUNT=9\n"
	_ = os.WriteFile(filepath.Join(tempDir, ".env"), []byte(envContent), 0644)

	cfg, _, _ := LoadAppConfig(tempDir, "")
	if cfg.RetentionCount != 9 {
		t.Errorf("expected retention_count 9 from .env, got: %d", cfg.RetentionCount)
	}
}

func TestParseBoardFlagsKeep(t *testing.T) {
	cfg := &config.Config{
		RetentionCount: 5,
	}
	_, _, _, _, _, _ = parseBoardFlags(cfg, []string{"day", "--keep", "7"})
	if cfg.RetentionCount != 7 {
		t.Errorf("expected RetentionCount=7 after --keep 7, got %d", cfg.RetentionCount)
	}

	_, _, _, _, _, _ = parseBoardFlags(cfg, []string{"day", "--retention=12"})
	if cfg.RetentionCount != 12 {
		t.Errorf("expected RetentionCount=12 after --retention=12, got %d", cfg.RetentionCount)
	}
}

func TestResolveRegistryUserAndToken(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ark_user_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 1. 测试从环境变量读取 ARK_USERNAME
	_ = os.Setenv(config.EnvArkUsername, "aliyun_user123")
	user := resolveRegistryUser("registry.cn-hangzhou.aliyuncs.com/myvps/ark")
	if user != "aliyun_user123" {
		t.Errorf("expected aliyun_user123, got: %s", user)
	}
	_ = os.Unsetenv(config.EnvArkUsername)

	// 2. 测试默认从仓库路径第二段提取
	userFallback := resolveRegistryUser("registry.cn-hangzhou.aliyuncs.com/myvps/ark")
	if userFallback != "myvps" {
		t.Errorf("expected myvps, got: %s", userFallback)
	}

	// 3. 测试从 .env 读取 ARK_PASSWORD 作为 token
	envContent := "ARK_PASSWORD=secret_pass_888\n"
	_ = os.WriteFile(filepath.Join(tempDir, ".env"), []byte(envContent), 0644)
	tok := loadToken(tempDir)
	if tok != "secret_pass_888" {
		t.Errorf("expected secret_pass_888, got: %s", tok)
	}
}

func TestExtractTargetFlag(t *testing.T) {
	// 测试 --to ali
	args1 := []string{"day", "--to", "ali", "--clean-all"}
	cleaned1, target1 := extractTargetFlag(args1)
	if target1 != "ali" {
		t.Errorf("expected target 'ali', got: %s", target1)
	}
	if len(cleaned1) != 2 || cleaned1[0] != "day" || cleaned1[1] != "--clean-all" {
		t.Errorf("cleaned args mismatch: %v", cleaned1)
	}

	// 测试独立位置别名 both
	args2 := []string{"both", "day"}
	cleaned2, target2 := extractTargetFlag(args2)
	if target2 != "both" {
		t.Errorf("expected target 'both', got: %s", target2)
	}
	if len(cleaned2) != 1 || cleaned2[0] != "day" {
		t.Errorf("cleaned args mismatch: %v", cleaned2)
	}

	// 测试 --both
	args3 := []string{"day", "--both"}
	_, target3 := extractTargetFlag(args3)
	if target3 != "both" {
		t.Errorf("expected target 'both' from --both, got: %s", target3)
	}
}

func TestResolveRegistryTargets(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ark_targets_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	envContent := `
ALIYUN_REPOSITORY="registry.cn-hangzhou.aliyuncs.com/test/ark"
ALIYUN_USERNAME="ali_user"
ALIYUN_PASSWORD="ali_password"
GITHUB_REPOSITORY="ghcr.io/duorameng/ark"
GH_TOKEN="ghp_test_token"
`
	_ = os.WriteFile(filepath.Join(tempDir, ".env"), []byte(envContent), 0644)

	// 1. 测试单推 aliyun
	targetsAli := resolveRegistryTargets(tempDir, "ali", "", "")
	if len(targetsAli) != 1 {
		t.Fatalf("expected 1 target for ali, got %d", len(targetsAli))
	}
	if targetsAli[0].Key != "aliyun" || targetsAli[0].Repository != "registry.cn-hangzhou.aliyuncs.com/test/ark" {
		t.Errorf("unexpected aliyun target: %+v", targetsAli[0])
	}
	if targetsAli[0].Username != "ali_user" || targetsAli[0].Password != "ali_password" {
		t.Errorf("aliyun credentials mismatch: %+v", targetsAli[0])
	}

	// 2. 测试单推 github
	targetsGH := resolveRegistryTargets(tempDir, "gh", "", "")
	if len(targetsGH) != 1 {
		t.Fatalf("expected 1 target for gh, got %d", len(targetsGH))
	}
	if targetsGH[0].Key != "github" || targetsGH[0].Repository != "ghcr.io/duorameng/ark" {
		t.Errorf("unexpected github target: %+v", targetsGH[0])
	}
	if targetsGH[0].Password != "ghp_test_token" {
		t.Errorf("github token mismatch: %+v", targetsGH[0])
	}

	// 3. 测试双推 both
	targetsBoth := resolveRegistryTargets(tempDir, "both", "", "")
	if len(targetsBoth) != 2 {
		t.Fatalf("expected 2 targets for both, got %d", len(targetsBoth))
	}
	if targetsBoth[0].Key != "aliyun" || targetsBoth[1].Key != "github" {
		t.Errorf("unexpected both targets order/keys: %+v", targetsBoth)
	}
}

func TestFixedTagParsing(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ark_fixed_tag_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 1. 测试从 .env 加载 ARK_TAG
	envContent := "ARK_TAG=latest\nARK_REPOSITORY=ghcr.io/test/ark\n"
	_ = os.WriteFile(filepath.Join(tempDir, ".env"), []byte(envContent), 0644)

	cfg, _, err := LoadAppConfig(tempDir, "")
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("LoadAppConfig failed: %v", err)
	}
	if cfg.FixedTag != "latest" {
		t.Errorf("expected cfg.FixedTag to be 'latest', got '%s'", cfg.FixedTag)
	}

	// 2. 当 cfg.FixedTag 为 latest 且命令行未传参时，parseBoardFlags 返回 latest 和 fixed
	tag, _, precision, _, _, _ := parseBoardFlags(cfg, []string{})
	if tag != "latest" || precision != "fixed" {
		t.Errorf("expected tag 'latest' and precision 'fixed', got tag '%s', precision '%s'", tag, precision)
	}

	// 3. 基础配置未设置 FixedTag 时，命令行 --latest 标志
	cfgEmpty := &config.Config{
		Category:       "vps",
		TagPrecision:   "second",
		RetentionCount: 5,
	}
	tag1, _, prec1, _, _, _ := parseBoardFlags(cfgEmpty, []string{"--latest"})
	if tag1 != "latest" || prec1 != "fixed" {
		t.Errorf("expected tag 'latest' from --latest, got '%s', prec '%s'", tag1, prec1)
	}

	// 4. 命令行 -l 简写
	tag2, _, prec2, _, _, _ := parseBoardFlags(cfgEmpty, []string{"-l"})
	if tag2 != "latest" || prec2 != "fixed" {
		t.Errorf("expected tag 'latest' from -l, got '%s', prec '%s'", tag2, prec2)
	}

	// 5. 命令行位置参数 latest
	tag3, _, prec3, _, _, _ := parseBoardFlags(cfgEmpty, []string{"latest"})
	if tag3 != "latest" || prec3 != "fixed" {
		t.Errorf("expected tag 'latest' from positional 'latest', got '%s', prec '%s'", tag3, prec3)
	}
}




