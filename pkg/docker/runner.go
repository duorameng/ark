package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GenerateDockerfile 生成独立分层的 BuildKit Dockerfile
func GenerateDockerfile(destPath string, layerFiles []string) error {
	var sb strings.Builder
	sb.WriteString("# syntax=docker/dockerfile:1.4\n")
	sb.WriteString("FROM scratch\n")

	for _, file := range layerFiles {
		base := filepath.Base(file)
		sb.WriteString(fmt.Sprintf("COPY --link cache/%s /cargo/%s\n", base, base))
	}

	sb.WriteString("CMD [\"ark-voyage\"]\n")

	return os.WriteFile(destPath, []byte(sb.String()), 0644)
}

// Build 运行 Docker BuildKit 构建独立分层快照镜像
func Build(dockerfilePath, workspaceRoot string, tags ...string) error {
	args := []string{"build", "-f", dockerfilePath}
	for _, t := range tags {
		args = append(args, "-t", t)
	}
	args = append(args, workspaceRoot)

	cmd := exec.Command("docker", args...)
	cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// Login 使用 GH_TOKEN 执行 docker login
func Login(registryHost, username, token string) error {
	cmd := exec.Command("docker", "login", registryHost, "-u", username, "--password-stdin")
	cmd.Stdin = strings.NewReader(token)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// PushWithRetry 将镜像推送到 OCI Registry，支持指定重试次数与自动退避重试
func PushWithRetry(tag string, maxRetries int, initialDelay time.Duration) error {
	if maxRetries <= 0 {
		maxRetries = 1
	}

	var lastErr error
	delay := initialDelay
	if delay <= 0 {
		delay = 3 * time.Second
	}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		cmd := exec.Command("docker", "push", tag)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err == nil {
			return nil
		}

		lastErr = err
		if attempt < maxRetries {
			fmt.Fprintf(os.Stderr, "\n[!] 镜像推送遇到网络抖动或超时 (%v)，正在进行第 %d/%d 次重试 (等待 %v)...\n", err, attempt+1, maxRetries, delay)
			time.Sleep(delay)
			// 指数退避，上限 15 秒
			delay *= 2
			if delay > 15*time.Second {
				delay = 15 * time.Second
			}
		}
	}

	return fmt.Errorf("连续重试 %d 次推送均失败: %w", maxRetries, lastErr)
}

// Push 兼容原有单次调用
func Push(tag string) error {
	return PushWithRetry(tag, 1, 0)
}

// Pull 从 OCI Registry 拉取镜像
func Pull(tag string) error {
	cmd := exec.Command("docker", "pull", tag)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ExtractCargoFromImage 从镜像中导出 /cargo 目录
func ExtractCargoFromImage(imageTag, outDir string) error {
	// 1. 创建免运行容器
	createCmd := exec.Command("docker", "create", imageTag)
	cidBytes, err := createCmd.Output()
	if err != nil {
		return fmt.Errorf("无法创建临时提取容器: %w", err)
	}
	cid := strings.TrimSpace(string(cidBytes))
	defer func() {
		_ = exec.Command("docker", "rm", cid).Run()
	}()

	// 2. 导出文件
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}

	src := fmt.Sprintf("%s:/cargo/.", cid)
	cpCmd := exec.Command("docker", "cp", src, outDir)
	cpCmd.Stdout = os.Stdout
	cpCmd.Stderr = os.Stderr
	if err := cpCmd.Run(); err != nil {
		return fmt.Errorf("导出容器数据失败: %w", err)
	}

	return nil
}
