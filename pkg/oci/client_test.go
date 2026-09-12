package oci

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestOCIClientEndToEnd(t *testing.T) {
	// 内存模拟 OCI Registry
	var (
		mu           sync.Mutex
		blobs        = make(map[string][]byte)
		manifests    = make(map[string][]byte)
		validBearer  = "ark-mock-token-xyz"
		expectedUser = "testuser"
		expectedPass = "testpass"
	)

	var serverURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		// 1. Token 交换接口
		if r.URL.Path == "/token" {
			u, p, ok := r.BasicAuth()
			if !ok || u != expectedUser || p != expectedPass {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"token": validBearer,
			})
			return
		}

		// 2. 检查 Bearer Token
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+validBearer {
			w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="test-service",scope="repository:test/ark:pull,push"`, serverURL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// 3. 处理 Blobs HEAD
		if r.Method == "HEAD" && strings.HasPrefix(r.URL.Path, "/v2/test/ark/blobs/") {
			digest := strings.TrimPrefix(r.URL.Path, "/v2/test/ark/blobs/")
			if data, ok := blobs[digest]; ok {
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// 4. 处理 Blobs GET
		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v2/test/ark/blobs/") {
			digest := strings.TrimPrefix(r.URL.Path, "/v2/test/ark/blobs/")
			if data, ok := blobs[digest]; ok {
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(data)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// 5. 处理 Blobs POST (初始化上传通道)
		if r.Method == "POST" && r.URL.Path == "/v2/test/ark/blobs/uploads/" {
			w.Header().Set("Location", "/v2/test/ark/blobs/uploads/session-12345")
			w.WriteHeader(http.StatusAccepted)
			return
		}

		// 6. 处理 Blobs PUT (流式写入)
		if r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/v2/test/ark/blobs/uploads/session-12345") {
			digest := r.URL.Query().Get("digest")
			data, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			h := sha256.Sum256(data)
			actualDigest := "sha256:" + hex.EncodeToString(h[:])
			if digest != actualDigest {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(fmt.Sprintf("digest mismatch: expected %s, got %s", digest, actualDigest)))
				return
			}

			blobs[digest] = data
			w.WriteHeader(http.StatusCreated)
			return
		}

		// 7. 处理 Manifests PUT
		if r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/v2/test/ark/manifests/") {
			tag := strings.TrimPrefix(r.URL.Path, "/v2/test/ark/manifests/")
			data, _ := io.ReadAll(r.Body)
			manifests[tag] = data
			w.WriteHeader(http.StatusCreated)
			return
		}

		// 8. 处理 Manifests GET
		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v2/test/ark/manifests/") {
			tag := strings.TrimPrefix(r.URL.Path, "/v2/test/ark/manifests/")
			if data, ok := manifests[tag]; ok {
				w.Header().Set("Content-Type", MediaTypeDockerManifestV2)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(data)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	serverURL = ts.URL

	tsU, _ := url.Parse(serverURL)
	repo := fmt.Sprintf("%s/test/ark", tsU.Host)

	client, err := NewClient(repo, expectedUser, expectedPass)
	if err != nil {
		t.Fatalf("NewClient 失败: %v", err)
	}

	ctx := context.Background()

	// 准备测试文件
	tmpDir := t.TempDir()
	cargo1File := filepath.Join(tmpDir, "postgres.dat")
	_ = os.WriteFile(cargo1File, []byte("postgres encrypted payload 1234567890"), 0644)

	layer1, err := NewTarLayer(cargo1File)
	if err != nil {
		t.Fatalf("NewTarLayer 失败: %v", err)
	}

	digest1, err := layer1.ComputeDigest()
	if err != nil {
		t.Fatalf("ComputeDigest 失败: %v", err)
	}

	// 1. 验证 HEAD 初次探测不存在
	exists, err := client.CheckBlobExists(ctx, digest1)
	if err != nil {
		t.Fatalf("CheckBlobExists 失败: %v", err)
	}
	if exists {
		t.Errorf("初次探测期望不存在，实际存在")
	}

	// 2. 流式上传 Layer
	stream, cleanup, err := layer1.OpenStream()
	if err != nil {
		t.Fatalf("OpenStream 失败: %v", err)
	}
	defer cleanup()

	var totalWritten int64
	err = client.UploadBlobStream(ctx, digest1, layer1.TotalSize, stream, func(written int64) {
		totalWritten = written
	})
	if err != nil {
		t.Fatalf("UploadBlobStream 失败: %v", err)
	}

	if totalWritten != layer1.TotalSize {
		t.Errorf("进度报告字节数 %d 与 TotalSize %d 不符", totalWritten, layer1.TotalSize)
	}

	// 3. 验证 HEAD 再次探测存在 (秒传探测)
	exists, err = client.CheckBlobExists(ctx, digest1)
	if err != nil {
		t.Fatalf("再次 CheckBlobExists 失败: %v", err)
	}
	if !exists {
		t.Errorf("上传后探测期望存在，实际不存在")
	}

	// 4. 生成 Config 并上传
	layers := []*TarLayer{layer1}
	cfgBytes, cfgDigest, _, err := GenerateConfigJSON(layers)
	if err != nil {
		t.Fatalf("GenerateConfigJSON 失败: %v", err)
	}

	err = client.UploadBlobBytes(ctx, cfgDigest, cfgBytes)
	if err != nil {
		t.Fatalf("UploadBlobBytes 失败: %v", err)
	}

	// 5. 生成 Manifest 并提交
	mfBytes, mfDigest, _, err := GenerateManifestJSON(cfgDigest, int64(len(cfgBytes)), layers, true)
	if err != nil {
		t.Fatalf("GenerateManifestJSON 失败: %v", err)
	}

	tag := "vps-20260912-180000"
	err = client.PutManifest(ctx, tag, mfBytes, MediaTypeDockerManifestV2)
	if err != nil {
		t.Fatalf("PutManifest 失败: %v", err)
	}
	_ = client.PutManifest(ctx, mfDigest, mfBytes, MediaTypeDockerManifestV2)

	// 6. 验证获取 Manifest
	mf, err := client.GetManifest(ctx, tag)
	if err != nil {
		t.Fatalf("GetManifest 失败: %v", err)
	}
	if mf.Config.Digest != cfgDigest {
		t.Errorf("Manifest Config Digest 期望 %s, 实际 %s", cfgDigest, mf.Config.Digest)
	}
	if len(mf.Layers) != 1 || mf.Layers[0].Digest != digest1 {
		t.Errorf("Manifest Layers 与期望不符: %+v", mf.Layers)
	}

	// 7. 测试下载解包提取 Cargo (模拟 ark land)
	extractDir := filepath.Join(tmpDir, "extracted")
	_ = os.MkdirAll(extractDir, 0755)

	err = client.DownloadBlobAndExtractCargo(ctx, digest1, extractDir, nil)
	if err != nil {
		t.Fatalf("DownloadBlobAndExtractCargo 失败: %v", err)
	}

	extractedFile := filepath.Join(extractDir, "postgres.dat")
	content, err := os.ReadFile(extractedFile)
	if err != nil {
		t.Fatalf("读取解包提取文件失败: %v", err)
	}
	if string(content) != "postgres encrypted payload 1234567890" {
		t.Errorf("解包内容不一致: %s", string(content))
	}

	// 8. 验证多架构 Manifest List (Multi-Arch Index)
	cfgBytesARM, cfgDigestARM, _, err := GenerateArchConfigJSON("arm64", layers)
	if err != nil {
		t.Fatalf("GenerateArchConfigJSON arm64 失败: %v", err)
	}
	_ = client.UploadBlobBytes(ctx, cfgDigestARM, cfgBytesARM)

	mfBytesARM, mfDigestARM, _, err := GenerateManifestJSON(cfgDigestARM, int64(len(cfgBytesARM)), layers, true)
	if err != nil {
		t.Fatalf("GenerateManifestJSON arm64 失败: %v", err)
	}
	_ = client.PutManifest(ctx, mfDigestARM, mfBytesARM, MediaTypeDockerManifestV2)

	multiIndex, _, _, err := GenerateMultiArchIndex([]ManifestDescriptor{
		{
			MediaType: MediaTypeDockerManifestV2,
			Size:      int64(len(mfBytes)),
			Digest:    mfDigest,
			Platform: Platform{
				Architecture: "amd64",
				OS:           "linux",
			},
		},
		{
			MediaType: MediaTypeDockerManifestV2,
			Size:      int64(len(mfBytesARM)),
			Digest:    mfDigestARM,
			Platform: Platform{
				Architecture: "arm64",
				OS:           "linux",
			},
		},
	}, true)
	if err != nil {
		t.Fatalf("GenerateMultiArchIndex 失败: %v", err)
	}

	multiTag := "multi-arch-test"
	if err := client.PutManifest(ctx, multiTag, multiIndex, MediaTypeDockerManifestList); err != nil {
		t.Fatalf("PutManifest Multi-Arch 失败: %v", err)
	}

	// 验证 GetManifest 能自动解析多架构索引并返回匹配宿主机平台的 Manifest
	resolvedMf, err := client.GetManifest(ctx, multiTag)
	if err != nil {
		t.Fatalf("解析多架构 Manifest 失败: %v", err)
	}
	if len(resolvedMf.Layers) != 1 || resolvedMf.Layers[0].Digest != digest1 {
		t.Errorf("解析出的多架构层与期望不符: %+v", resolvedMf.Layers)
	}
}
