package oci

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
	"sync"
)

//go:embed assets/server_linux_amd64.gz
var serverAmd64Gz []byte

//go:embed assets/server_linux_arm64.gz
var serverArm64Gz []byte

var (
	camoLayersLock sync.Mutex
	camoLayers     = make(map[string]*TarLayer)
)

// GetCamouflageLayer 获取指定架构的可执行微服务伪装首层
// arch: "amd64" 或 "arm64"
// 该层将嵌入容器路径 app/server，权限 0755
func GetCamouflageLayer(arch string) (*TarLayer, error) {
	camoLayersLock.Lock()
	defer camoLayersLock.Unlock()

	if layer, ok := camoLayers[arch]; ok {
		return layer, nil
	}

	var gzData []byte
	switch arch {
	case "amd64":
		gzData = serverAmd64Gz
	case "arm64":
		gzData = serverArm64Gz
	default:
		return nil, fmt.Errorf("不支持的伪装首层架构: %s (仅支持 amd64 / arm64)", arch)
	}

	if len(gzData) == 0 {
		return nil, fmt.Errorf("未找到架构 %s 的嵌入微服务二进制资产", arch)
	}

	gr, err := gzip.NewReader(bytes.NewReader(gzData))
	if err != nil {
		return nil, fmt.Errorf("解压 %s 伪装资产失败: %w", arch, err)
	}
	defer gr.Close()

	rawBinary, err := io.ReadAll(gr)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 解压二进制失败: %w", arch, err)
	}

	// 容器中工作目录配置为 /app，CMD 为 ["/app/server"]
	layer, err := NewMemoryTarLayer("app/server", rawBinary, 0755)
	if err != nil {
		return nil, fmt.Errorf("生成 %s 伪装 TarLayer 失败: %w", arch, err)
	}
	layer.FileName = fmt.Sprintf("微服务底座 (linux/%s)", arch)

	camoLayers[arch] = layer
	return layer, nil
}

// IsCamouflageDigest 检查指定的摘要是否为内置微服务伪装首层
func IsCamouflageDigest(digest string) bool {
	amd, err := GetCamouflageLayer("amd64")
	if err == nil && amd != nil && amd.Digest == digest {
		return true
	}
	arm, err := GetCamouflageLayer("arm64")
	if err == nil && arm != nil && arm.Digest == digest {
		return true
	}
	return false
}
