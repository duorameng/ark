# Ark 班轮货运管理终端 (Golang Single Binary Edition)

基于 Docker BuildKit `COPY --link` 独立分层快照特性与 GitHub Packages (GHCR) 的零依赖、隐蔽化班轮装载、登船与下船管理系统。

---

## 核心特性

1. **Golang 独立单二进制 (Zero External Dependencies)**：
   - 纯 Go 编写打包与加解密，免装 `tar`、`openssl`、`jq`、`curl`。
   - 编译为极简单一二进制文件（~6.3MB），跨平台即放即用。

2. **纯 Go 原生 OCI 直推引擎 (Zero-Docker Pipeline)**：
   - 默认抛弃 Docker 守护进程，直接基于 OCI Distribution Spec v1.1 与 Docker Registry v2 协议。
   - **内存单通道流式直推 (64KB Buffer)**：宿主机额外磁盘占用**严格为 0 字节**，完全杜绝 Docker BuildKit 对已加密 `.dat` 密文的无效二次 CPU 压缩。
   - **性能飞跃**：消灭 Build Context 传输 (55s) 与二次压缩干烧 (140s)，耗时直接从近 4 分钟骤降至 20~30 秒（仅受限于上传带宽）。
   - **双架构原生索引 (linux/amd64 + linux/arm64)**：自动生成标准 OCI Image Index (Manifest List)，双架构共享数据 Layer（0 额外存储，0 额外流量）；Apple Silicon Mac、树莓派、甲骨文 ARM VPS 或 Intel/AMD 主机运行 `docker pull` 原生匹配，0 架构警告。
   - **子平台显式打标 (消除 Untagged 悬空显示)**：自动为多架构子清单打上 `<tag>-amd64` 与 `<tag>-arm64` 标签，消除 GitHub Packages 网页端散落裸露的 untagged 悬空版本条目，支持按需精确拉取单一架构版本。
   - **双向平滑引擎选择**：默认启用 OCI 原生直推引擎，保留 `--engine=docker` 作为传统备选。

3. **数字 UID/GID 与文件权限严格保持 (Postgres 等服务无缝保障)**：
   - 纯 Go 通过底层系统调用原生提取与还原数字 UID/GID。
   - 彻底避免传统 `--owner=0 --group=0` 破坏容器运行用户权限（如 PostgreSQL `70:0`）的问题，恢复即可直接正常启动运行。

4. **全自动扫描探测与冷热变动率排序 (Smart Hot/Cold Sorting)**：
   - 支持一键扫描指定父目录（`./ark 扫描 /root/workspace`），自动发现所有项目并评估变动频率（评分 10~90 分）。
   - **冷数据在前、热数据在后**：变动少的配置/静态数据排在基础镜像层，高频变动的数据排在顶部，最大化镜像层与本地 Tree Hash 缓存复用率。

5. **秒级 Tree Hash 状态感知（封条未动 0 耗时）**：
   - 基于多 Goroutine 并发树形哈希算法，极速计算货舱指纹。
   - 无变动目录显示 `[封条完好 ✓]`，完全跳过打包与加密过程，秒级完成就绪。

6. **班轮集装箱独立分层快照与 0 流量秒传 (HEAD Dedup)**：
   - 每个舱位封装为完全独立的集装箱 Snapshot Layer。
   - 直推前通过 HEAD 请求探测远端 Registry，未变动的舱位显示 `[远端已就绪 ✓] 0 流量秒传`，不耗费任何上传带宽。

7. **场景分类与日期精确版本控制 (Category + Timestamp Tags)**：
   - 航次标签严格规范为：`{分类}-{年月日-时分秒}`（例如 `vps-20260912-140000`），**不生成任何 latest 标签**，确保每一航次均有确切不可变的时间戳版本。
   - 同一个镜像仓库（`ghcr.io/duorameng/ark`）可并行容纳多个独立业务场景（如 `vps`、`nas`、`db`），互不覆盖干扰。

