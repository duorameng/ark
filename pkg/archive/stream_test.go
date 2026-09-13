package archive

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
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

func TestFilesOnlyPackAndUnpack(t *testing.T) {
	tempDir := t.TempDir()
	srcDir := filepath.Join(tempDir, "src")
	_ = os.MkdirAll(filepath.Join(srcDir, "sub_service"), 0755)

	confContent := []byte("version: '3.8'\nservices:\n  app:\n    image: myapp\n")
	envContent := []byte("DB_HOST=localhost\nDB_PORT=5432\n")
	_ = os.WriteFile(filepath.Join(srcDir, "docker-compose.yml"), confContent, 0644)
	_ = os.WriteFile(filepath.Join(srcDir, ".env"), envContent, 0644)
	_ = os.WriteFile(filepath.Join(srcDir, "sub_service", "sub_file.txt"), []byte("should be ignored"), 0644)

	passphrase := []byte("FilesOnlySecret2026")
	datPath := filepath.Join(tempDir, "root_files.dat")

	// 1. FilesOnly 打包加密
	if err := PackAndSealSourceStream(srcDir, datPath, passphrase, true); err != nil {
		t.Fatalf("PackAndSealSourceStream failed: %v", err)
	}

	// 2. 原位解包还原到 restoreDir
	restoreDir := filepath.Join(tempDir, "restore")
	if err := UnsealAndUnpackStream(datPath, restoreDir, passphrase); err != nil {
		t.Fatalf("UnsealAndUnpackStream failed: %v", err)
	}

	// 3. 验证同级文件存在
	gotConf, err := os.ReadFile(filepath.Join(restoreDir, "docker-compose.yml"))
	if err != nil || !bytes.Equal(gotConf, confContent) {
		t.Fatalf("docker-compose.yml mismatch or missing: %v", err)
	}
	gotEnv, err := os.ReadFile(filepath.Join(restoreDir, ".env"))
	if err != nil || !bytes.Equal(gotEnv, envContent) {
		t.Fatalf(".env mismatch or missing: %v", err)
	}

	// 4. 验证子目录没有被重复打包到 root_files
	if _, err := os.Stat(filepath.Join(restoreDir, "sub_service")); err == nil {
		t.Errorf("expected sub_service to NOT exist in files_only bundle")
	}
}

