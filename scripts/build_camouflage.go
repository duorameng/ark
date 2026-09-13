package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// 本工具用于将 cmd/fakeweb/main.go 静态交叉编译为 linux/amd64 与 linux/arm64 二进制，
// 并使用 Gzip 压缩后输出到 pkg/oci/assets/，供 Ark 主程序通过 //go:embed 嵌入。
func main() {
	architectures := []struct {
		goarch   string
		outAsset string
	}{
		{goarch: "amd64", outAsset: "server_linux_amd64.gz"},
		{goarch: "arm64", outAsset: "server_linux_arm64.gz"},
	}

	assetsDir := filepath.Join("pkg", "oci", "assets")
	_ = os.MkdirAll(assetsDir, 0755)

	tmpDir, err := os.MkdirTemp("", "ark_camo_build_*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建临时目录失败: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	for _, arch := range architectures {
		fmt.Printf("🔨 正在静态编译 cmd/fakeweb -> linux/%s...\n", arch.goarch)
		rawBinPath := filepath.Join(tmpDir, "server_"+arch.goarch)

		cmd := exec.Command("go", "build", "-ldflags=-s -w", "-trimpath", "-o", rawBinPath, "./cmd/fakeweb")
		cmd.Env = append(os.Environ(),
			"CGO_ENABLED=0",
			"GOOS=linux",
			"GOARCH="+arch.goarch,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "编译 linux/%s 失败: %v\n", arch.goarch, err)
			os.Exit(1)
		}

		binData, err := os.ReadFile(rawBinPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取编译产物失败: %v\n", err)
			os.Exit(1)
		}

		// 使用 Gzip 最高压缩等级压缩
		var gzBuf bytes.Buffer
		gw, err := gzip.NewWriterLevel(&gzBuf, gzip.BestCompression)
		if err != nil {
			fmt.Fprintf(os.Stderr, "初始化 gzip 失败: %v\n", err)
			os.Exit(1)
		}
		if _, err := gw.Write(binData); err != nil {
			fmt.Fprintf(os.Stderr, "gzip 压缩失败: %v\n", err)
			os.Exit(1)
		}
		_ = gw.Close()

		targetAssetPath := filepath.Join(assetsDir, arch.outAsset)
		if err := os.WriteFile(targetAssetPath, gzBuf.Bytes(), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入资产文件失败: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("✓ 生成嵌入资产: %s (原始大小: %d 字节 -> Gzip: %d 字节)\n",
			targetAssetPath, len(binData), gzBuf.Len())
	}

	fmt.Println("\n🎉 全部架构伪装微服务底座资产已就绪，可直接编译 Ark 主程序！")
}
