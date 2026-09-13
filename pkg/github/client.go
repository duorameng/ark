package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"ark/pkg/format"
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

// ListVersions 获取所有镜像版本 (支持全量自动分页与多账号路径自适应探测)
func (c *Client) ListVersions() ([]VersionInfo, error) {
	endpoints := []string{
		"https://api.github.com/user/packages/container/%s/versions",
		"https://api.github.com/users/%s/packages/container/%s/versions",
		"https://api.github.com/orgs/%s/packages/container/%s/versions",
	}

	for _, epPattern := range endpoints {
		var ep string
		if strings.Count(epPattern, "%s") == 2 {
			ep = fmt.Sprintf(epPattern, c.Owner, c.Package)
		} else {
			ep = fmt.Sprintf(epPattern, c.Package)
		}

		allVersions := make([]VersionInfo, 0)
		page := 1
		success := false

		for {
			u := fmt.Sprintf("%s?per_page=100&page=%d", ep, page)
			req, err := http.NewRequest("GET", u, nil)
			if err != nil {
				break
			}
			req.Header.Set("Authorization", "Bearer "+c.Token)
			req.Header.Set("Accept", "application/vnd.github+json")

			resp, err := c.httpClient.Do(req)
			if err != nil {
				break
			}

			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
				resp.Body.Close()
				break
			}

			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				break
			}

			var pageVersions []VersionInfo
			err = json.NewDecoder(resp.Body).Decode(&pageVersions)
			resp.Body.Close()
			if err != nil {
				break
			}

			success = true
			allVersions = append(allVersions, pageVersions...)
			if len(pageVersions) < 100 {
				break
			}
			page++
		}

		if success {
			return allVersions, nil
		}
	}

	return nil, fmt.Errorf("无法获取仓库 %s/%s 的版本信息，请确认 GH_TOKEN 具备 packages 读写权限", c.Owner, c.Package)
}

// DeleteVersion 删除指定的 Package 版本
func (c *Client) DeleteVersion(versionID int64) error {
	urls := []string{
		fmt.Sprintf("https://api.github.com/user/packages/container/%s/versions/%d", c.Package, versionID),
		fmt.Sprintf("https://api.github.com/users/%s/packages/container/%s/versions/%d", c.Owner, c.Package, versionID),
		fmt.Sprintf("https://api.github.com/orgs/%s/packages/container/%s/versions/%d", c.Owner, c.Package, versionID),
	}

	var lastErr error
	for _, u := range urls {
		req, err := http.NewRequest("DELETE", u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
			return nil
		}
		if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusForbidden {
			lastErr = fmt.Errorf("删除版本失败 (HTTP %d)", resp.StatusCode)
		}
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("未找到对应版本或权限不足")
}

// PruneUntaggedVersions 扫描并清理所有未打标签 (untagged) 的孤立/悬空版本
func (c *Client) PruneUntaggedVersions() (int, error) {
	if c.Token == "" {
		return 0, fmt.Errorf("未配置 GitHub Token，无法清理远端悬空版本")
	}

	versions, err := c.ListVersions()
	if err != nil {
		return 0, err
	}

	deletedCount := 0
	for _, v := range versions {
		if len(v.Metadata.Container.Tags) == 0 {
			nameSnippet := v.Name
			if len(nameSnippet) > 19 {
				nameSnippet = nameSnippet[:19] + "..."
			}
			fmt.Printf("   -> 正在清理远端未打标孤立版本: ID %d (%s, 创建于: %s)...\n",
				v.ID, nameSnippet, timezone.FormatDefault(v.CreatedAt))
			if err := c.DeleteVersion(v.ID); err == nil {
				fmt.Printf("      ✓ 已成功删除\n")
				deletedCount++
			} else {
				fmt.Printf("      [!] 提示: 删除遇到问题: %v\n", err)
			}
		}
	}

	return deletedCount, nil
}

// getBaseVoyageTag 提取主航次标签名称 (去除 -amd64, -arm64, -latest 后缀)
func getBaseVoyageTag(tag string) string {
	t := strings.TrimSuffix(tag, "-amd64")
	t = strings.TrimSuffix(t, "-arm64")
	t = strings.TrimSuffix(t, "-latest")
	return t
}

type voyageGroup struct {
	baseTag   string
	createdAt time.Time
	versions  []VersionInfo
}

// PruneCategoryVersions 根据分类与保留上限，按日期自动清理旧版本 (支持多架构子标签聚合轮转)
func (c *Client) PruneCategoryVersions(category string, retentionCount int) error {
	if c.Token == "" || retentionCount <= 0 {
		return nil
	}

	allVersions, err := c.ListVersions()
	if err != nil {
		return err
	}

	prefix := category + "-"
	groupMap := make(map[string]*voyageGroup)

	for _, v := range allVersions {
		for _, tag := range v.Metadata.Container.Tags {
			if strings.HasPrefix(tag, prefix) {
				base := getBaseVoyageTag(tag)
				grp, exists := groupMap[base]
				if !exists {
					grp = &voyageGroup{
						baseTag:   base,
						createdAt: v.CreatedAt,
						versions:  make([]VersionInfo, 0),
					}
					groupMap[base] = grp
				}
				if v.CreatedAt.After(grp.createdAt) {
					grp.createdAt = v.CreatedAt
				}
				// 避免重复添加同一个 VersionInfo
				already := false
				for _, ev := range grp.versions {
					if ev.ID == v.ID {
						already = true
						break
					}
				}
				if !already {
					grp.versions = append(grp.versions, v)
				}
			}
		}
	}

	groups := make([]*voyageGroup, 0, len(groupMap))
	for _, grp := range groupMap {
		groups = append(groups, grp)
	}

	fmt.Printf("当前 [%s] 分类已记录航次: %d，保留上限: %d\n", category, len(groups), retentionCount)

	if len(groups) <= retentionCount {
		fmt.Println("✓ 泊位充足，无需清理旧航次。")
		return nil
	}

	// 按创建时间由新到旧排序
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].createdAt.After(groups[j].createdAt)
	})

	toDeleteGroups := groups[retentionCount:]
	for _, grp := range toDeleteGroups {
		fmt.Printf("-> 正在归档 [%s] 过期航次: %s (包含 %d 个平台版本清单, 交付于: %s)...\n",
			category, grp.baseTag, len(grp.versions), timezone.FormatDefault(grp.createdAt))

		for _, v := range grp.versions {
			tags := strings.Join(v.Metadata.Container.Tags, ", ")
			if err := c.DeleteVersion(v.ID); err == nil {
				fmt.Printf("   ✓ 已归档子项 ID %d [%s]\n", v.ID, tags)
			}
		}
	}

	fmt.Printf("✓ [%s] 历史航次维护完毕 (其他分类完全不受影响)。\n", category)
	return nil
}

