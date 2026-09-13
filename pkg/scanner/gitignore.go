package scanner

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// PathFilter 抽象路径过滤匹配器接口 (用于解耦 scanner, archive, hash 模块)
type PathFilter interface {
	ShouldIgnore(fullPath, cargoRoot string, isDir bool) bool
}

// GitIgnorePattern 单条 Git 忽略规则解析结构
type GitIgnorePattern struct {
	RawPattern string         // 原始规则文本
	Pattern    string         // 规范化后的路径表达式
	Negate     bool           // 是否为 ! 否定规则 (重新包含)
	DirOnly    bool           // 是否以 / 结尾 (仅匹配目录)
	Anchored   bool           // 是否锚定于根目录 (开头有 / 或中间包含 /)
	Regex      *regexp.Regexp // 编译后的确定性正则
}

// GitIgnoreMatcher 管理一组按序求值的 GitIgnore 规则集合
type GitIgnoreMatcher struct {
	patterns []*GitIgnorePattern
	scanRoot string // 扫描总目录 (用于绝对路径对齐)
	ws       string // 工作区目录
}

// NewGitIgnoreMatcher 创建 GitIgnore 规则匹配器
func NewGitIgnoreMatcher(ws, scanRoot string) *GitIgnoreMatcher {
	return &GitIgnoreMatcher{
		patterns: make([]*GitIgnorePattern, 0),
		scanRoot: filepath.ToSlash(filepath.Clean(scanRoot)),
		ws:       filepath.ToSlash(filepath.Clean(ws)),
	}
}

// ParseGitIgnorePattern 将单行文本解析为 GitIgnorePattern
func ParseGitIgnorePattern(line, scanRoot, ws string) *GitIgnorePattern {
	raw := line
	line = strings.TrimSpace(line)

	// 1. 空行或注释行忽略
	if line == "" || (strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "\\#")) {
		return nil
	}
	if strings.HasPrefix(line, "\\#") {
		line = line[1:]
	}

	negate := false
	if strings.HasPrefix(line, "!") {
		negate = true
		line = line[1:]
	}

	// 2. 统一斜杠
	line = filepath.ToSlash(line)

	// 3. 检查末尾是否仅限目录
	dirOnly := false
	if strings.HasSuffix(line, "/") {
		dirOnly = true
		line = strings.TrimSuffix(line, "/")
	}

	// 4. 绝对路径自适应对齐：
	// 如果用户传入主机绝对路径 (例如 /data/workspace/baihu/envs 或 C:/workspace/baihu/envs)
	// 自动剥离 scanRoot 或 ws 前缀，自适应转换为相对于根目录的锚定规则 /baihu/envs
	cleanLine := filepath.Clean(line)
	cleanScanRoot := filepath.Clean(scanRoot)
	cleanWs := filepath.Clean(ws)

	if scanRoot != "" && strings.HasPrefix(filepath.ToSlash(cleanLine), filepath.ToSlash(cleanScanRoot)) {
		rel, err := filepath.Rel(cleanScanRoot, cleanLine)
		if err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			line = "/" + filepath.ToSlash(rel)
		}
	} else if ws != "" && strings.HasPrefix(filepath.ToSlash(cleanLine), filepath.ToSlash(cleanWs)) {
		rel, err := filepath.Rel(cleanWs, cleanLine)
		if err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			line = "/" + filepath.ToSlash(rel)
		}
	}

	// 5. 锚定判断 (Git规范规则 6):
	// - 开头有 /：锚定根目录
	// - 中间有 /：锚定相对路径
	// - 无 /：匹配任意深度的同名文件/目录
	anchored := false
	if strings.HasPrefix(line, "/") {
		anchored = true
		line = strings.TrimPrefix(line, "/")
	} else if strings.Contains(line, "/") {
		anchored = true
	}

	re, err := compileGitPatternToRegex(line, dirOnly, anchored)
	if err != nil {
		return nil
	}

	return &GitIgnorePattern{
		RawPattern: raw,
		Pattern:    line,
		Negate:     negate,
		DirOnly:    dirOnly,
		Anchored:   anchored,
		Regex:      re,
	}
}

