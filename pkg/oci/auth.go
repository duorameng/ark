package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// AuthManager 管理 OCI 注册表的 Bearer Token 协商与缓存
type AuthManager struct {
	mu         sync.RWMutex
	tokenCache map[string]string // key: "realm|service|scope" -> token
	username   string
	password   string
	httpClient *http.Client
}

// NewAuthManager 创建认证管理器
func NewAuthManager(username, password string, httpClient *http.Client) *AuthManager {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &AuthManager{
		tokenCache: make(map[string]string),
		username:   username,
		password:   password,
		httpClient: httpClient,
	}
}

// TokenResponse 标准 OAuth2 / Docker Registry Token 响应
type TokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
}

// GetTokenForScope 获取满足特定 scope 的有效 Bearer Token
func (a *AuthManager) GetTokenForScope(ctx context.Context, realm, service, scope string) (string, error) {
	return a.getTokenInternal(ctx, realm, service, scope, false)
}

// GetFreshTokenForScope 强制向鉴权服务器获取最新的 Bearer Token (遇到 401 挑战重试时使用)
func (a *AuthManager) GetFreshTokenForScope(ctx context.Context, realm, service, scope string) (string, error) {
	return a.getTokenInternal(ctx, realm, service, scope, true)
}

func (a *AuthManager) getTokenInternal(ctx context.Context, realm, service, scope string, forceRefresh bool) (string, error) {
	cacheKey := fmt.Sprintf("%s|%s|%s", realm, service, scope)
	if !forceRefresh {
		a.mu.RLock()
		if tok, ok := a.tokenCache[cacheKey]; ok && tok != "" {
			a.mu.RUnlock()
			return tok, nil
		}
		a.mu.RUnlock()
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if !forceRefresh {
		if tok, ok := a.tokenCache[cacheKey]; ok && tok != "" {
			return tok, nil
		}
	}

	u, err := url.Parse(realm)
	if err != nil {
		return "", fmt.Errorf("解析 auth realm 失败: %w", err)
	}

	q := u.Query()
	if service != "" {
		q.Set("service", service)
	}
	if scope != "" {
		q.Set("scope", scope)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", DefaultDockerUserAgent)
	req.Header.Set("Accept", "application/json")

	if a.password != "" {
		user := a.username
		if user == "" {
			user = "oauth2"
		}
		req.SetBasicAuth(user, a.password)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 auth realm 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("获取 token 失败 (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var tr TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("解析 token 响应失败: %w", err)
	}

	token := tr.Token
	if token == "" {
		token = tr.AccessToken
	}
	if token == "" {
		return "", fmt.Errorf("auth 响应未返回有效 token")
	}

	a.tokenCache[cacheKey] = token
	return token, nil
}

// GetCachedTokenForScope 查找与 scope 匹配且能满足权限动作的已有有效缓存 Token
func (a *AuthManager) GetCachedTokenForScope(scope string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// 1. 优先严格匹配
	for k, v := range a.tokenCache {
		parts := strings.SplitN(k, "|", 3)
		if len(parts) == 3 && parts[2] == scope && v != "" {
			return v
		}
	}

	// 2. 检查是否有权限超集能满足当前请求 (例如 cached 具备 pull,push，可满足 pull 或 push,pull 请求)
	for k, v := range a.tokenCache {
		parts := strings.SplitN(k, "|", 3)
		if len(parts) == 3 && v != "" {
			if ScopeSatisfies(parts[2], scope) {
				return v
			}
		}
	}

	return ""
}

// ScopeSatisfies 校验 cachedScope 是否能够涵盖 requiredScope 所需的权限动作
func ScopeSatisfies(cachedScope, requiredScope string) bool {
	if cachedScope == requiredScope {
		return true
	}
	if requiredScope == "" {
		return true
	}

	cParts := strings.Split(cachedScope, ":")
	rParts := strings.Split(requiredScope, ":")
	if len(cParts) < 3 || len(rParts) < 3 {
		return false
	}
	// 资源类型 (如 repository) 和资源路径 (如 engigu/ark) 必须完全一致
	if cParts[0] != rParts[0] || cParts[1] != rParts[1] {
		return false
	}

	// 校验缓存的 action 集合是否包含所需 action
	cachedActions := strings.Split(cParts[2], ",")
	actionMap := make(map[string]bool, len(cachedActions))
	for _, act := range cachedActions {
		actionMap[strings.TrimSpace(act)] = true
	}

	for _, reqAct := range strings.Split(rParts[2], ",") {
		if !actionMap[strings.TrimSpace(reqAct)] {
			return false
		}
	}

	return true
}

// ParseWwwAuthenticate 解析 401 响应的 Www-Authenticate: Bearer 头
// 严谨遵循 RFC 7235 / RFC 6750 规范，基于双引号状态机解析，避免将 scope="repository:x:pull,push" 内部的逗号误切
func ParseWwwAuthenticate(header string) (realm, service, scope string) {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(header, "Bearer ") {
		return "", "", ""
	}

	header = strings.TrimPrefix(header, "Bearer ")

	var currentKey, currentVal strings.Builder
	inKey := true
	inQuote := false

	for i := 0; i < len(header); i++ {
		ch := header[i]
		if inKey {
			if ch == '=' {
				inKey = false
			} else if ch != ' ' && ch != '\t' {
				currentKey.WriteByte(ch)
			}
		} else {
			if ch == '"' {
				inQuote = !inQuote
			} else if ch == ',' && !inQuote {
				assignAuthParam(currentKey.String(), currentVal.String(), &realm, &service, &scope)
				currentKey.Reset()
				currentVal.Reset()
				inKey = true
			} else {
				currentVal.WriteByte(ch)
			}
		}
	}
	if currentKey.Len() > 0 {
		assignAuthParam(currentKey.String(), currentVal.String(), &realm, &service, &scope)
	}
	return
}

func assignAuthParam(k, v string, realm, service, scope *string) {
	k = strings.ToLower(strings.TrimSpace(k))
	v = strings.Trim(strings.TrimSpace(v), "\"")
	switch k {
	case "realm":
		*realm = v
	case "service":
		*service = v
	case "scope":
		*scope = v
	}
}
