package oci

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// DefaultDockerUserAgent 深度拟态官方 Docker CLI / Containerd 的 User-Agent
	DefaultDockerUserAgent = "docker/26.1.4 go/go1.22.4 git-commit/564c1f4 os/linux arch/amd64"
	// DockerAPIVersionHeader 官方 Docker 交互必带协议头
	DockerAPIVersionHeader = "registry/2.0"
)

// Client OCI 注册表原生客户端
type Client struct {
	RegistryHost string // 如 ghcr.io
	RepoPath     string // 如 duorameng/ark
	Scheme       string // https 或 http
	Auth         *AuthManager
	HTTPClient   *http.Client
}

// NewClient 创建 OCI 客户端
func NewClient(fullRepo, username, token string) (*Client, error) {
	fullRepo = strings.TrimSpace(fullRepo)
	fullRepo = strings.TrimPrefix(fullRepo, "https://")
	fullRepo = strings.TrimPrefix(fullRepo, "http://")

	parts := strings.SplitN(fullRepo, "/", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("非法仓库名称 %s, 格式应为 <registry>/<owner>/<repo>", fullRepo)
	}

	regHost := parts[0]
	repoPath := parts[1]

	scheme := "https"
	if strings.HasPrefix(regHost, "localhost") || strings.HasPrefix(regHost, "127.0.0.1") {
		scheme = "http"
	}

	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	}
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   0, // 上传大文件不设全局超时，由具体 context 或传输超时控制
	}

	auth := NewAuthManager(username, token, httpClient)

	return &Client{
		RegistryHost: regHost,
		RepoPath:     repoPath,
		Scheme:       scheme,
		Auth:         auth,
		HTTPClient:   httpClient,
	}, nil
}

// EnsureAuth 预热鉴权通道，确保已有合法有效 Bearer Token
func (c *Client) EnsureAuth(ctx context.Context, defaultScope string) error {
	if defaultScope == "" {
		defaultScope = fmt.Sprintf("repository:%s:pull,push", c.RepoPath)
	}
	if tok := c.Auth.GetCachedTokenForScope(defaultScope); tok != "" {
		return nil
	}

	u := fmt.Sprintf("%s://%s/v2/", c.Scheme, c.RegistryHost)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json, */*")

	resp, err := c.doRequest(ctx, req, defaultScope)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// doRequest 执行带有 401 自动协商 Token 的 HTTP 请求
func (c *Client) doRequest(ctx context.Context, req *http.Request, defaultScope string) (*http.Response, error) {
	if defaultScope == "" {
		defaultScope = fmt.Sprintf("repository:%s:pull,push", c.RepoPath)
	}

	// 0. 深度拟态：设置官方 Docker CLI 特征 Header
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", DefaultDockerUserAgent)
	}
	req.Header.Set("Docker-Distribution-API-Version", DockerAPIVersionHeader)

	// 1. 若未显式设置 Authorization，优先尝试复用缓存中的有效 Token
	if req.Header.Get("Authorization") == "" {
		if tok := c.Auth.GetCachedTokenForScope(defaultScope); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}

	// 1. 发起请求
	resp, err := c.HTTPClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	// 2. 如果不需要认证或成功，直接返回
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	// 3. 处理 401 鉴权挑战
	authHeader := resp.Header.Get("Www-Authenticate")
	resp.Body.Close()

	if authHeader == "" {
		return nil, fmt.Errorf("收到 HTTP 401 但未包含 Www-Authenticate 头部")
	}

	realm, service, scope := ParseWwwAuthenticate(authHeader)
	if scope == "" {
		scope = defaultScope
	}

	token, err := c.Auth.GetTokenForScope(ctx, realm, service, scope)
	if err != nil {
		return nil, fmt.Errorf("鉴权协商失败: %w", err)
	}

	// 4. 使用新获得的 Token 重新构建请求并重试
	retryReq := req.Clone(ctx)
	retryReq.Header.Set("Authorization", "Bearer "+token)

	return c.HTTPClient.Do(retryReq)
}

// CheckBlobExists 探测远端是否存在指定 SHA-256 的 Blob (HEAD 请求)
func (c *Client) CheckBlobExists(ctx context.Context, digest string) (bool, error) {
	u := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", c.Scheme, c.RegistryHost, c.RepoPath, digest)
	req, err := http.NewRequestWithContext(ctx, "HEAD", u, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/octet-stream, */*")

	resp, err := c.doRequest(ctx, req, fmt.Sprintf("repository:%s:pull", c.RepoPath))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}

	return false, fmt.Errorf("检测 Blob 状态异常 (HTTP %d)", resp.StatusCode)
}

