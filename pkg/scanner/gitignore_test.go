package scanner

import (
	"testing"
)

func TestGitIgnoreMatcher_Basic(t *testing.T) {
	matcher := NewGitIgnoreMatcher("/data/ark", "/data/workspace")

	// 1. 测试绝对路径自适应：/data/workspace/baihu/envs/ 转化为锚定 /baihu/envs/
	matcher.AddRule("/data/workspace/baihu/envs/")
	// 2. 测试任意深度目录：node_modules/
	matcher.AddRule("node_modules/")
	// 3. 测试文件通配：*.log
	matcher.AddRule("*.log")
	// 4. 测试否定重新包含：!important.log
	matcher.AddRule("!important.log")

	// 验证 1: baihu 目录本身不能被忽略
	if matcher.MatchCargoPath("/data/workspace/baihu", "/data/workspace", true) {
		t.Errorf("baihu directory itself should NOT be ignored")
	}

	// 验证 2: baihu/envs 目录必须被忽略
	if !matcher.MatchCargoPath("/data/workspace/baihu/envs", "/data/workspace/baihu", true) {
		t.Errorf("baihu/envs directory MUST be ignored")
	}

	// 验证 3: baihu/envs/bin/python 子文件必须被忽略
	if !matcher.MatchCargoPath("/data/workspace/baihu/envs/bin/python", "/data/workspace/baihu", false) {
		t.Errorf("file inside baihu/envs/ MUST be ignored")
	}

	// 验证 4: baihu 下的正常文件不被忽略
	if matcher.MatchCargoPath("/data/workspace/baihu/main.py", "/data/workspace/baihu", false) {
		t.Errorf("baihu/main.py should NOT be ignored")
	}

	// 验证 5: other/envs 目录不应被忽略 (因为规则是 /baihu/envs/)
	if matcher.MatchCargoPath("/data/workspace/other/envs", "/data/workspace/other", true) {
		t.Errorf("other/envs should NOT be ignored because rule is anchored to /baihu/envs/")
	}

	// 验证 6: node_modules/ 任意深度生效
	if !matcher.MatchCargoPath("/data/workspace/web/node_modules", "/data/workspace/web", true) {
		t.Errorf("web/node_modules should be ignored")
	}
	if !matcher.MatchCargoPath("/data/workspace/web/node_modules/package.json", "/data/workspace/web", false) {
		t.Errorf("web/node_modules/package.json should be ignored")
	}

	// 验证 7: *.log 与 !important.log
	if !matcher.MatchCargoPath("/data/workspace/baihu/debug.log", "/data/workspace/baihu", false) {
		t.Errorf("debug.log should be ignored")
	}
	if matcher.MatchCargoPath("/data/workspace/baihu/important.log", "/data/workspace/baihu", false) {
		t.Errorf("important.log should NOT be ignored due to ! negation")
	}
}

func TestGitIgnoreMatcher_DirOnlyVsFile(t *testing.T) {
	matcher := NewGitIgnoreMatcher("", "")
	matcher.AddRule("cache/")

	// 目录名为 cache 应该被忽略
	if !matcher.Match("cache", true) {
		t.Errorf("directory 'cache' should be ignored by 'cache/'")
	}

	// 文件名为 cache (普通文件) 不能被忽略
	if matcher.Match("cache", false) {
		t.Errorf("file 'cache' should NOT be ignored by 'cache/'")
	}

	// cache 目录下的文件应被忽略
	if !matcher.Match("cache/temp.txt", false) {
		t.Errorf("file 'cache/temp.txt' should be ignored because parent dir is ignored")
	}
}

func TestGitIgnoreMatcher_DoubleStar(t *testing.T) {
	matcher := NewGitIgnoreMatcher("", "")
	matcher.AddRule("a/**/b")

	if !matcher.Match("a/b", true) {
		t.Errorf("a/b should match a/**/b")
	}
	if !matcher.Match("a/x/b", true) {
		t.Errorf("a/x/b should match a/**/b")
	}
	if !matcher.Match("a/x/y/z/b", true) {
		t.Errorf("a/x/y/z/b should match a/**/b")
	}
	if matcher.Match("a/x/c", true) {
		t.Errorf("a/x/c should NOT match a/**/b")
	}
}
