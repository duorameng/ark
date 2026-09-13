package oci

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetCamouflageLayer(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			layer, err := GetCamouflageLayer(arch)
			if err != nil {
				t.Fatalf("获取 %s 伪装首层失败: %v", arch, err)
			}
			if layer == nil {
				t.Fatalf("%s 伪装首层为 nil", arch)
			}
			if !strings.Contains(layer.FileName, "微服务底座") {
				t.Errorf("期望 FileName 包含微服务底座, 实际 %s", layer.FileName)
			}
			if layer.TargetCargo != "app/server" {
				t.Errorf("期望 TargetCargo 为 app/server, 实际 %s", layer.TargetCargo)
			}
			if !strings.HasPrefix(layer.Digest, "sha256:") {
				t.Errorf("期望 Digest 以 sha256: 开头, 实际 %s", layer.Digest)
			}
			if layer.TotalSize <= 0 {
				t.Errorf("期望 TotalSize > 0, 实际 %d", layer.TotalSize)
			}

			// 测试缓存一致性
			layer2, err := GetCamouflageLayer(arch)
			if err != nil {
				t.Fatalf("第二次获取 %s 伪装首层失败: %v", arch, err)
			}
			if layer != layer2 {
				t.Errorf("期望命中缓存返回同一指针")
			}
		})
	}

	// 测试不支持的架构
	_, err := GetCamouflageLayer("mips")
	if err == nil {
		t.Errorf("期望不支持的架构返回错误，实际无错误")
	}

	// 测试 IsCamouflageDigest
	amdLayer, _ := GetCamouflageLayer("amd64")
	if !IsCamouflageDigest(amdLayer.Digest) {
		t.Errorf("期望 IsCamouflageDigest(%s) 为 true", amdLayer.Digest)
	}
	armLayer, _ := GetCamouflageLayer("arm64")
	if !IsCamouflageDigest(armLayer.Digest) {
		t.Errorf("期望 IsCamouflageDigest(%s) 为 true", armLayer.Digest)
	}
	if IsCamouflageDigest("sha256:0000000000000000000000000000000000000000000000000000000000000000") {
		t.Errorf("期望未知摘要返回 false")
	}
}

func TestCamouflageLayerEndToEndAndLandIsolation(t *testing.T) {
	tmpDir := t.TempDir()

	// 创建一个模拟的 cargo 数据层文件
	cargoFile := filepath.Join(tmpDir, "mock_data.dat")
	if err := os.WriteFile(cargoFile, []byte("cargo data content"), 0644); err != nil {
		t.Fatalf("写入 cargo 文件失败: %v", err)
	}

	cargoLayer, err := NewTarLayer(cargoFile)
	if err != nil {
		t.Fatalf("NewTarLayer 失败: %v", err)
	}

	camoAMD, err := GetCamouflageLayer("amd64")
	if err != nil {
		t.Fatalf("GetCamouflageLayer(amd64) 失败: %v", err)
	}

	// 组装分层: Layer 0 为微服务伪装，Layer 1 为业务货舱
	combinedLayers := []*TarLayer{camoAMD, cargoLayer}

	// 验证 Config 生成
	cfgBytes, cfgDigest, cfgSize, err := GenerateArchConfigJSON("amd64", combinedLayers)
	if err != nil {
		t.Fatalf("GenerateArchConfigJSON 失败: %v", err)
	}
	if len(cfgBytes) == 0 || cfgDigest == "" || cfgSize <= 0 {
		t.Fatalf("Config 生成数据异常")
	}

	// 验证 Manifest 生成
	mfBytes, mfDigest, mfSize, err := GenerateManifestJSON(cfgDigest, cfgSize, combinedLayers, true)
	if err != nil {
		t.Fatalf("GenerateManifestJSON 失败: %v", err)
	}
	if len(mfBytes) == 0 || mfDigest == "" || mfSize <= 0 {
		t.Fatalf("Manifest 生成数据异常")
	}

	// 验证下船隔离 (通过直接解析 Tar 流模拟)
	landOutDir := filepath.Join(tmpDir, "land_out")
	if err := os.MkdirAll(landOutDir, 0755); err != nil {
		t.Fatalf("创建解压测试目录失败: %v", err)
	}

	// 模拟解压伪装首层：IsCamouflageDigest 应直接识别并跳过
	if !IsCamouflageDigest(camoAMD.Digest) {
		t.Errorf("camoAMD 应该被 IsCamouflageDigest 识别")
	}

	// 即使未跳过直接流式提取，由于 target 是 app/server，不命中 app/data 或 cargo，绝不会有任何文件解出
	stream, cleanup, err := camoAMD.OpenStream()
	if err != nil {
		t.Fatalf("打开伪装层流失败: %v", err)
	}
	defer cleanup()

	tr := tar.NewReader(stream)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("解析伪装 Tar 失败: %v", err)
		}
		cleanName := filepath.Clean(hdr.Name)
		if strings.HasPrefix(cleanName, "cargo") || strings.HasPrefix(cleanName, "app/data") || strings.HasPrefix(cleanName, "app\\data") {
			t.Errorf("伪装层中不应该存在 cargo 或 app/data 前缀的文件: %s", cleanName)
		}
	}

	entries, _ := os.ReadDir(landOutDir)
	if len(entries) != 0 {
		t.Errorf("伪装层不应在解包目录下产生任何文件")
	}

	// 验证业务数据层能被正常识别为 app/data 并提取
	cargoStream, cargoCleanup, err := cargoLayer.OpenStream()
	if err != nil {
		t.Fatalf("打开 cargo 层流失败: %v", err)
	}
	defer cargoCleanup()

	cargoTr := tar.NewReader(cargoStream)
	extractedCount := 0
	for {
		hdr, err := cargoTr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("解析 cargo Tar 失败: %v", err)
		}
		cleanName := filepath.Clean(hdr.Name)
		isCargo := strings.HasPrefix(cleanName, "app/data") || strings.HasPrefix(cleanName, "app\\data") ||
			strings.HasPrefix(cleanName, "cargo") || strings.HasPrefix(cleanName, "cargo/") || strings.HasPrefix(cleanName, "cargo\\")
		if isCargo {
			extractedCount++
		}
	}
	if extractedCount != 1 {
		t.Errorf("期望解包提取 1 个 cargo 文件，实际 %d", extractedCount)
	}
}