func TestPackAndSealWithProgress(t *testing.T) {
	tempDir := t.TempDir()
	srcDir := filepath.Join(tempDir, "src")
	_ = os.MkdirAll(srcDir, 0755)

	content := make([]byte, 500*1024) // 500KB
	for i := range content {
		content[i] = byte(i % 256)
	}
	_ = os.WriteFile(filepath.Join(srcDir, "test.dat"), content, 0644)

	var reported int64
	callbackCount := 0
	progressCb := func(processed int64) {
		reported = processed
		callbackCount++
	}

	passphrase := []byte("ProgressTest2026")
	datPath := filepath.Join(tempDir, "sealed.dat")

	if err := PackAndSealSourceStreamWithProgress(srcDir, datPath, passphrase, false, progressCb); err != nil {
		t.Fatalf("PackAndSealSourceStreamWithProgress failed: %v", err)
	}

	if callbackCount == 0 || reported < int64(len(content)) {
		t.Errorf("expected progress callback to report >= %d bytes, got %d (callbacks: %d)", len(content), reported, callbackCount)
	}

	unpackDir := filepath.Join(tempDir, "unpacked")
	var unpackReported int64
	unpackCbCount := 0
	unpackProgressCb := func(processed int64) {
		unpackReported = processed
		unpackCbCount++
	}

	if err := UnsealAndUnpackStreamWithProgress(datPath, unpackDir, passphrase, unpackProgressCb); err != nil {
		t.Fatalf("UnsealAndUnpackStreamWithProgress failed: %v", err)
	}

	if unpackCbCount == 0 || unpackReported <= 0 {
		t.Errorf("expected unseal progress callback to report > 0 bytes, got %d (callbacks: %d)", unpackReported, unpackCbCount)
	}

	got, err := os.ReadFile(filepath.Join(unpackDir, "test.dat"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("content corrupted after progress-tracked pack/unpack")
	}
}

func TestUnpackTarWithProgress(t *testing.T) {
	tempDir := t.TempDir()
	srcDir := filepath.Join(tempDir, "src")
	_ = os.MkdirAll(srcDir, 0755)

	content := []byte("Hello UnpackTarWithProgress Test Data 2026")
	_ = os.WriteFile(filepath.Join(srcDir, "hello.txt"), content, 0644)

	tarGzPath := filepath.Join(tempDir, "archive.tar.gz")
	if err := PackTarGz(srcDir, tarGzPath); err != nil {
		t.Fatalf("PackTarGz failed: %v", err)
	}

	destDir := filepath.Join(tempDir, "dest")
	var reported int64
	cbCount := 0
	if err := UnpackTarWithProgress(tarGzPath, destDir, func(processed int64) {
		reported = processed
		cbCount++
	}); err != nil {
		t.Fatalf("UnpackTarWithProgress failed: %v", err)
	}

	if cbCount == 0 || reported <= 0 {
		t.Errorf("expected unpack progress callback to fire, got %d bytes, %d calls", reported, cbCount)
	}

	got, err := os.ReadFile(filepath.Join(destDir, "hello.txt"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("content mismatch in UnpackTarWithProgress")
	}
}

func TestWriteTooLongProtection(t *testing.T) {
	// 测试当底层数据流大于声明的 targetSize 时 (模拟 MySQL 在打包瞬间追加数据)，copyFileToTar 绝不报错
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	targetSize := int64(100)
	hdr := &tar.Header{
		Name:     "mysql/ibdata1",
		Mode:     0644,
		Size:     targetSize,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("WriteHeader failed: %v", err)
	}

	// 准备 500 字节数据 (远大于 targetSize 100 字节)
	overflowData := bytes.Repeat([]byte("A"), 500)
	r := bytes.NewReader(overflowData)
	copyBuf := make([]byte, 1024)

	// 原生如果不加 LimitReader 直接 CopyBuffer 会触发 "archive/tar: write too long"
	if err := copyFileToTar(tw, r, targetSize, copyBuf); err != nil {
		t.Fatalf("copyFileToTar should NOT return error when file grows dynamically: %v", err)
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tw.Close failed: %v", err)
	}

	// 验证解包后刚好是 100 字节
	tr := tar.NewReader(&buf)
	h, err := tr.Next()
	if err != nil {
		t.Fatalf("tr.Next failed: %v", err)
	}
	if h.Size != targetSize {
		t.Fatalf("expected size %d, got %d", targetSize, h.Size)
	}
	readBack, _ := io.ReadAll(tr)
	if int64(len(readBack)) != targetSize {
		t.Fatalf("expected %d bytes, got %d", targetSize, len(readBack))
	}
}

func BenchmarkPackAndSealStream(b *testing.B) {
	tempDir := b.TempDir()
	srcDir := filepath.Join(tempDir, "src")
	_ = os.MkdirAll(srcDir, 0755)

	// 生成 300 个 100KB 伪随机数据文件 (共 30MB)
	chunk := make([]byte, 100*1024)
	for i := range chunk {
		chunk[i] = byte((i*31 + 17) % 256)
	}
	for i := 0; i < 300; i++ {
		chunk[0] = byte(i)
		chunk[1] = byte(i >> 8)
		_ = os.WriteFile(filepath.Join(srcDir, fmt.Sprintf("file_%04d.dat", i)), chunk, 0644)
	}

	passphrase := []byte("BenchmarkPassword2026")
	datPath := filepath.Join(tempDir, "out.dat")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = PackAndSealStream(srcDir, datPath, passphrase)
	}
}

func BenchmarkLargeMixedPackAndSeal(b *testing.B) {
	tempDir := b.TempDir()
	srcDir := filepath.Join(tempDir, "src")
	_ = os.MkdirAll(srcDir, 0755)

	// 1. 生成 25MB 高压缩率文本数据 (模拟 SQL/日志)
	textChunk := bytes.Repeat([]byte("INSERT INTO `large_table` VALUES (1001, 'Ark High Throughput Test Record 2026', NOW(), 'abcdefghijklmnopqrstuvwxyz');\n"), 250000)
	_ = os.WriteFile(filepath.Join(srcDir, "dump.sql"), textChunk, 0644)

	// 2. 生成 25MB 伪随机二进制文件 (模拟已压缩数据/媒体)
	binChunk := make([]byte, 25*1024*1024)
	for i := range binChunk {
		binChunk[i] = byte((i*37 + 19) % 256)
	}
	_ = os.WriteFile(filepath.Join(srcDir, "archive.bin"), binChunk, 0644)

	passphrase := []byte("LargeBenchmarkPass2026")
	datPath := filepath.Join(tempDir, "out_large.dat")

	b.ResetTimer()
	b.SetBytes(int64(len(textChunk) + len(binChunk)))
	for i := 0; i < b.N; i++ {
		_ = PackAndSealStream(srcDir, datPath, passphrase)
	}
}

