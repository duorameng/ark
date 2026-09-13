package oci

import (
	"testing"
)

func TestParseWwwAuthenticate(t *testing.T) {
	tests := []struct {
		name        string
		header      string
		wantRealm   string
		wantService string
		wantScope   string
	}{
		{
			name:        "Aliyun ACR with comma in scope",
			header:      `Bearer realm="https://dockerauth.cn-hangzhou.aliyuncs.com/auth",service="registry.aliyuncs.com:cn-hangzhou:26842",scope="repository:engigu/ark:push,pull",error="insufficient_scope"`,
			wantRealm:   "https://dockerauth.cn-hangzhou.aliyuncs.com/auth",
			wantService: "registry.aliyuncs.com:cn-hangzhou:26842",
			wantScope:   "repository:engigu/ark:push,pull",
		},
		{
			name:        "Standard Docker Hub / GHCR challenge",
			header:      `Bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:duorameng/ark:pull,push"`,
			wantRealm:   "https://ghcr.io/token",
			wantService: "ghcr.io",
			wantScope:   "repository:duorameng/ark:pull,push",
		},
		{
			name:        "Empty scope in root /v2/ probe",
			header:      `Bearer realm="https://dockerauth.cn-hangzhou.aliyuncs.com/auth",service="registry.aliyuncs.com:cn-hangzhou:26842"`,
			wantRealm:   "https://dockerauth.cn-hangzhou.aliyuncs.com/auth",
			wantService: "registry.aliyuncs.com:cn-hangzhou:26842",
			wantScope:   "",
		},
		{
			name:        "Unquoted parameters",
			header:      `Bearer realm=https://auth.example.com/v2,service=registry.local,scope=repository:test:pull`,
			wantRealm:   "https://auth.example.com/v2",
			wantService: "registry.local",
			wantScope:   "repository:test:pull",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			realm, service, scope := ParseWwwAuthenticate(tt.header)
			if realm != tt.wantRealm {
				t.Errorf("realm = %q, want %q", realm, tt.wantRealm)
			}
			if service != tt.wantService {
				t.Errorf("service = %q, want %q", service, tt.wantService)
			}
			if scope != tt.wantScope {
				t.Errorf("scope = %q, want %q", scope, tt.wantScope)
			}
		})
	}
}

func TestScopeSatisfies(t *testing.T) {
	tests := []struct {
		cached   string
		required string
		want     bool
	}{
		{"repository:a/b:pull,push", "repository:a/b:pull,push", true},
		{"repository:a/b:pull,push", "repository:a/b:push,pull", true},
		{"repository:a/b:pull,push", "repository:a/b:pull", true},
		{"repository:a/b:pull,push", "repository:a/b:push", true},
		{"repository:a/b:pull", "repository:a/b:pull,push", false},
		{"repository:a/b:push", "repository:a/b:pull,push", false},
		{"repository:a/b:pull,push", "repository:other/repo:pull", false},
		{"repository:a/b:pull,push", "", true},
	}

	for _, tt := range tests {
		got := ScopeSatisfies(tt.cached, tt.required)
		if got != tt.want {
			t.Errorf("ScopeSatisfies(%q, %q) = %v, want %v", tt.cached, tt.required, got, tt.want)
		}
	}
}

func TestAuthManagerCacheIsolation(t *testing.T) {
	auth := NewAuthManager("user", "pass", nil)

	// 缓存一个只读 token
	auth.tokenCache["realm|service|repository:engigu/ark:pull"] = "token-pull-only"

	// 1. 请求 pull 权限，期望返回只读 token
	if tok := auth.GetCachedTokenForScope("repository:engigu/ark:pull"); tok != "token-pull-only" {
		t.Errorf("期望命中只读 token，实际: %q", tok)
	}

	// 2. 请求 pull,push 权限，期望坚决不复用只读 token
	if tok := auth.GetCachedTokenForScope("repository:engigu/ark:pull,push"); tok != "" {
		t.Errorf("期望权限不足时不复用，实际错误返回: %q", tok)
	}

	// 3. 缓存读写 token
	auth.tokenCache["realm|service|repository:engigu/ark:pull,push"] = "token-pull-and-push"

	// 4. 再次请求 pull,push 期望命中
	if tok := auth.GetCachedTokenForScope("repository:engigu/ark:pull,push"); tok != "token-pull-and-push" {
		t.Errorf("期望命中读写 token，实际: %q", tok)
	}

	// 5. 请求 push,pull (语序颠倒) 期望仍能通过超集匹配命中
	if tok := auth.GetCachedTokenForScope("repository:engigu/ark:push,pull"); tok != "token-pull-and-push" {
		t.Errorf("期望通过超集满足 push,pull，实际: %q", tok)
	}
}
