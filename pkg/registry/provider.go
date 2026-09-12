package registry

import (
	"context"

	"ark/pkg/oci"
)

// Provider 定义云端镜像注册表的流程行为接口。
// 基于通用 OCI Distribution Spec 规范，各云厂商（如 GitHub GHCR、阿里云 ACR、自建 Harbor 等）
// 实现该接口以承载其特定的鉴权、标签检索、配额轮转与清理行为。
type Provider interface {
	// ID 返回注册表专属英文标识符 (如 "github", "aliyun", "generic")
	ID() string

	// DisplayName 返回人类友好的展示名称 (如 "阿里云容器镜像服务 (ACR)")
	DisplayName() string

	// Host 返回注册表主机域名 (如 "ghcr.io", "registry.cn-hangzhou.aliyuncs.com")
	Host() string

	// Repository 返回绑定的完整镜像仓库地址 (如 "registry.cn-hangzhou.aliyuncs.com/my-ns/ark")
	Repository() string

	// Username 返回当前通道的登录用户名
	Username() string

	// MaskedUsername 返回经过安全脱敏处理的用户名
	MaskedUsername() string

	// Password 返回当前通道的登录密码或访问 Token
	Password() string

	// MaskedPassword 返回经过安全脱敏处理的密码/Token
	MaskedPassword() string

	// GetOCIClient 获取为该通道适配并完成鉴权封装的 OCI 客户端
	GetOCIClient(ctx context.Context) (*oci.Client, error)

	// ResolveLatestTag 智能检索或推导该分类下的最新航次标签
	ResolveLatestTag(ctx context.Context, category string) (string, error)

	// ListVersions 检索并打印该分类下的所有历史航次记录
	ListVersions(ctx context.Context, categoryFilter string) error

	// MaintainQuota 在航次推送成功后执行历史配额维护 (如版本淘汰、清理 untagged 等)
	MaintainQuota(ctx context.Context, category string, retentionCount int, tag string, isFixedTag bool) error

	// PruneUntagged 清理远端孤立未打标版本 (Untagged Versions)
	PruneUntagged(ctx context.Context) (int, error)

	// Check 执行连通性与通行凭据预检握手
	Check(ctx context.Context) error
}
