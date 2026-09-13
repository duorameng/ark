package registry

import (
	"context"
	"fmt"
	"strings"
)

// AliyunProvider 实现阿里云容器镜像服务 (ACR) 的特定流程与能力。
// 适配阿里云 ACR 个人版的专属配额管理、固定 Tag 覆盖与控制台版本指引。
type AliyunProvider struct {
	*BaseProvider
}

// NewAliyunProvider 构造 Aliyun ACR Provider
func NewAliyunProvider(repository, username, password string) *AliyunProvider {
	base := NewBaseProvider("aliyun", "阿里云容器镜像服务 (ACR)", repository, username, password)
	return &AliyunProvider{
		BaseProvider: base,
	}
}

// Host 返回阿里云 ACR 镜像注册表域名 (若未显式指定则默认 registry.cn-hangzhou.aliyuncs.com)
func (a *AliyunProvider) Host() string {
	parts := strings.Split(a.repository, "/")
	if len(parts) > 1 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":")) {
		return parts[0]
	}
	return "registry.cn-hangzhou.aliyuncs.com"
}

// ResolveLatestTag 阿里云 ACR 个人版无免费版标签查询 API，默认定位并调取固定 latest 标签
func (a *AliyunProvider) ResolveLatestTag(ctx context.Context, category string) (string, error) {
	return "latest", nil
}

// ListVersions 阿里云 ACR 版本指引与控制台查询提示
func (a *AliyunProvider) ListVersions(ctx context.Context, categoryFilter string) error {
	fmt.Printf("[港位] 阿里云容器镜像服务 (ACR): %s\n", a.Repository())
	fmt.Println("💡 提示: 阿里云 ACR 个人版可通过固定 Tag 模式 (如 :latest) 查看最新镜像，或登录阿里云控制台镜像版本页面查看历史列表。")
	return nil
}

// MaintainQuota 执行阿里云 ACR 配额与生命周期策略维护 (支持固定 Tag 覆盖与控制台策略提示)
func (a *AliyunProvider) MaintainQuota(ctx context.Context, category string, retentionCount int, tag string, isFixedTag bool) error {
	if isFixedTag {
		fmt.Printf("  ✓ 固定 Tag 覆盖模式: 远端保持最新单版本 (%s)\n", tag)
	}
	return nil
}

// PruneUntagged 阿里云 ACR 提示跳过 GitHub 专属的 untagged 清理
func (a *AliyunProvider) PruneUntagged(ctx context.Context) (int, error) {
	fmt.Println("✓ 当前目标为阿里云 ACR 镜像注册表，跳过 GitHub 专属 untagged API 清理。")
	return 0, nil
}
