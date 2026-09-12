package registry

import (
	"context"
	"fmt"
	"strings"

	"ark/pkg/github"
)

// GitHubProvider 实现 GitHub Packages (GHCR) 的特定流程与能力
type GitHubProvider struct {
	*BaseProvider
	ghClient *github.Client
}

// NewGitHubProvider 构造 GitHub Provider
func NewGitHubProvider(repository, username, token string) *GitHubProvider {
	if username == "" {
		username = "token" // GHCR 可通过任意用户名 + Token 登录
	}
	base := NewBaseProvider("github", "GitHub Packages (GHCR)", repository, username, token)
	return &GitHubProvider{
		BaseProvider: base,
		ghClient:     github.NewClient(repository, token),
	}
}

// Host 返回 GitHub 镜像注册表域名 (若未显式指定则默认 ghcr.io)
func (g *GitHubProvider) Host() string {
	parts := strings.Split(g.repository, "/")
	if len(parts) > 1 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":")) {
		return parts[0]
	}
	return "ghcr.io"
}

// ResolveLatestTag 通过 GitHub REST API 查询指定分类下的最新版本标签
func (g *GitHubProvider) ResolveLatestTag(ctx context.Context, category string) (string, error) {
	if g.password == "" {
		return "latest", nil
	}
	tag, err := g.ghClient.GetLatestTag(category)
	if err != nil {
		return "latest", err
	}
	return tag, nil
}

// ListVersions 通过 GitHub API 列出所有版本
func (g *GitHubProvider) ListVersions(ctx context.Context, categoryFilter string) error {
	if g.password == "" {
		return fmt.Errorf("未配置 GitHub Token，无法查询 GitHub Packages 历史版本")
	}
	return g.ghClient.PrintCategoryVersions(categoryFilter)
}

// MaintainQuota 执行 GitHub 历史版本淘汰与悬空 Untagged 清理
func (g *GitHubProvider) MaintainQuota(ctx context.Context, category string, retentionCount int, tag string, isFixedTag bool) error {
	fmt.Printf("\n------------------- 正在维护 [%s] 分类的历史航次配额 (%s) -------------------\n", category, g.DisplayName())
	if g.password == "" {
		fmt.Println("未提供通行凭据，跳过远端航次轮转与 untagged 清理维护。")
		return nil
	}

	if isFixedTag {
		fmt.Printf("✓ 当前采用固定 Tag 覆盖模式 (%s)，正在维护历史版本指针...\n", tag)
	} else if retentionCount > 0 {
		fmt.Printf("-> 正在按配额清理超出保留数 (%d 个) 的旧版本...\n", retentionCount)
		_ = g.ghClient.PruneCategoryVersions(category, retentionCount)
	}

	fmt.Println("-> 正在顺带扫描并清理远端未打标孤立版本 (Untagged Versions)...")
	deletedUntagged, errUntagged := g.ghClient.PruneUntaggedVersions()
	if errUntagged != nil {
		fmt.Printf("   [-] 清理远端未打标版本提示: %v\n", errUntagged)
	} else if deletedUntagged > 0 {
		fmt.Printf("   ✓ 顺带成功清理了 %d 个远端孤立 untagged 版本！\n", deletedUntagged)
	} else {
		fmt.Println("   ✓ 远端未发现任何孤立 untagged 版本，状态清洁。")
	}
	return nil
}

// PruneUntagged 扫描并清理孤立 untagged 版本
func (g *GitHubProvider) PruneUntagged(ctx context.Context) (int, error) {
	if g.password == "" {
		return 0, fmt.Errorf("未配置 GitHub Token，无法清理远端 untagged 版本")
	}
	return g.ghClient.PruneUntaggedVersions()
}
