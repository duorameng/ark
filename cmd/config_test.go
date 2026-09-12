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


