package hash

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestComputeSourceTreeHash(t *testing.T) {
	tempDir := t.TempDir()

	// 1. 创建子文件与嵌套目录
	_ = os.MkdirAll(filepath.Join(tempDir, "sub"), 0755)
	_ = os.WriteFile(filepath.Join(tempDir, "file1.txt"), []byte("hello world"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "sub", "file2.txt"), []byte("sub content"), 0644)

	info1, err := ComputeSourceTreeHash(tempDir, false)
	if err != nil {
		t.Fatalf("ComputeSourceTreeHash failed: %v", err)
	}
	if info1.FileCount != 2 {
		t.Errorf("expected 2 files, got %d", info1.FileCount)
	}

	// 2. 相同内容计算结果必须确定性一致
	info2, err := ComputeSourceTreeHash(tempDir, false)
	if err != nil || info1.Hash != info2.Hash {
		t.Fatalf("hashes should match for identical directory state")
	}

	// 3. 修改文件内容后，哈希必然变化
	time.Sleep(10 * time.Millisecond) // 确保 modTime 变动
	_ = os.WriteFile(filepath.Join(tempDir, "file1.txt"), []byte("hello modified world"), 0644)

	info3, err := ComputeSourceTreeHash(tempDir, false)
	if err != nil {
		t.Fatalf("ComputeSourceTreeHash failed after modification: %v", err)
	}
	if info3.Hash == info1.Hash {
		t.Fatalf("hash should change after file modification")
	}
}

func TestLargeFileFastSampleHash(t *testing.T) {
	tempDir := t.TempDir()
	largePath := filepath.Join(tempDir, "large_sample.dat")

	// 生成 5MB 大文件 (验证首尾采样与微秒级极速返回)
	data := bytes.Repeat([]byte("1234567890ABCDEF"), 320*1024) // 5MB
	_ = os.WriteFile(largePath, data, 0644)

	start := time.Now()
	info, err := ComputeSourceTreeHash(largePath, false)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("ComputeSourceTreeHash on large file failed: %v", err)
	}
	if info.FileCount != 1 || info.TotalBytes != int64(len(data)) {
		t.Fatalf("invalid info: count=%d, bytes=%d", info.FileCount, info.TotalBytes)
	}

	// 验证耗时远小于 100ms (应在 1~5ms 以内)
	if elapsed > 150*time.Millisecond {
		t.Errorf("large file hash took too long: %v (expected < 150ms)", elapsed)
	}
}

type mockFilter struct {
	ignored string
}

func (m *mockFilter) ShouldIgnore(fullPath, cargoRoot string, isDir bool) bool {
	return filepath.Base(fullPath) == m.ignored
}

func TestComputeSourceTreeHashWithFilter(t *testing.T) {
	tempDir := t.TempDir()

	_ = os.MkdirAll(filepath.Join(tempDir, "envs"), 0755)
	_ = os.WriteFile(filepath.Join(tempDir, "main.py"), []byte("main"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "envs", "lib.py"), []byte("lib"), 0644)

	filter := &mockFilter{ignored: "envs"}

	info1, err := ComputeSourceTreeHashWithFilter(tempDir, false, filter)
	if err != nil {
		t.Fatalf("ComputeSourceTreeHashWithFilter failed: %v", err)
	}
	if info1.FileCount != 1 {
		t.Fatalf("expected 1 file counted (envs ignored), got: %d", info1.FileCount)
	}

	// 在 envs 内部新增/变动文件，整体 Hash 绝不能变动！
	time.Sleep(10 * time.Millisecond)
	_ = os.WriteFile(filepath.Join(tempDir, "envs", "new.txt"), []byte("changed"), 0644)

	info2, err := ComputeSourceTreeHashWithFilter(tempDir, false, filter)
	if err != nil {
		t.Fatalf("ComputeSourceTreeHashWithFilter failed: %v", err)
	}
	if info1.Hash != info2.Hash {
		t.Fatalf("hash should remain identical when changes occur inside ignored envs/")
	}
}

