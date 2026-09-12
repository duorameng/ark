package main

import (
	"ark/cmd"
)

var (
	// Version 当前程序版本号 (支持构建时动态注入: -ldflags "-X main.Version=...")
	Version = "dev"
	// GitCommit 源码 Commit ID (支持构建时动态注入: -ldflags "-X main.GitCommit=...")
	GitCommit = "none"
	// BuildDate 编译构建日期 (支持构建时动态注入: -ldflags "-X main.BuildDate=...")
	BuildDate = "unknown"
)

func main() {
	cmd.Execute(Version, GitCommit, BuildDate)
}