8. **分类作用域历史轮转 (Category-Scoped Pruning)**：
   - 自动轮转历史航次时，仅筛选匹配当前 `{分类}-*` 的航次进行保留数控制，绝不误触其他分类。

9. **高强度密闭封条 (适配免费公开港口)**：
   - 默认启用与 OpenSSL 完全兼容的 AES-256-CBC + PBKDF2 (100,000 次哈希) 安全封条（`.dat` 密闭二进制块）。
   - 仓库完全公开，外界看到的也只是加密二进制块；享受 GitHub 公开包**永久免费、无限存储、无限流量**。

10. **全链路免 Docker 独立灾备闭环 (Zero-Docker Voyage & Restore)**：
   - 登船（`ark board`）、下船（`ark land`）与就地解包（`ark unpack`）均支持 100% 独立脱离 Docker 守护进程运行，任何基础 Linux/Windows 机器均可秒级还原。

---

## 目录结构

```
ark/
├── ark                       # Golang 独立可执行程序 (Linux 64位)
├── ark.exe                   # Golang 独立可执行程序 (Windows 64位)
├── config.json               # 航海清运清单配置文件
├── .env                      # [可选] 港口通行凭据 GH_TOKEN
├── cache/                    # 集装箱与指纹缓存区 (git 忽略)
├── tmp/                      # 临时装配区 (git 忽略)
├── keys/                     # 封条密钥存放区 (git 忽略，请妥善异地备份 seal.key)
└── pkg/                      # 核心模块源码
    ├── oci/                  # 纯 Go 原生 OCI 直推/拉取客户端、Token 自动协商与内存单层 Tar 封装
    ├── archive/              # 纯 Go Tar 打包/解包与 AES-256 密闭加解密 (含 UID/GID 保持)
    ├── config/               # 清单配置与冷热优先级排序
    ├── docker/               # Docker BuildKit 传统备选引擎链路
    ├── github/               # GHCR 远端港口航次查询与分类轮转清理
    ├── hash/                 # 多并发树形哈希算法
    └── scanner/              # 自动工程探测与变动率评分引擎
```

---

## 命令行操作指南

### 1. 系统与配置自检体检 (Diagnostic Check)
```bash
# 全面扫描测试当前所有配置、JSON语法、货舱物理路径、加密封条与云端凭据:
ark check

# 支持指定自定义口令进行端到端闭环加密/解密往返自测:
ark check --key "MySecretPass"

# 亦可使用通用别名:
ark doctor  # 或 ark test
```

### 2. 封条密钥管理与自定义口令 (Key Management)
```bash
# 方式 A (推荐)：指定自定义口令（磁盘自动使用密码学哈希非明文存储，跨机器还原免拷贝文件）:
ark keygen "MySecretPass123"

# 方式 B：自动生成 32 字节高强度真随机密钥:
ark keygen

# 提示: 在任意命令中均可直接通过 --key 参数指定口令 (例如免配置直接恢复):
ark land vps --key "MySecretPass123"
```

### 3. 全自动扫描与清单生成 (Auto Scan & Rank)
```bash
# 全自动探测总目录下所有子工程，按冷热度排序并写入 config.json
ark scan /root/workspace
```

### 4. 登船推送 (Ship Cargo)
```bash
# 模拟试航 (DRY RUN): 验证哈希对比、打包加密与 OCI Manifest 构型，不实际上传
ark dry

# 默认登船: 默认启用纯 Go 原生 OCI 极速直推引擎 (本地 0 额外落盘，0 无效压缩)
ark board

# 快捷按天生成航次标签 (如 vps-20260912，适合每日定时备份)
ark board day
# 或使用参数: ark board --day (或 -d)

# 指定交付引擎 (默认: oci; 亦可指定使用传统 docker buildkit 引擎)
ark board --engine=oci      # 纯 Go 原生直推 (免 Docker daemon, 速度最快)
ark board --engine=docker   # 传统 Docker BuildKit 链路

# 指定分类为 db 并按天生成航次标签 (生成: db-20260912)
ark board db day

# 指定时间精度为分 (如 vps-20260912-1533)
ark board --precision minute

# 指定推送失败重试次数 (默认 3 次，指数退避防网络抖动)
ark board --retry 5    # 或简写: -r 5

# 组合使用: 指定分类为 db、按天生成标签、失败重试 5 次
ark board db day --retry 5

# 全量极致干净模式 (推送完成后自动彻底清空本地 cache 与临时文件，恢复 0 字节初始状态):
ark board nd3 day --clean-all   # 或使用别名: --purge / --reset

# 亦可在 config.json 中永久配置默认精度与清理策略:
# "tag_precision": "day", "push_retry": 5, "clean_all_after_push": true
```

