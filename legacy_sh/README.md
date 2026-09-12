# 历史 Shell 脚本封存区 (Legacy Shell Scripts)

本目录封存了 Ark 第一阶段的 Shell 脚本实现，供后续设计与逻辑参照使用。
目前系统已全面升级为纯 Go 单二进制实现（参见根目录 `main.go` 与 `pkg/`）。

## 文件对照表

| 脚本文件 | 原对应功能 | 现 Golang 单二进制对应命令 |
| :--- | :--- | :--- |
| `board.sh` | 登船：打包、AES-256 加密、BuildKit 构建、推送、分类版本轮转清理 | `./ark 登船` 或 `./ark board` |
| `land.sh` | 下船：从 GHCR 调取班轮镜像、导出容器内货物、解密解包恢复数据 | `./ark 下船` 或 `./ark land` |
| `scan.sh` | 扫描：遍历总目录、计算变动率冷热度评分、智能排序生成配置 | `./ark 扫描` 或 `./ark scan` |
| `list.sh` | 查验：按分类过滤检索 GHCR 港口历史航次列表与创建日期 | `./ark 查验` 或 `./ark list` |
