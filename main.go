package main

import (
	"ark/cmd"
)

// Version 当前编译版本号 (支持编译时 -ldflags 动态注入)
var Version = "v1.1.0"

func main() {
	cmd.Execute(Version)
}
