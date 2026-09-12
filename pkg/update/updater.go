package update

import (
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

// GitHubRelease GitHub Releases API 数据结构
type GitHubRelease struct {
	TagName string         `json:"tag_name"`
	Name    string         `json:"name"`
	Body    string         `json:"body"`
	Assets  []ReleaseAsset `json:"assets"`
}

// ReleaseAsset Release 附件资产
type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// MirrorNode 国内加速镜像节点配置
type MirrorNode struct {
	Name   string
	Prefix string
}

// 国内常用优质 GitHub 加速镜像源列表 (按稳定性与重试顺序排布)
var defaultMirrors = []MirrorNode{
	{Name: "GitHub Direct (Official)", Prefix: ""},
	{Name: "GHProxy CDN (ghproxy.net)", Prefix: "https://ghproxy.net/"},
	{Name: "GH-Proxy Global (gh-proxy.com)", Prefix: "https://gh-proxy.com/"},
	{Name: "FastGit Mirror (mirror.ghproxy.com)", Prefix: "https://mirror.ghproxy.com/"},
	{Name: "Moeyy Speedup (github.moeyy.xyz)", Prefix: "https://github.moeyy.xyz/"},
}

// GetExpectedAssetName 获取当前系统与架构对应的标准二进制名称
func GetExpectedAssetName() string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("ark-%s-%s%s", runtime.GOOS, runtime.GOARCH, ext)
}

// CleanOldExecutable Windows 平台下清理遗留的 .old 备份程序
func CleanOldExecutable() {
	if runtime.GOOS == "windows" {
		exePath, err := os.Executable()
		if err == nil {
			oldPath := exePath + ".old"
			_ = os.Remove(oldPath)
		}
	}
}

// FetchLatestRelease 带故障转移的 Release 元信息获取
func FetchLatestRelease(targetTag, token string) (*GitHubRelease, error) {
	apiEndpoints := []string{
		"https://api.github.com/repos/duorameng/ark/releases/latest",
	}
	if targetTag != "" && targetTag != "latest" {
		tag := targetTag
		if !strings.HasPrefix(tag, "v") {
			tag = "v" + tag
		}
		apiEndpoints = []string{
			fmt.Sprintf("https://api.github.com/repos/duorameng/ark/releases/tags/%s", tag),
		}
	}

	// 增加备用 API 代理终端
	for _, mirror := range defaultMirrors {
		if mirror.Prefix != "" {
			apiEndpoints = append(apiEndpoints, mirror.Prefix+apiEndpoints[0])
		}
	}

	var lastErr error
	client := &http.Client{Timeout: 12 * time.Second}

	for i, endpoint := range apiEndpoints {
		nodeName := "Official API"
		if i > 0 {
			nodeName = fmt.Sprintf("Mirror API (%s)", defaultMirrors[i-1].Name)
		}

		req, err := http.NewRequest("GET", endpoint, nil)
		if err != nil {
			lastErr = err
			continue
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "Ark-Updater")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("[%s] 连接超时或网络不可达: %w", nodeName, err)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			var rel GitHubRelease
			err := json.NewDecoder(resp.Body).Decode(&rel)
			resp.Body.Close()
			if err == nil && len(rel.Assets) > 0 {
				return &rel, nil
			}
		} else {
			resp.Body.Close()
			lastErr = fmt.Errorf("[%s] 返回状态码异常 (HTTP %d)", nodeName, resp.StatusCode)
		}
	}

	return nil, fmt.Errorf("未能从任何源获取到有效 Release 元数据: %w", lastErr)
}

// DownloadWithMirrors 遍历所有镜像源自动重试下载二进制
func DownloadWithMirrors(rawURL, destFile, token string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	var lastErr error

	for idx, mirror := range defaultMirrors {
		targetURL := mirror.Prefix + rawURL
		fmt.Printf("-> [%d/%d] 正在尝试从 [%s] 下载...\n", idx+1, len(defaultMirrors), mirror.Name)

		req, err := http.NewRequest("GET", targetURL, nil)
		if err != nil {
			lastErr = err
			continue
		}
		if token != "" && mirror.Prefix == "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("User-Agent", "Ark-Updater")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("   [-] 连接失败或超时: %v，自动切换下一个镜像源...\n", err)
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			fmt.Printf("   [-] 节点响应异常 (HTTP %d)，自动切换下一个镜像源...\n", resp.StatusCode)
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}

		out, err := os.OpenFile(destFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			resp.Body.Close()
			return fmt.Errorf("创建目标文件失败: %w", err)
		}

		written, copyErr := io.Copy(out, resp.Body)
		out.Close()
		resp.Body.Close()

		if copyErr != nil || written < 1024*100 { // 小于 100KB 说明下载不完整或返回了错误页
			_ = os.Remove(destFile)
			fmt.Printf("   [-] 数据包不完整或被截断 (已接收 %d 字节)，自动切换下一个镜像源...\n", written)
			lastErr = fmt.Errorf("文件不完整 (大小 %d 字节)", written)
			continue
		}

		sizeMB := float64(written) / 1024 / 1024
		fmt.Printf("   ✓ 下载成功！(来自 %s，大小: %.2f MB)\n", mirror.Name, sizeMB)
		return nil
	}

	return fmt.Errorf("所有国内加速源与直连源均尝试失败: %w", lastErr)
}

