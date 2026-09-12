package archive

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestStreamPackAndUnpack(t *testing.T) {
	tempDir := t.TempDir()
	srcDir := filepath.Join(tempDir, "src")
	if err := os.MkdirAll(filepath.Join(srcDir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}

	file1 := filepath.Join(srcDir, "file1.txt")
	file2 := filepath.Join(srcDir, "sub", "file2.json")
	content1 := []byte("Hello, world! This is a test file for ark stream compression.")
	content2 := []byte(`{"message": "ark voyage stream backup", "data": [1,2,3,4,5]}`)

	if err := os.WriteFile(file1, content1, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file2, content2, 0600); err != nil {
		t.Fatal(err)
	}

	passphrase := []byte("SuperSecurePassphrase2026!#")
	datPath := filepath.Join(tempDir, "cargo.dat")

	// 1. Pack and Seal stream (内存流式 打包 -> gzip -> aes)
	if err := PackAndSealStream(srcDir, datPath, passphrase); err != nil {
		t.Fatalf("PackAndSealStream failed: %v", err)
	}

	// 验证 dat 文件存在且非空
	fi, err := os.Stat(datPath)
	if err != nil || fi.Size() == 0 {
		t.Fatalf("dat file missing or empty: %v", err)
	}

	// 2. Unseal and Unpack stream (内存流式 aes -> gzip -> unpack)
	restoreDir := filepath.Join(tempDir, "restore")
	if err := UnsealAndUnpackStream(datPath, restoreDir, passphrase); err != nil {
		t.Fatalf("UnsealAndUnpackStream failed: %v", err)
	}

	// 3. 验证还原内容
	restored1, err := os.ReadFile(filepath.Join(restoreDir, "file1.txt"))
	if err != nil {
		t.Fatalf("file1 not restored: %v", err)
	}
	if !bytes.Equal(restored1, content1) {
		t.Fatalf("file1 content mismatch: got %s, want %s", restored1, content1)
	}

	restored2, err := os.ReadFile(filepath.Join(restoreDir, "sub", "file2.json"))
	if err != nil {
		t.Fatalf("file2 not restored: %v", err)
	}
	if !bytes.Equal(restored2, content2) {
		t.Fatalf("file2 content mismatch: got %s, want %s", restored2, content2)
	}
}

func TestStreamBackwardCompatibilityWithRawTar(t *testing.T) {
	tempDir := t.TempDir()
	srcDir := filepath.Join(tempDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	file1 := filepath.Join(srcDir, "old.txt")
	content1 := []byte("Old raw tar backward compatibility test")
	if err := os.WriteFile(file1, content1, 0644); err != nil {
		t.Fatal(err)
	}

	// 旧流程：未压缩 PackTar -> SealFile
	oldTar := filepath.Join(tempDir, "old.tar")
	if err := PackTar(srcDir, oldTar); err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("OldPassword123!")
	oldDat := filepath.Join(tempDir, "old.dat")
	if err := SealFile(oldTar, oldDat, passphrase); err != nil {
		t.Fatal(err)
	}

	// 新版 UnsealAndUnpackStream 必须能无缝解密解压旧版未压缩 raw tar！
	restoreDir := filepath.Join(tempDir, "restore_old")
	if err := UnsealAndUnpackStream(oldDat, restoreDir, passphrase); err != nil {
		t.Fatalf("UnsealAndUnpackStream failed to restore old raw tar: %v", err)
	}

	restored1, err := os.ReadFile(filepath.Join(restoreDir, "old.txt"))
	if err != nil {
		t.Fatalf("restored old.txt failed: %v", err)
	}
	if !bytes.Equal(restored1, content1) {
		t.Fatalf("content mismatch: got %s, want %s", restored1, content1)
	}
}
