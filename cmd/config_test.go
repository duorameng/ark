package cmd

import (
	"os"
	"path/filepath"
	"testing"
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
