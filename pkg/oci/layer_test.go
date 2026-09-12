package oci

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestTarLayer(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test_file.dat")
	testData := []byte("hello oci layer test data with custom length to check padding!")
	if err := os.WriteFile(testFile, testData, 0644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}

	layer, err := NewTarLayer(testFile)
	if err != nil {
		t.Fatalf("NewTarLayer 失败: %v", err)
	}

	if layer.FileName != "test_file.dat" {
		t.Errorf("期望 FileName 为 test_file.dat, 实际为 %s", layer.FileName)
	}
	if layer.TargetCargo != "cargo/test_file.dat" {
		t.Errorf("期望 TargetCargo 为 cargo/test_file.dat, 实际为 %s", layer.TargetCargo)
	}

	digest, err := layer.ComputeDigest()
	if err != nil {
		t.Fatalf("ComputeDigest 失败: %v", err)
	}

	stream, cleanup, err := layer.OpenStream()
	if err != nil {
		t.Fatalf("OpenStream 失败: %v", err)
	}
	defer cleanup()

	// 读取整个 stream 并验证大小与 sha256
	var buf bytes.Buffer
	n, err := io.Copy(&buf, stream)
	if err != nil {
		t.Fatalf("读取 stream 失败: %v", err)
	}

	if n != layer.TotalSize {
		t.Errorf("读取字节数 %d 与 TotalSize %d 不符", n, layer.TotalSize)
	}

	expectedDigest := "sha256:" + hex.EncodeToString(func() []byte {
		h := sha256.Sum256(buf.Bytes())
		return h[:]
	}())
	if digest != expectedDigest {
		t.Errorf("计算的 Digest %s 与流校验和 %s 不一致", digest, expectedDigest)
	}

	// 用标准 tar.Reader 解包验证
	tr := tar.NewReader(&buf)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("tar.Reader.Next 失败: %v", err)
	}

	if hdr.Name != "cargo/test_file.dat" {
		t.Errorf("解包文件名期望 cargo/test_file.dat, 实际 %s", hdr.Name)
	}
	if hdr.Size != int64(len(testData)) {
		t.Errorf("解包大小期望 %d, 实际 %d", len(testData), hdr.Size)
	}

	content, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("读取解包数据失败: %v", err)
	}
	if !bytes.Equal(content, testData) {
		t.Errorf("解包内容与原始数据不一致")
	}

	// 确认之后没有更多文件
	_, err = tr.Next()
	if err != io.EOF {
		t.Errorf("期望 EOF, 实际为 %v", err)
	}
}