// UploadBlobStream 单次流式上传 Blob 到 OCI Registry (0 落盘，单通道直推)
func (c *Client) UploadBlobStream(ctx context.Context, digest string, size int64, stream io.Reader, onProgress func(written int64)) error {
	scope := fmt.Sprintf("repository:%s:pull,push", c.RepoPath)
	_ = c.EnsureAuth(ctx, scope)
	// 1. POST /v2/<repo>/blobs/uploads/ 获取上传通道
	initURL := fmt.Sprintf("%s://%s/v2/%s/blobs/uploads/", c.Scheme, c.RegistryHost, c.RepoPath)
	postReq, err := http.NewRequestWithContext(ctx, "POST", initURL, nil)
	if err != nil {
		return err
	}
	postReq.Header.Set("Content-Length", "0")

	postResp, err := c.doRequest(ctx, postReq, fmt.Sprintf("repository:%s:pull,push", c.RepoPath))
	if err != nil {
		return fmt.Errorf("初始化 Blob 上传通道失败: %w", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(postResp.Body)
		return fmt.Errorf("创建上传通道失败 (HTTP %d): %s", postResp.StatusCode, string(body))
	}

	location := postResp.Header.Get("Location")
	if location == "" {
		return fmt.Errorf("注册表未返回上传 Location 头")
	}

	uploadURL, err := postResp.Request.URL.Parse(location)
	if err != nil {
		return fmt.Errorf("解析上传通道 Location 失败: %w", err)
	}

	// 2. 追加 digest 参数
	q := uploadURL.Query()
	q.Set("digest", digest)
	uploadURL.RawQuery = q.Encode()

	// 3. 构建带进度监听的 Reader
	bodyReader := stream
	if onProgress != nil {
		bodyReader = &progressReader{
			reader:     stream,
			onProgress: onProgress,
		}
	}

	// 4. 单次 PUT 上传整个 Blob
	putReq, err := http.NewRequestWithContext(ctx, "PUT", uploadURL.String(), bodyReader)
	if err != nil {
		return err
	}
	putReq.ContentLength = size
	putReq.Header.Set("Content-Type", "application/octet-stream")
	putReq.Header.Set("Content-Length", fmt.Sprintf("%d", size))

	putResp, err := c.doRequest(ctx, putReq, fmt.Sprintf("repository:%s:pull,push", c.RepoPath))
	if err != nil {
		return fmt.Errorf("推流上传 Blob 失败: %w", err)
	}
	defer putResp.Body.Close()

	if putResp.StatusCode != http.StatusCreated && putResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(putResp.Body)
		return fmt.Errorf("提交 Blob 失败 (HTTP %d): %s", putResp.StatusCode, string(body))
	}

	return nil
}

// UploadBlobBytes 上传内存字节数组 Blob (适用于 Config JSON)
func (c *Client) UploadBlobBytes(ctx context.Context, digest string, data []byte) error {
	exists, err := c.CheckBlobExists(ctx, digest)
	if err == nil && exists {
		return nil
	}
	return c.UploadBlobStream(ctx, digest, int64(len(data)), bytes.NewReader(data), nil)
}

