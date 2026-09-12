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
	cacheKey := fmt.Sprintf("%s|%s|%s", realm, service, scope)
	a.mu.RLock()
	if tok, ok := a.tokenCache[cacheKey]; ok && tok != "" {
		a.mu.RUnlock()
		return tok, nil
	}
	a.mu.RUnlock()

	a.mu.Lock()
	defer a.mu.Unlock()

	// Double check
	if tok, ok := a.tokenCache[cacheKey]; ok && tok != "" {
		return tok, nil
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

// GetCachedTokenForScope 查找与 scope 匹配的已有有效缓存 Token
func (a *AuthManager) GetCachedTokenForScope(scope string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	for k, v := range a.tokenCache {
		if strings.HasSuffix(k, "|"+scope) {
			return v
		}
	}
	// 若无严格后缀匹配，且缓存中存在单一 token，优先复用
	for _, v := range a.tokenCache {
		if v != "" {
			return v
		}
	}
	return ""
}

// ParseWwwAuthenticate 解析 401 响应的 Www-Authenticate: Bearer 头
func ParseWwwAuthenticate(header string) (realm, service, scope string) {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(header, "Bearer ") {
		return "", "", ""
	}

	header = strings.TrimPrefix(header, "Bearer ")
	parts := strings.Split(header, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		val := strings.Trim(strings.TrimSpace(kv[1]), "\"")
		switch key {
		case "realm":
			realm = val
		case "service":
			service = val
		case "scope":
			scope = val
		}
	}
	return
}
