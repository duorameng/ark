package registry

import (
	"strings"
)

// NewProvider 根据配置的目标标识符与仓库地址智能实例化对应的 Provider
func NewProvider(key, repository, username, password string) Provider {
	lowerKey := strings.ToLower(strings.TrimSpace(key))
	lowerRepo := strings.ToLower(strings.TrimSpace(repository))

	switch {
	case lowerKey == "aliyun" || lowerKey == "ali" || lowerKey == "acr" || strings.Contains(lowerRepo, "aliyuncs.com"):
		return NewAliyunProvider(repository, username, password)

	case lowerKey == "github" || lowerKey == "gh" || lowerKey == "ghcr" || strings.HasPrefix(lowerRepo, "ghcr.io") || strings.Contains(lowerRepo, "github"):
		return NewGitHubProvider(repository, username, password)

	default:
		return NewGenericOCIProvider(repository, username, password)
	}
}
