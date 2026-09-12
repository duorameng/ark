package registry_test

import (
	"context"
	"strings"
	"testing"

	"ark/pkg/registry"
)

func TestNewProviderFactory(t *testing.T) {
	tests := []struct {
		key          string
		repo         string
		expectedType string
		expectedID   string
		expectedHost string
	}{
		{
			key:          "aliyun",
			repo:         "registry.cn-hangzhou.aliyuncs.com/testorg/app",
			expectedType: "*registry.AliyunProvider",
			expectedID:   "aliyun",
			expectedHost: "registry.cn-hangzhou.aliyuncs.com",
		},
		{
			key:          "acr",
			repo:         "myacr.aliyuncs.com/demo/ark",
			expectedType: "*registry.AliyunProvider",
			expectedID:   "aliyun",
			expectedHost: "myacr.aliyuncs.com",
		},
		{
			key:          "",
			repo:         "registry.cn-shanghai.aliyuncs.com/demo/backup",
			expectedType: "*registry.AliyunProvider",
			expectedID:   "aliyun",
			expectedHost: "registry.cn-shanghai.aliyuncs.com",
		},
		{
			key:          "github",
			repo:         "ghcr.io/example-org/repo",
			expectedType: "*registry.GitHubProvider",
			expectedID:   "github",
			expectedHost: "ghcr.io",
		},
		{
			key:          "ghcr",
			repo:         "example-org/repo",
			expectedType: "*registry.GitHubProvider",
			expectedID:   "github",
			expectedHost: "ghcr.io",
		},
		{
			key:          "",
			repo:         "ghcr.io/myorg/vps-backup",
			expectedType: "*registry.GitHubProvider",
			expectedID:   "github",
			expectedHost: "ghcr.io",
		},
		{
			key:          "harbor",
			repo:         "harbor.example.internal/project/app",
			expectedType: "*registry.GenericOCIProvider",
			expectedID:   "generic",
			expectedHost: "harbor.example.internal",
		},
		{
			key:          "",
			repo:         "docker.io/library/alpine",
			expectedType: "*registry.GenericOCIProvider",
			expectedID:   "generic",
			expectedHost: "docker.io",
		},
	}

	for _, tt := range tests {
		p := registry.NewProvider(tt.key, tt.repo, "mock_user", "mock_password")
		if p == nil {
			t.Fatalf("NewProvider(%q, %q) 返回了 nil", tt.key, tt.repo)
		}
		if p.ID() != tt.expectedID {
			t.Errorf("NewProvider(%q, %q).ID() = %s; 预期 %s", tt.key, tt.repo, p.ID(), tt.expectedID)
		}
		if p.Host() != tt.expectedHost {
			t.Errorf("NewProvider(%q, %q).Host() = %s; 预期 %s", tt.key, tt.repo, p.Host(), tt.expectedHost)
		}
	}
}

func TestMaskString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "(未配置)"},
		{"   ", "(未配置)"},
		{"alice@example.com", "ali****@example.com"},
		{"ab@test.org", "a****@test.org"},
		{"1234567890", "123****890"},
		{"12345", "12****"},
		{"123", "****"},
	}

	for _, tt := range tests {
		actual := registry.MaskString(tt.input)
		if actual != tt.expected {
			t.Errorf("MaskString(%q) = %q; 期望 %q", tt.input, actual, tt.expected)
		}
	}
}

func TestAliyunProviderBehaviors(t *testing.T) {
	repo := "registry.cn-hangzhou.aliyuncs.com/mockns/mockrepo"
	p := registry.NewAliyunProvider(repo, "mockuser@example.com", "mock_secret_pass_123")

	if p.ID() != "aliyun" {
		t.Errorf("预期 ID 'aliyun'，得到 %s", p.ID())
	}
	if !strings.Contains(p.DisplayName(), "阿里云") {
		t.Errorf("预期 DisplayName 包含 '阿里云'，得到 %s", p.DisplayName())
	}
	if p.Repository() != repo {
		t.Errorf("预期 Repository %s，得到 %s", repo, p.Repository())
	}

	ctx := context.Background()

	// 1. ResolveLatestTag
	tag, err := p.ResolveLatestTag(ctx, "vps")
	if err != nil || tag != "latest" {
		t.Errorf("ResolveLatestTag 失败: tag=%s, err=%v", tag, err)
	}

	// 2. PruneUntagged
	count, err := p.PruneUntagged(ctx)
	if err != nil || count != 0 {
		t.Errorf("PruneUntagged 失败: count=%d, err=%v", count, err)
	}

	// 3. MaintainQuota (固定 tag 模式)
	if err := p.MaintainQuota(ctx, "vps", 5, "latest", true); err != nil {
		t.Errorf("MaintainQuota (fixed) 返回错误: %v", err)
	}

	// 4. MaintainQuota (时间戳模式)
	if err := p.MaintainQuota(ctx, "vps", 5, "vps-20260912-120000", false); err != nil {
		t.Errorf("MaintainQuota (timestamp) 返回错误: %v", err)
	}

	// 5. ListVersions
	if err := p.ListVersions(ctx, "vps"); err != nil {
		t.Errorf("ListVersions 返回错误: %v", err)
	}
}

func TestGenericOCIProviderBehaviors(t *testing.T) {
	repo := "harbor.internal.net/ops/ark-backup"
	p := registry.NewGenericOCIProvider(repo, "robot$ops", "mock_robot_token_secret")

	if p.ID() != "generic" {
		t.Errorf("预期 ID 'generic'，得到 %s", p.ID())
	}
	if p.Repository() != repo {
		t.Errorf("预期 Repository %s，得到 %s", repo, p.Repository())
	}

	ctx := context.Background()

	tag, err := p.ResolveLatestTag(ctx, "vps")
	if err != nil || tag != "latest" {
		t.Errorf("ResolveLatestTag 失败: tag=%s, err=%v", tag, err)
	}

	count, err := p.PruneUntagged(ctx)
	if err != nil || count != 0 {
		t.Errorf("PruneUntagged 失败: count=%d, err=%v", count, err)
	}

	if err := p.MaintainQuota(ctx, "vps", 3, "latest", true); err != nil {
		t.Errorf("MaintainQuota 返回错误: %v", err)
	}

	if err := p.ListVersions(ctx, ""); err != nil {
		t.Errorf("ListVersions 返回错误: %v", err)
	}
}

func TestGitHubProviderProperties(t *testing.T) {
	repo := "ghcr.io/mockorg/mockrepo"
	p := registry.NewGitHubProvider(repo, "mockuser", "ghp_mocktoken1234567890")

	if p.ID() != "github" {
		t.Errorf("预期 ID 'github'，得到 %s", p.ID())
	}
	if !strings.Contains(p.DisplayName(), "GitHub") {
		t.Errorf("预期 DisplayName 包含 'GitHub'，得到 %s", p.DisplayName())
	}
	if p.Repository() != repo {
		t.Errorf("预期 Repository %s，得到 %s", repo, p.Repository())
	}
}