// PutManifest 提交镜像清单 Manifest
func (c *Client) PutManifest(ctx context.Context, tag string, manifestBytes []byte, mediaType string) error {
	u := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", c.Scheme, c.RegistryHost, c.RepoPath, tag)
	req, err := http.NewRequestWithContext(ctx, "PUT", u, bytes.NewReader(manifestBytes))
	if err != nil {
		return err
	}

	req.ContentLength = int64(len(manifestBytes))
	req.Header.Set("Content-Type", mediaType)

	resp, err := c.doRequest(ctx, req, fmt.Sprintf("repository:%s:pull,push", c.RepoPath))
	if err != nil {
		return fmt.Errorf("提交 Manifest 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("提交 Manifest 失败 (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// GetManifest 拉取镜像清单 Manifest
func (c *Client) GetManifest(ctx context.Context, tag string) (*Manifest, error) {
	u := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", c.Scheme, c.RegistryHost, c.RepoPath, tag)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	acceptList := strings.Join([]string{
		MediaTypeDockerManifestV2,
		"application/vnd.docker.distribution.manifest.list.v2+json",
		MediaTypeOCIManifestV1,
		"application/vnd.oci.image.index.v1+json",
		"*/*",
	}, ", ")
	req.Header.Set("Accept", acceptList)

	resp, err := c.doRequest(ctx, req, fmt.Sprintf("repository:%s:pull", c.RepoPath))
	if err != nil {
		return nil, fmt.Errorf("拉取 Manifest 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("获取 Manifest 失败 (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var mf Manifest
	if err := json.NewDecoder(resp.Body).Decode(&mf); err != nil {
		return nil, fmt.Errorf("解析 Manifest JSON 失败: %w", err)
	}

	// 若拉取到的是多架构索引清单 (ManifestList / ImageIndex)，自动按当前宿主机架构解析具体的单架构 Manifest
	if len(mf.Manifests) > 0 {
		targetDigest := mf.Manifests[0].Digest
		currentArch := runtime.GOARCH
		for _, m := range mf.Manifests {
			if m.Platform.Architecture == currentArch {
				targetDigest = m.Digest
				break
			}
		}
		// 递归获取具体平台的单架构 Manifest
		return c.GetManifest(ctx, targetDigest)
	}

	return &mf, nil
}

// DownloadBlobAndExtractCargo 下载指定 Layer Blob 并提取其中的 cargo/* 货物文件
func (c *Client) DownloadBlobAndExtractCargo(ctx context.Context, digest, outDir string, onProgress func(written int64)) error {
	u := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", c.Scheme, c.RegistryHost, c.RepoPath, digest)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/octet-stream, */*")

	resp, err := c.doRequest(ctx, req, fmt.Sprintf("repository:%s:pull", c.RepoPath))
	if err != nil {
		return fmt.Errorf("请求 Layer Blob 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("下载 Blob 失败 (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var reader io.Reader = resp.Body
	if onProgress != nil {
		reader = &progressReader{
			reader:     resp.Body,
			onProgress: onProgress,
		}
	}

	tr := tar.NewReader(reader)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("解析 Layer Tar 失败: %w", err)
		}

		// 仅提取 cargo/ 目录下的货物
		cleanName := filepath.Clean(hdr.Name)
		if strings.HasPrefix(cleanName, "cargo") || strings.HasPrefix(cleanName, "cargo/") || strings.HasPrefix(cleanName, "cargo\\") {
			baseName := filepath.Base(cleanName)
			destPath := filepath.Join(outDir, baseName)
			f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				return fmt.Errorf("创建解包目标文件失败: %w", err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("写入解包目标文件失败: %w", err)
			}
			f.Close()
		}
	}

	return nil
}

type progressReader struct {
	reader     io.Reader
	onProgress func(written int64)
	written    int64
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 {
		pr.written += int64(n)
		if pr.onProgress != nil {
			pr.onProgress(pr.written)
		}
	}
	return n, err
}