### 5. 查验港口航次 (List Voyages)
```bash
# 查验港口所有航次
ark list

# 仅检索指定分类 (例如 vps) 的历史航次
ark list vps
```

### 6. 下船还原货物 (Restore Cargo)
```bash
# 卸载指定分类的最新班次 (默认使用 OCI 原生流式调取，无 Docker 机器亦可秒级还原):
ark land vps

# 亦可强制指定调取引擎:
ark land vps --engine=oci
ark land vps --engine=docker

# 卸载指定日期的历史班次到指定目录:
ark land vps-20260912-140000 /root/workspace/restored_vps
```

### 7. 独立解封 (Zero-Docker Restore)
```bash
# 直接解封单个集装箱 (例如恢复 postgres 数据，UID/GID 严格还原为 70:0)
ark unpack cache/postgres.dat /root/workspace/pg_restored

# 批量解封 cache 目录下所有货物
ark unpack cache /root/workspace/all_restored
```

### 8. 本地工作区重置与环境清理 (Clean & Reset)
```bash
# 一键清空 cache/ 缓存、tmp/ 临时文件、历史落地货物目录，深度释放 Docker 悬空层与 BuildKit 缓存，恢复初始状态:
ark clean

# 自动扫描并清理远端 GitHub Packages 孤立/悬空的未打标 (untagged) 版本:
ark clean --untagged

# 连同本地历史关联的 Docker 镜像一并清理:
ark clean --docker

# 全量彻底重置 (本地工作区 + Docker 镜像 + 远端所有 untagged 孤儿版本):
ark clean --all
```

### 9. 自我升级 (Self-Update with CDN Failover)
```bash
# 自动检测 GitHub 最新 Release 并通过国内镜像源自动容灾重试下载更新自身:
ark update

# 查看当前程序版本:
ark version

# 从自定义 URL 直接下载最新二进制替换自身:
ark update https://github.com/duorameng/ark/releases/download/v1.0.0/ark-linux-amd64
```

### 10. Shell 自动补全 (Auto-Completion)
```bash
# 一键自动安装补全到当前 Shell 配置文件 (~/.bashrc 或 ~/.zshrc):
ark completion install

# 或在当前会话临时启用 (Bash):
source <(ark completion bash)

# 或在当前会话临时启用 (Zsh):
source <(ark completion zsh)

# 或在当前会话临时启用 (PowerShell):
ark completion powershell | Out-String | Invoke-Expression
```

---

## 自动定时班轮 (Crontab)

使用 `crontab -e` 配置每日凌晨 3:00 自动登船出海：
```bash
0 3 * * * cd /root/workspace/ark && ./ark board >> /var/log/ark_voyage.log 2>&1
```

---

## 动态编译构建 (Build with Dynamic Version)

Ark 支持在构建时通过 Go 链接器参数动态注入版本号、Git Commit 与构建时间：

```bash
# 1. 快速构建指定版本 (Linux)
go build -ldflags "-s -w -X main.Version=v1.2.0 -X main.GitCommit=$(git rev-parse --short HEAD) -X main.BuildDate=$(date +%Y-%m-%d)" -o ark .

# 2. 本地使用 PowerShell 脚本一键构建全平台二进制
.\tmp\build.ps1 -Version "v1.2.0"
```



