package registry

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"ark/pkg/oci"
)

// BaseProvider 提供所有 OCI 注册表共享的基础通信和鉴权设施
type BaseProvider struct {
	id          string
	displayName string
	repository  string
	username    string
	password    string

	mu        sync.Mutex
	ociClient *oci.Client
}

// NewBaseProvider 构造基础 OCI Provider
func NewBaseProvider(id, displayName, repository, username, password string) *BaseProvider {
	return &BaseProvider{
		id:          id,
		displayName: displayName,
		repository:  repository,
		username:    username,
		password:    password,
	}
}

func (b *BaseProvider) ID() string {
	return b.id
}

func (b *BaseProvider) DisplayName() string {
	return b.displayName
}

// Host 解析并返回注册表主机域名
func (b *BaseProvider) Host() string {
	parts := strings.Split(b.repository, "/")
	if len(parts) > 1 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":")) {
		return parts[0]
	}
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return "registry.local"
}

func (b *BaseProvider) Repository() string {
	return b.repository
}

func (b *BaseProvider) Username() string {
	return b.username
}

// MaskedUsername 返回安全脱敏后的用户名
func (b *BaseProvider) MaskedUsername() string {
	if b.username == "" {
		return "(Token 自动协商)"
	}
	return MaskString(b.username)
}

func (b *BaseProvider) Password() string {
	return b.password
}

// MaskedPassword 返回安全脱敏后的密码/Token
func (b *BaseProvider) MaskedPassword() string {
	return MaskString(b.password)
}

// GetOCIClient 懒加载并返回绑定的 OCI 客户端
func (b *BaseProvider) GetOCIClient(ctx context.Context) (*oci.Client, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.ociClient != nil {
		return b.ociClient, nil
	}

	client, err := oci.NewClient(b.repository, b.username, b.password)
	if err != nil {
		return nil, fmt.Errorf("创建 OCI 客户端失败 [%s]: %w", b.repository, err)
	}
	b.ociClient = client
	return b.ociClient, nil
}

// Check 执行与远端 OCI 注册表的网络握手与 Bearer Token 鉴权探测
func (b *BaseProvider) Check(ctx context.Context) error {
	client, err := b.GetOCIClient(ctx)
	if err != nil {
		return err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	return client.EnsureAuth(probeCtx, "")
}

// ResolveLatestTag 标准 OCI 通用行为：默认调取 latest 标签
func (b *BaseProvider) ResolveLatestTag(ctx context.Context, category string) (string, error) {
	return "latest", nil
}

// ListVersions 标准 OCI 通用行为：打印港位信息与控制台查看指引
func (b *BaseProvider) ListVersions(ctx context.Context, categoryFilter string) error {
	fmt.Printf("[港位] %s: %s\n", b.DisplayName(), b.Repository())
	fmt.Printf("💡 提示: %s 请通过对应云平台控制台或 OCI 客户端查看历史标签版本。\n", b.DisplayName())
	return nil
}

// MaintainQuota 标准 OCI 通用行为：输出配额维护与固定 Tag 覆盖指引
func (b *BaseProvider) MaintainQuota(ctx context.Context, category string, retentionCount int, tag string, isFixedTag bool) error {
	fmt.Printf("\n------------------- 通用 OCI 注册表配额管理 (%s) -------------------\n", b.Host())
	fmt.Printf("✓ 航次已成功推送至 %s (%s)\n", b.DisplayName(), b.Repository())
	if isFixedTag {
		fmt.Printf("✓ 当前采用固定 Tag 覆盖模式 (%s)，已自动覆写上一航次，远端仓库始终保持最新单版本。\n", tag)
	} else {
		fmt.Printf("💡 提示: 针对 %s 等环境，建议使用固定 Tag 覆盖模式 (如 ark board --latest) 实现自动覆写，免手动清理。\n", b.DisplayName())
	}
	return nil
}

// PruneUntagged 标准 OCI 通用行为：跳过 GitHub 专属的 untagged 清理
func (b *BaseProvider) PruneUntagged(ctx context.Context) (int, error) {
	fmt.Printf("✓ 当前目标为 %s，跳过 GitHub 专属 untagged API 清理。\n", b.DisplayName())
	return 0, nil
}

// MaskString 安全脱敏工具函数
func MaskString(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(未配置)"
	}
	if strings.Contains(s, "@") {
		parts := strings.SplitN(s, "@", 2)
		u := parts[0]
		if len(u) > 3 {
			u = u[:3] + "****"
		} else {
			u = u[:1] + "****"
		}
		return u + "@" + parts[1]
	}
	if len(s) > 8 {
		return s[:3] + "****" + s[len(s)-3:]
	}
	if len(s) > 4 {
		return s[:2] + "****"
	}
	return "****"
}
