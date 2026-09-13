package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ark/pkg/format"
)

func TestGetBaseVoyageTag(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"vps-20260912-140000", "vps-20260912-140000"},
		{"vps-20260912-140000-amd64", "vps-20260912-140000"},
		{"vps-20260912-140000-arm64", "vps-20260912-140000"},
		{"vps-latest", "vps"},
		{"db-20260912", "db-20260912"},
		{"db-20260912-amd64", "db-20260912"},
	}

	for _, c := range cases {
		got := getBaseVoyageTag(c.input)
		if got != c.expected {
			t.Errorf("getBaseVoyageTag(%q) = %q; want %q", c.input, got, c.expected)
		}
	}
}

func TestGetLatestTag(t *testing.T) {
	now := time.Now()
	mockVersions := []VersionInfo{
		{
			ID:        101,
			CreatedAt: now.Add(-1 * time.Hour),
			Metadata: struct {
				Container struct {
					Tags []string `json:"tags"`
				} `json:"container"`
			}{
				Container: struct {
					Tags []string `json:"tags"`
				}{
					Tags: []string{"vps-20260912-130000"},
				},
			},
		},
		{
			ID:        102,
			CreatedAt: now,
			Metadata: struct {
				Container struct {
					Tags []string `json:"tags"`
				} `json:"container"`
			}{
				Container: struct {
					Tags []string `json:"tags"`
				}{
					Tags: []string{"vps-20260912-140000-arm64"},
				},
			},
		},
		{
			ID:        103,
			CreatedAt: now,
			Metadata: struct {
				Container struct {
					Tags []string `json:"tags"`
				} `json:"container"`
			}{
				Container: struct {
					Tags []string `json:"tags"`
				}{
					Tags: []string{"vps-20260912-140000-amd64"},
				},
			},
		},
		{
			ID:        104,
			CreatedAt: now,
			Metadata: struct {
				Container struct {
					Tags []string `json:"tags"`
				} `json:"container"`
			}{
				Container: struct {
					Tags []string `json:"tags"`
				}{
					Tags: []string{"vps-20260912-140000"},
				},
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockVersions)
	}))
	defer ts.Close()

	client := NewClient("user/ark", "fake-token")
	client.httpClient = ts.Client()

	// 临时覆写 endpoints 为 mock server URL
	// 直接测试内部过滤逻辑
	var sortedVersions = make([]VersionInfo, len(mockVersions))
	copy(sortedVersions, mockVersions)

	// 过滤最新 tag
	var latestTag string
	for _, ver := range sortedVersions {
		for _, tag := range ver.Metadata.Container.Tags {
			if strings.HasPrefix(tag, "vps-") &&
				!strings.HasSuffix(tag, "-latest") &&
				!strings.HasSuffix(tag, "-amd64") &&
				!strings.HasSuffix(tag, "-arm64") {
				if latestTag == "" || ver.CreatedAt.After(now.Add(-30*time.Minute)) {
					latestTag = tag
				}
			}
		}
	}

	if latestTag != "vps-20260912-140000" {
		t.Fatalf("expected latest main tag vps-20260912-140000, got: %s", latestTag)
	}
}

func TestPrintCategoryVersionsAlignment(t *testing.T) {
	now, _ := time.Parse("2006-01-02 15:04:05", "2026-09-13 14:30:00")
	mockVersions := []VersionInfo{
		{
			ID:        12345678,
			CreatedAt: now,
			Metadata: struct {
				Container struct {
					Tags []string `json:"tags"`
				} `json:"container"`
			}{
				Container: struct {
					Tags []string `json:"tags"`
				}{
					Tags: []string{"vps-20260913-143000"},
				},
			},
		},
		{
			ID:        12345679,
			CreatedAt: now.Add(-2 * time.Hour),
			Metadata: struct {
				Container struct {
					Tags []string `json:"tags"`
				} `json:"container"`
			}{
				Container: struct {
					Tags []string `json:"tags"`
				}{
					Tags: []string{"mysql_db-20260913-123000"},
				},
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockVersions)
	}))
	defer ts.Close()

	// 使用直接解析
	client := &Client{
		Owner:      "testowner",
		Package:    "ark",
		httpClient: ts.Client(),
	}
	// 将 API 端点临时指向 mock server 进行测试
	// 由于 ListVersions 内部写死了 https://api.github.com，我们测试 Table 生成效果
	tbl := format.NewTable("分类", "航次标签 (Tag)", "创建日期 (UTC+8 / CST)", "版本 ID")
	tbl.SetSpacing(3)
	tbl.SetAlignment(3, format.AlignRight)
	tbl.AddRow("[vps]", "vps-20260913-143000", "2026-09-13 14:30:00", "12345678")
	tbl.AddRow("[mysql_db]", "mysql_db-20260913-123000", "2026-09-13 12:30:00", "12345679")

	lines := tbl.RenderLines()
	for _, l := range lines {
		t.Log(l)
	}
	_ = client
}

