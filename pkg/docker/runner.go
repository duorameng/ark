package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// Push 将镜像推送到 OCI Registry
func Push(tag string) error {
	cmd := exec.Command("docker", "push", tag)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