// compileGitPatternToRegex 将 gitignore 通配符转换为正则
func compileGitPatternToRegex(pattern string, dirOnly, anchored bool) (*regexp.Regexp, error) {
	var sb strings.Builder

	if anchored {
		sb.WriteString("^")
	} else {
		sb.WriteString("(?:^|.*/)")
	}

	i := 0
	n := len(pattern)
	for i < n {
		ch := pattern[i]
		if ch == '*' {
			if i+1 < n && pattern[i+1] == '*' {
				// 处理 "**"
				if i+2 < n && pattern[i+2] == '/' {
					// "**/" 匹配 0 或多级目录
					sb.WriteString("(?:.*/)?")
					i += 3
					continue
				} else if i > 0 && pattern[i-1] == '/' {
					// "/**" 匹配其下所有内容
					sb.WriteString(".*")
					i += 2
					continue
				} else {
					sb.WriteString(".*")
					i += 2
					continue
				}
			} else {
				// 单个 "*" 匹配本级内除 / 外的所有字符
				sb.WriteString("[^/]*")
				i++
				continue
			}
		} else if ch == '?' {
			sb.WriteString("[^/]")
			i++
			continue
		} else if ch == '[' {
			// 处理字符集 [...]
			j := i + 1
			if j < n && pattern[j] == '!' {
				j++
			}
			if j < n && pattern[j] == ']' {
				j++
			}
			for j < n && pattern[j] != ']' {
				j++
			}
			if j < n {
				classContent := pattern[i+1 : j]
				if strings.HasPrefix(classContent, "!") {
					sb.WriteString("[^" + classContent[1:] + "]")
				} else {
					sb.WriteString("[" + classContent + "]")
				}
				i = j + 1
				continue
			} else {
				sb.WriteString("\\[")
				i++
				continue
			}
		} else if ch == '\\' {
			if i+1 < n {
				sb.WriteString(regexp.QuoteMeta(string(pattern[i+1])))
				i += 2
				continue
			}
			sb.WriteString("\\\\")
			i++
			continue
		} else {
			sb.WriteString(regexp.QuoteMeta(string(ch)))
			i++
		}
	}

	// 匹配自身或作为目录前缀覆盖其下所有子孙路径
	sb.WriteString("(?:/.*)?$")

	return regexp.Compile(sb.String())
}

// Match 评估单个相对路径与目录属性是否命中此规则
func (p *GitIgnorePattern) Match(relPath string, isDir bool) bool {
	relPath = filepath.ToSlash(relPath)
	relPath = strings.TrimPrefix(relPath, "/")

	if relPath == "" {
		return false
	}

	// 若为目录限定规则 (DirOnly)
	if p.DirOnly {
		if isDir {
			return p.Regex.MatchString(relPath)
		}
		// 若当前检查的是普通文件，需检查该文件的某个祖先目录是否命中了此目录规则
		parts := strings.Split(relPath, "/")
		if len(parts) <= 1 {
			return false
		}
		for i := 1; i < len(parts); i++ {
			ancestor := strings.Join(parts[:i], "/")
			if p.Regex.MatchString(ancestor) {
				return true
			}
		}
		return false
	}

	// 普通规则：自身匹配即可
	return p.Regex.MatchString(relPath)
}

// AddRule 添加单条规则文本
func (m *GitIgnoreMatcher) AddRule(line string) {
	pat := ParseGitIgnorePattern(line, m.scanRoot, m.ws)
	if pat != nil {
		m.patterns = append(m.patterns, pat)
	}
}

// AddRules 批量添加规则文本列表
func (m *GitIgnoreMatcher) AddRules(lines []string) {
	for _, line := range lines {
		m.AddRule(line)
	}
}

// LoadDirRules 从指定目录读取 .arkignore 或 .gitignore 并加载规则
func (m *GitIgnoreMatcher) LoadDirRules(dir string) {
	if dir == "" {
		return
	}
	candidates := []string{
		filepath.Join(dir, ".arkignore"),
		filepath.Join(dir, ".gitignore"),
	}
	for _, f := range candidates {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			m.AddRule(scanner.Text())
		}
		// 只要优先找到一个文件即可
		break
	}
}