// PrintCategoryVersions 表格打印航次列表 (自动感知 CJK 字符真实视觉列宽，保证 100% 垂直对齐)
func (c *Client) PrintCategoryVersions(categoryFilter string) error {
	versions, err := c.ListVersions()
	if err != nil {
		return err
	}

	if len(versions) == 0 {
		fmt.Println("[-] 当前仓库暂未查询到已记录的航次版本。")
		return nil
	}

	tbl := format.NewTable("分类", "航次标签 (Tag)", "创建日期 (UTC+8 / CST)", "版本 ID")
	tbl.SetSpacing(3)
	tbl.SetAlignment(3, format.AlignRight)

	count := 0
	for _, ver := range versions {
		for _, tag := range ver.Metadata.Container.Tags {
			if strings.Contains(tag, "-") && !strings.HasSuffix(tag, "-latest") {
				parts := strings.SplitN(tag, "-", 2)
				cat := parts[0]
				if categoryFilter == "" || categoryFilter == cat {
					tbl.AddRow(
						fmt.Sprintf("[%s]", cat),
						tag,
						timezone.FormatDefault(ver.CreatedAt),
						strconv.FormatInt(ver.ID, 10),
					)
					count++
				}
			}
		}
	}

	if count == 0 {
		if categoryFilter != "" {
			fmt.Printf("[-] 未找到分类为 [%s] 的航次记录。\n", categoryFilter)
		} else {
			fmt.Println("[-] 未找到符合条件的航次记录。")
		}
		return nil
	}

	fmt.Println()
	fmt.Print(tbl.Render())
	fmt.Printf("共找到 %d 个匹配的航次记录。\n", count)
	return nil
}

// GetLatestTag 自动从远端港口获取指定分类下创建时间最新的航次 Tag (无须依赖 latest 标签，优先匹配多架构主标签)
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
			if strings.HasPrefix(tag, category+"-") &&
				!strings.HasSuffix(tag, "-latest") &&
				!strings.HasSuffix(tag, "-amd64") &&
				!strings.HasSuffix(tag, "-arm64") {
				return tag, nil
			}
		}
	}

	return "", fmt.Errorf("远端港口暂未发现分类 [%s] 的任何历史航次", category)
}

