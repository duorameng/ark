package oci

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// TarLayer 表示一个单文件 Tar 构成的 OCI / Docker 镜像层
type TarLayer struct {
	FilePath    string // 本地文件绝对路径 (如 cache/mock_static.dat)
	FileName    string // 文件名 (如 mock_static.dat)
	TargetCargo string // 容器内相对路径 (如 cargo/mock_static.dat)
	FileSize    int64  // 原始文件字节大小
	HeaderBytes []byte // 512 字节 Tar USTAR 头部
	PadSize     int64  // Tar 块 512 对齐填充大小
	TotalSize   int64  // 整个 Tar 流的精确总字节大小
	Digest      string // 计算出的 sha256:<hex> 指纹
}

// NewTarLayer 创建一个单文件 TarLayer，完成数学常数大小推导与 header 生成
func NewTarLayer(filePath string) (*TarLayer, error) {
	fi, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("无法读取层源文件 %s: %w", filePath, err)
	}

	baseName := filepath.Base(filePath)
	cargoPath := "cargo/" + baseName

	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)
	hdr := &tar.Header{
		Name:     cargoPath,
		Mode:     0644,
		Size:     fi.Size(),
		ModTime:  time.Unix(0, 0).UTC(),
		Format:   tar.FormatUSTAR,
		Typeflag: tar.TypeReg,
	}

	if err := tw.WriteHeader(hdr); err != nil {
		return nil, fmt.Errorf("生成 Tar 头部失败: %w", err)
	}
	_ = tw.Flush()

	headerBytes := buf.Bytes()
	if len(headerBytes) != 512 {
		return nil, fmt.Errorf("Tar USTAR 头部长度异常 (预期 512，实际 %d)", len(headerBytes))
	}

	fileSize := fi.Size()
	padSize := (512 - (fileSize % 512)) % 512
	// 512 字节 Header + 原始数据 + Pad 对齐 + 1024 字节 EOF
	totalSize := int64(len(headerBytes)) + fileSize + padSize + 1024

	return &TarLayer{
		FilePath:    filePath,
		FileName:    baseName,
		TargetCargo: cargoPath,
		FileSize:    fileSize,
		HeaderBytes: headerBytes,
		PadSize:     padSize,
		TotalSize:   totalSize,
	}, nil
}

// ComputeDigest 计算流式单文件 Tar 的完整 SHA-256 (零额外磁盘写入，单 buffer 流式计算)
func (l *TarLayer) ComputeDigest() (string, error) {
	if l.Digest != "" {
		return l.Digest, nil
	}

	stream, cleanup, err := l.OpenStream()
	if err != nil {
		return "", err
	}
	defer cleanup()

	h := sha256.New()
	buf := make([]byte, 64*1024)
	if _, err := io.CopyBuffer(h, stream, buf); err != nil {
		return "", fmt.Errorf("计算 Layer SHA-256 失败: %w", err)
	}

	l.Digest = "sha256:" + hex.EncodeToString(h.Sum(nil))
	return l.Digest, nil
}

// OpenStream 打开一个流式读取通道，提供完整的 Tar 数据流，无需落盘或占用大内存
func (l *TarLayer) OpenStream() (io.Reader, func(), error) {
	file, err := os.Open(l.FilePath)
	if err != nil {
		return nil, nil, fmt.Errorf("打开文件流失败: %w", err)
	}

	hr := bytes.NewReader(l.HeaderBytes)
	var pr io.Reader
	if l.PadSize > 0 {
		pr = bytes.NewReader(make([]byte, l.PadSize))
	} else {
		pr = bytes.NewReader(nil)
	}
	er := bytes.NewReader(make([]byte, 1024))

	cleanup := func() {
		_ = file.Close()
	}

	stream := io.MultiReader(hr, file, pr, er)
	return stream, cleanup, nil
}