// Match 按照 Git 规范求值：自上而下匹配，最后命中的规则生效 (支持 ! 否定覆盖)
func (m *GitIgnoreMatcher) Match(relPath string, isDir bool) bool {
	if m == nil || len(m.patterns) == 0 {
		return false
	}

	ignored := false
	for _, p := range m.patterns {
		if p.Match(relPath, isDir) {
			if p.Negate {
				ignored = false
			} else {
				ignored = true
			}
		}
	}
	return ignored
}

// MatchCargoPath 对给定的文件完整路径进行全维度适配匹配 (实现 PathFilter 接口)
func (m *GitIgnoreMatcher) MatchCargoPath(fullPath, cargoRoot string, isDir bool) bool {
	if m == nil || len(m.patterns) == 0 {
		return false
	}

	fullClean := filepath.ToSlash(filepath.Clean(fullPath))
	cargoClean := filepath.ToSlash(filepath.Clean(cargoRoot))

	// 1. 相对 cargoRoot (例如: envs 或 envs/site.py)
	if cargoRoot != "" {
		if rel, err := filepath.Rel(cargoClean, fullClean); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			if m.Match(filepath.ToSlash(rel), isDir) {
				return true
			}
		}
	}

	// 2. 相对 scanRoot (例如: baihu/envs 或 baihu/envs/site.py)
	if m.scanRoot != "" {
		if rel, err := filepath.Rel(m.scanRoot, fullClean); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			if m.Match(filepath.ToSlash(rel), isDir) {
				return true
			}
		}
	}

	// 3. 相对 ws 工作区
	if m.ws != "" && m.ws != m.scanRoot {
		if rel, err := filepath.Rel(m.ws, fullClean); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			if m.Match(filepath.ToSlash(rel), isDir) {
				return true
			}
		}
	}

	// 4. 尝试直接以全路径或名称比对 (仅当非根目录自身时)
	if fullClean != m.scanRoot && fullClean != m.ws && fullClean != cargoClean {
		if m.Match(fullClean, isDir) {
			return true
		}
		baseName := filepath.Base(fullClean)
		if m.Match(baseName, isDir) {
			return true
		}
	}

	return false
}

// ShouldIgnore 实现 PathFilter 接口
func (m *GitIgnoreMatcher) ShouldIgnore(fullPath, cargoRoot string, isDir bool) bool {
	return m.MatchCargoPath(fullPath, cargoRoot, isDir)
}

// MatchWithReason 返回是否被忽略以及命中的原始规则
func (m *GitIgnoreMatcher) MatchWithReason(fullPath, cargoRoot string, isDir bool) (bool, string) {
	if m == nil || len(m.patterns) == 0 {
		return false, ""
	}

	fullClean := filepath.ToSlash(filepath.Clean(fullPath))
	cargoClean := filepath.ToSlash(filepath.Clean(cargoRoot))

	var hitPattern string
	ignored := false

	evalPath := func(rel string) {
		for _, p := range m.patterns {
			if p.Match(rel, isDir) {
				if p.Negate {
					ignored = false
					hitPattern = ""
				} else {
					ignored = true
					hitPattern = p.RawPattern
				}
			}
		}
	}

	// 1. 相对 cargoRoot
	if cargoRoot != "" {
		if rel, err := filepath.Rel(cargoClean, fullClean); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			evalPath(filepath.ToSlash(rel))
			if ignored {
				return true, hitPattern
			}
		}
	}

	// 2. 相对 scanRoot
	if m.scanRoot != "" {
		if rel, err := filepath.Rel(m.scanRoot, fullClean); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			evalPath(filepath.ToSlash(rel))
			if ignored {
				return true, hitPattern
			}
		}
	}

	// 3. 相对 ws
	if m.ws != "" && m.ws != m.scanRoot {
		if rel, err := filepath.Rel(m.ws, fullClean); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			evalPath(filepath.ToSlash(rel))
			if ignored {
				return true, hitPattern
			}
		}
	}

	// 4. 全路径或 baseName (仅当非根目录自身时)
	if fullClean != m.scanRoot && fullClean != m.ws && fullClean != cargoClean {
		evalPath(fullClean)
		if ignored {
			return true, hitPattern
		}
		evalPath(filepath.Base(fullClean))
		if ignored {
			return true, hitPattern
		}
	}

	return false, ""
}

// Count 返回当前已装载规则数量
func (m *GitIgnoreMatcher) Count() int {
	if m == nil {
		return 0
	}
	return len(m.patterns)
}
