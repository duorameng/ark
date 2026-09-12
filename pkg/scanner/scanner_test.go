package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"ark/pkg/config"
)

func TestScanRootWithFilesAndDirs(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ark_scan_test_*")
	if err != nil {
		t.Fatalf("create temp dir failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 1. 创建子目录 (包括一个就叫 root_files 的真实用户子目录，用于测试防冲突！)
	subDir1 := filepath.Join(tempDir, "service_a")
	subDir2 := filepath.Join(tempDir, "root_files") // 用户工程本身的 root_files 目录
	_ = os.MkdirAll(subDir1, 0755)
	_ = os.MkdirAll(subDir2, 0755)
	_ = os.WriteFile(filepath.Join(subDir1, "data.txt"), []byte("sub1"), 0644)
	_ = os.WriteFile(filepath.Join(subDir2, "conf.yaml"), []byte("nested"), 0644)

	// 2. 创建根级同级文件 (配置文件/脚本)
	_ = os.WriteFile(filepath.Join(tempDir, "docker-compose.yml"), []byte("version: '3'"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, ".env"), []byte("PORT=8080"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "Makefile"), []byte("all:"), 0644)

	results, err := ScanRoot(tempDir)
	if err != nil {
		t.Fatalf("ScanRoot error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 sources (1 root files cargo + 2 subdirs), got: %d", len(results))
	}

	// 3. 根文件归集舱位排在第一位 (Priority: 10)
	first := results[0]
	if first.Source.ID != config.DefaultRootFilesID {
		t.Errorf("expected first source ID to be %s, got: %s", config.DefaultRootFilesID, first.Source.ID)
	}
	if !first.Source.FilesOnly {
		t.Errorf("expected root files cargo FilesOnly to be true")
	}
	if !first.Source.IsRootFiles() {
		t.Errorf("expected IsRootFiles() to be true")
	}
	if first.FileCount != 3 {
		t.Errorf("expected 3 root files, got: %d", first.FileCount)
	}

	// 4. 关键验证：用户的 root_files 普通文件夹不能被识别为 IsRootFiles
	var userFolder *ScanResult
	for _, r := range results {
		if r.Source.Name == "root_files" {
			userFolder = &r
			break
		}
	}
	if userFolder == nil {
		t.Fatalf("user subfolder 'root_files' was not detected")
	}
	if userFolder.Source.IsRootFiles() {
		t.Errorf("user subfolder 'root_files' should NOT be identified as IsRootFiles")
	}
	if userFolder.Source.FilesOnly {
		t.Errorf("user subfolder 'root_files' should NOT have FilesOnly=true")
	}
}

func TestScanConflictWithInternalID(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ark_scan_conflict_*")
	if err != nil {
		t.Fatalf("create temp dir failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 创建与专属 UUID ID 同名的真实子目录 (测试极端防碰撞)
	conflictDir := filepath.Join(tempDir, config.DefaultRootFilesID)
	_ = os.MkdirAll(conflictDir, 0755)
	_ = os.WriteFile(filepath.Join(conflictDir, "app.py"), []byte("print(1)"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "root.txt"), []byte("root"), 0644)

	results, err := ScanRoot(tempDir)
	if err != nil {
		t.Fatalf("ScanRoot error: %v", err)
	}

	// 两个源的 ID 绝不能相同
	if len(results) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(results))
	}
	if results[0].Source.ID == results[1].Source.ID {
		t.Fatalf("collision detected! both sources have ID: %s", results[0].Source.ID)
	}
}
