package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"ark/pkg/timezone"
)

// VersionInfo GitHub Packages 容器版本结构
type VersionInfo struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Metadata  struct {
		Container struct {
			Tags []string `json:"tags"`
		} `json:"container"`
	} `json:"metadata"`
}

// Client GitHub REST API 交互客户端
type Client struct {
	Token      string
	Owner      string
	Package    string
	httpClient *http.Client
}

// NewClient 创建客户端实例
func NewClient(repo, token string) *Client {
	parts := strings.Split(repo, "/")
	owner := "duorameng"
	pkg := "ark"
	if len(parts) >= 3 {
		owner = parts[1]
		pkg = parts[2]
	} else if len(parts) == 2 {
		owner = parts[0]
		pkg = parts[1]
	}

	return &Client{
		Token:   token,
		Owner:   owner,
		Package: pkg,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// ListVersions 获取所有镜像版本 (支持全量自动分页)
func (c *Client) ListVersions() ([]VersionInfo, error) {
	allVersions := make([]VersionInfo, 0)
	page := 1

	for {
		url := fmt.Sprintf("https://api.github.com/user/packages/container/%s/versions?per_page=100&page=%d", c.Package, page)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			return nil, nil
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("GitHub API 响应异常 (HTTP %d): %s", resp.StatusCode, string(body))
		}

		var pageVersions []VersionInfo
		err = json.NewDecoder(resp.Body).Decode(&pageVersions)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		allVersions = append(allVersions, pageVersions...)
		if len(pageVersions) < 100 {
			break
		}
		page++
	}

	return allVersions, nil
}

// PruneCategoryVersions 根据分类与保留上限，按日期自动清理旧版本
func (c *Client) PruneCategoryVersions(category string, retentionCount int) error {
	if c.Token == "" || retentionCount <= 0 {
		return nil
	}

	allVersions, err := c.ListVersions()
	if err != nil {
		return err
	}

	prefix := category + "-"
	matched := make([]VersionInfo, 0)

	for _, v := range allVersions {
		isMatch := false
		for _, tag := range v.Metadata.Container.Tags {
			if strings.HasPrefix(tag, prefix) {
				isMatch = true
				break
			}
		}
		if isMatch {
			matched = append(matched, v)
		}
	}

	fmt.Printf("当前 [%s] 分类已记录航次: %d，保留上限: %d\n", category, len(matched), retentionCount)

	if len(matched) <= retentionCount {
		fmt.Println("✓ 泊位充足，无需清理旧航次。")
		return nil
	}

	// 按创建时间由新到旧排序
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})

	toDelete := matched[retentionCount:]
	for _, v := range toDelete {
		tags := strings.Join(v.Metadata.Container.Tags, ", ")
		fmt.Printf("-> 正在归档 [%s] 过期航次 ID: %d [Tags: %s] (创建于: %s)...\n",
			category, v.ID, tags, timezone.FormatDefault(v.CreatedAt))

		delURL := fmt.Sprintf("https://api.github.com/user/packages/container/%s/versions/%d", c.Package, v.ID)
		delReq, err := http.NewRequest("DELETE", delURL, nil)
		if err != nil {
			continue
		}
		delReq.Header.Set("Authorization", "Bearer "+c.Token)
		delReq.Header.Set("Accept", "application/vnd.github+json")

		delResp, err := c.httpClient.Do(delReq)
		if err == nil {
			delResp.Body.Close()
			if delResp.StatusCode == http.StatusNoContent || delResp.StatusCode == http.StatusOK {
				fmt.Println("   ✓ 归档指令已送达")
			}
		}
	}

	fmt.Printf("✓ [%s] 历史航次维护完毕 (其他分类完全不受影响)。\n", category)
	return nil
}

// PrintCategoryVersions 表格打印航次列表
func (c *Client) PrintCategoryVersions(categoryFilter string) error {
	versions, err := c.ListVersions()
	if err != nil {
		return err
	}

	if len(versions) == 0 {
		fmt.Println("[-] 当前仓库暂未查询到已记录的航次版本。")
		return nil
	}

	fmt.Println()
	fmt.Printf("%-10s %-28s %-24s %s\n", "分类", "航次标签 (Tag)", "创建日期 (UTC+8 / CST)", "版本 ID")
	fmt.Println(strings.Repeat("-", 80))

	count := 0
	for _, ver := range versions {
		for _, tag := range ver.Metadata.Container.Tags {
			if strings.Contains(tag, "-") && !strings.HasSuffix(tag, "-latest") {
				parts := strings.SplitN(tag, "-", 2)
				cat := parts[0]
				if categoryFilter == "" || categoryFilter == cat {
					fmt.Printf("%-10s %-28s %-24s %d\n",
						fmt.Sprintf("[%s]", cat),
						tag,
						timezone.FormatDefault(ver.CreatedAt),
						ver.ID,
					)
					count++
				}
			}
		}
	}
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("共找到 %d 个匹配的航次记录。\n", count)
	return nil
}

// GetLatestTag 自动从远端港口获取指定分类下创建时间最新的航次 Tag (无须依赖 latest 标签)
func (c *Client) GetLatestTag(category string) (string, error) {
	versions, err := c.ListVersions()
	if err != nil {
		return "", err
	}

	// 按创建时间降序排序 (最新的排前面)
	sort.SliceStable(versions, func(i, j int) bool {
		return versions[i].CreatedAt.After(versions[j].CreatedAt)
	})

	for _, ver := range versions {
		for _, tag := range ver.Metadata.Container.Tags {
			if strings.HasPrefix(tag, category+"-") && !strings.HasSuffix(tag, "-latest") {
				return tag, nil
			}
		}
	}

	return "", fmt.Errorf("远端港口暂未发现分类 [%s] 的任何历史航次", category)
}