// SelfUpdate 从指定 URL 或通过 GitHub Release 镜像自动重试更新自身
func SelfUpdate(currentVersion, targetURLOrTag, token string) error {
	CleanOldExecutable()

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法获取当前程序路径: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("无法解析程序真实路径: %w", err)
	}

	downloadURL := ""
	newVersionTag := ""

	// 1. 如果用户直接指定了自建或特定下载 URL (以 http:// 或 https:// 开头)
	if strings.HasPrefix(targetURLOrTag, "http://") || strings.HasPrefix(targetURLOrTag, "https://") {
		downloadURL = targetURLOrTag
		newVersionTag = "custom-url"
		fmt.Printf("[Update] 正在从自定义下载链接获取新版本: %s\n", downloadURL)
	} else {
		// 2. 从 GitHub Release 获取元信息 (支持国内 API 加速备用节点)
		fmt.Println("[Update] 正在检索 GitHub Release 最新版本信息 (支持国内源容灾)...")
		rel, err := FetchLatestRelease(targetURLOrTag, token)
		if err != nil {
			return err
		}

		newVersionTag = rel.TagName
		expectedName := GetExpectedAssetName()

		fmt.Printf("[Version] 当前运行版本: %s\n", currentVersion)
		fmt.Printf("[Version] 远端最新版本: %s\n", newVersionTag)

		if currentVersion != "dev" && currentVersion == newVersionTag && !strings.Contains(targetURLOrTag, "force") {
			fmt.Println("✓ 当前已经是最新版本，无需更新。")
			return nil
		}

		for _, asset := range rel.Assets {
			if strings.EqualFold(asset.Name, expectedName) {
				downloadURL = asset.BrowserDownloadURL
				break
			}
		}

		if downloadURL == "" {
			for _, asset := range rel.Assets {
				if strings.Contains(asset.Name, runtime.GOOS) && strings.Contains(asset.Name, runtime.GOARCH) {
					downloadURL = asset.BrowserDownloadURL
					break
				}
			}
		}

		if downloadURL == "" {
			return fmt.Errorf("未在发布版本 %s 中找到适配当前平台 (%s/%s) 的二进制文件 (%s)",
				newVersionTag, runtime.GOOS, runtime.GOARCH, expectedName)
		}
	}

	// 3. 执行多源自动重试下载
	tmpFile := exePath + ".tmp"
	_ = os.Remove(tmpFile)

	fmt.Printf("[Download] 正在下载适配资产 (%s)...\n", filepath.Base(downloadURL))
	if err := DownloadWithMirrors(downloadURL, tmpFile, token); err != nil {
		_ = os.Remove(tmpFile)
		return err
	}

	// 4. 平台级原子自我替换
	fmt.Println("[Apply] 正在原子替换当前运行程序...")
	if runtime.GOOS == "windows" {
		oldFile := exePath + ".old"
		_ = os.Remove(oldFile)
		if err := os.Rename(exePath, oldFile); err != nil {
			_ = os.Remove(tmpFile)
			return fmt.Errorf("Windows 重命名旧程序失败: %w", err)
		}
		if err := os.Rename(tmpFile, exePath); err != nil {
			_ = os.Rename(oldFile, exePath) // 失败回滚
			return fmt.Errorf("替换新版本失败: %w", err)
		}
	} else {
		_ = os.Chmod(tmpFile, 0755)
		if err := os.Rename(tmpFile, exePath); err != nil {
			inTmp, errOpen := os.Open(tmpFile)
			if errOpen != nil {
				return errOpen
			}
			defer inTmp.Close()
			destFile, errDest := os.OpenFile(exePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if errDest != nil {
				return errDest
			}
			_, _ = io.Copy(destFile, inTmp)
			destFile.Close()
			_ = os.Remove(tmpFile)
		}
	}

	fmt.Println("================================================================")
	fmt.Printf("          🎉 Ark 已成功升级至版本: %s\n", newVersionTag)
	fmt.Printf("          程序路径: %s\n", exePath)
	fmt.Println("================================================================")
	return nil
}
