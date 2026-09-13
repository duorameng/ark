# Ark 班轮货运管理终端 (Golang Single Binary Edition)

基于 OCI 标准协议与现代容器分层特性的轻量级、多架构云端工作负载快照交付与分布式工作区同步系统。

---

## 🌟 核心架构特性

1. **Golang 独立单二进制 (Zero External Dependencies)**：
   - 纯 Go 编写打包与安全加解密，免装 `tar`、`openssl`、`jq`、`curl`。
   - 编译为极简单一二进制文件（~6.3MB），跨平台即放即用。

2. **真正可运行微服务首层伪装 (Executable Camouflage Base Layer)**：
   - 镜像首层原生内置极简 Go 静态微服务底座（0 libc 依赖，双架构体积仅 ~900 KB），容器通过 `docker run -d -p 8080:8080 <image>` 可真正启动运行，并在 `:8080` 端口响应 HTTP 200 探针（支持 `/` 与 `/healthz`）。
   - 完整具备 `ExposedPorts (8080/tcp)`、`StopSignal (SIGTERM)` 与规范的 OCI 元数据 Labels，提供高度合规的云原生容器镜像特征。

3. **纯 Go 原生 OCI 直推引擎 (Zero-Docker Pipeline)**：
   - 默认抛弃 Docker 守护进程，直接基于 OCI Distribution Spec v1.1 与 Docker Registry v2 规范。
   - **内存单通道流式直推 (64KB Buffer)**：宿主机额外磁盘占用**严格为 0 字节**，完全杜绝传统构建对密闭数据块的无效二次 CPU 压缩。
   - **性能飞跃**：消灭 Build Context 传输 (55s) 与二次压缩干烧 (140s)，交付耗时从数分钟骤降至 20~30 秒（仅受限于实际网络带宽）。
   - **双架构原生索引 (linux/amd64 + linux/arm64)**：自动生成标准 OCI Image Index (Manifest List)，双架构共享数据 Layer（0 额外存储，0 额外流量）；Apple Silicon Mac、树莓派、ARM 云主机或 Intel/AMD 主机运行 `docker pull` 原生匹配，0 架构警告。
   - **子平台显式打标 (消除 Untagged 悬空显示)**：自动为多架构子清单打上 `<tag>-amd64` 与 `<tag>-arm64` 显式标签，消除容器注册表控制台散落的悬空无标签版本，同时支持按需精确调取单一架构版本。

4. **目录树高度内聚与 Dockerfile 历史严格统一 (`app/data/`)**：
   - 业务舱位在容器内部严格挂载于 `/app/data/<filename>`，与 Dockerfile History 中的 `COPY --chown=app:app <filename> /app/data/` 保持 100% 严密一致。
   - 镜像文件系统呈现极具规范的生产级微服务组织架构：`/app/server`（主运行底座）+ `/app/data/`（业务资源分层）。

5. **根级同级文件自动归集与专属 UUID 隔离 (Root Files Packaging)**：
   - 自动扫描工作区根目录下的零散配置文件与脚本（如 `docker-compose.yml`, `.env`, `nginx.conf` 等），并自动归集为专属舱位。
   - 使用固定全球唯一 UUID（`71c038f0-c62e-457b-9768-95b72e004fd4`）作为舱位 ID 与集装箱文件名，**彻底根绝与用户常规同名文件夹的任何命名或部署冲突**。
   - 自动分配最高优先级（`Priority: 10`），置于基础分层以实现高命中率复用；下船装配时直接原位展开至工作区根目录，0 多余嵌套层。

6. **数字 UID/GID 与文件权限严格保持 (Postgres 等服务无缝保障)**：
   - 纯 Go 通过底层系统调用原生提取与装配数字 UID/GID。
   - 彻底避免传统 `--owner=0 --group=0` 破坏容器运行用户权限（如 PostgreSQL `70:0`）的问题，装配落地后即可直接正常启动运行。

7. **全自动扫描探测与冷热变动率排序 (Smart Hot/Cold Sorting)**：
   - 支持一键扫描指定父目录（`./ark scan /root/workspace`），自动发现所有项目并评估变动频率（评分 10~90 分）。
   - **冷数据在前、热数据在后**：变动少的配置/静态数据排在基础层，高频变动的数据排在顶部，最大化分层缓存复用率。

8. **秒级 Tree Hash 状态感知（封条未动 0 耗时）**：
   - 基于多 Goroutine 并发树形哈希算法，极速计算货舱指纹。
   - 无变动目录显示 `[封条完好 ✓]`，完全跳过打包与加密过程，秒级完成就绪。

9. **班轮集装箱独立分层快照与 0 流量秒传 (HEAD Dedup)**：
   - 每个舱位封装为完全独立的集装箱 Snapshot Layer。
   - 直推前通过 HEAD 请求探测远端 Registry，未变动的舱位显示 `[远端已就绪 ✓] 0 流量秒传`，不耗费任何上传带宽。

10. **场景分类与不可变时间戳版本控制 (Category + Timestamp Tags)**：
    - 航次标签严格规范为：`{分类}-{年月日-时分秒}`（例如 `vps-20260912-140000`），**不生成任何 latest 标签**，确保每一航次均有确切不可变的时间戳版本。
    - 同一个镜像仓库可并行容纳多个独立业务场景（如 `vps`、`nas`、`db`），互不覆盖干扰。

11. **高强度端到端密闭封条 (End-to-End Encryption)**：
    - 默认启用与 OpenSSL 完全兼容的 AES-256-CBC + PBKDF2 (100,000 次哈希) 安全封条（`.dat` 密闭加密二进制块）。
    - 数据在离开本地机器前已完成强加密，远端注册表仅存储不可逆的密文块，确保即使在托管镜像仓库中亦能获得最高等级的数据私密性保障。

12. **全链路免 Docker 独立运行闭环 (Zero-Docker Voyage & Materialization)**：
    - 登船交付（`ark board`）、下船装配（`ark land`）与就地解包（`ark unpack`）均支持 100% 独立脱离 Docker 守护进程运行，任何基础 Linux/Windows 机器均可秒级落地。

---

## 目录结构

```text
ark/
├── ark                       # Golang 独立可执行程序 (Linux 64位)
├── ark.exe                   # Golang 独立可执行程序 (Windows 64位)
├── config.json               # [可选] 航海清运清单配置文件
├── .env                      # 部署环境配置文件 (包含凭证、密钥与工作区源/目标目录)
├── .env.example              # 部署环境配置范本
├── cache/                    # 集装箱与指纹缓存区 (git 忽略)
├── tmp/                      # 临时装配区 (git 忽略)
├── keys/                     # 封条密钥存放区 (git 忽略，请妥善异地保管 seal.key)
└── pkg/                      # 核心模块源码
    ├── oci/                  # 纯 Go 原生 OCI 直推/拉取客户端、伪装微服务底座嵌入与内存单层 Tar 封装
    ├── archive/              # 纯 Go Tar 打包/解包与 AES-256 密闭加解密 (含 UID/GID 保持)
    ├── config/               # 集中常量、清单配置、冷热排序与环境变量展开
    ├── docker/               # Docker BuildKit 传统备选引擎链路
    ├── github/               # 远端注册表 API 交互、航次查询与版本轮转清理
    ├── hash/                 # 多并发树形哈希算法
    └── scanner/              # 自动工程探测与规则打分引擎
```

---

## ⚡ 核心场景速查表

| 使用场景 | 推荐命令 / 操作方式 | 说明 |
| :--- | :--- | :--- |
| **免配置文件极速登船交付** | 配置 `.env` 中的 `ARK_SOURCE_DIR`，运行 `./ark board` | 零门槛，自动探测并分层装载推送 |
| **试运行演练 (Dry Run)** | `./ark dry` | 检查冷热排序、UUID 根同级文件层、Tree Hash 构型 |
| **即时扫描指定目录** | `./ark scan /path/to/project` | 智能分析工程并生成 `config.json` |
| **按天定时交付打标** | `./ark board day` 或 `./ark board --day` | 标签为 `vps-YYYYMMDD`，适合每日自动化任务 |
| **快捷交付至阿里云** | `./ark board ali` 或 `./ark board --to ali` | 自动切换至国内阿里云 ACR 通道极速直推 |
| **快捷交付至 GitHub** | `./ark board gh` 或 `./ark board --to gh` | 自动切换至 GitHub Packages (GHCR) 通道 |
| **多云双推异地多活交付** | `./ark board both` 或 `./ark board --both` | 一次打包，同时交付阿里云 (本地秒级调取) + GitHub (异地存盘) |
| **自定义航次保留配额** | `./ark board --keep 7` 或 `.env` 设 `ARK_RETENTION_COUNT=7` | 自动轮转保留指定个数，顺带清理远端 untagged |
| **多业务分类隔离交付** | `./ark board db day` | 独立分类 `db`，与 `vps` 互不干扰 |
| **小磁盘极致干净模式** | `./ark board --clean-all` | 推送后彻底清空 `cache/` 与临时文件，0 字节残留 |
| **调取最新航次落地到指定目录** | `./ark land -o /path/to/dest` | 自动检索云端最新版本并原位解包就绪 |
| **调取历史特定时间版本** | `./ark land vps-20260912-201922 -o /path/to/dest` | 精准拉取并切换至特定航次快照 |
| **免 config.json 全新节点部署** | `./ark land --key "口令" -o /path/to/dest` | 全新机器仅需单二进制和口令即可一键落地装配 |
| **本地离线快照批量解封** | `./ark unpack cache -o /path/to/dest` | 脱机直接装配 `cache/` 目录中所有集装箱 |
| **本地单个集装箱解封提取** | `./ark unpack cache/backend.dat -o /path/to/backend` | 仅展开特定业务模块 |
| **系统全方位健康体检** | `./ark doctor` 或 `./ark check` | 自检权限、`.env` 配置、密钥加密与网络连通闭环 |
| **清理远端悬空 untagged** | `./ark clean --untagged` | 自动扫描并清理镜像仓库散落的孤立无标签版本 |
| **程序自我一键升级** | `./ark update` | 自动检测官方发布并下载最新二进制替换自身 |

---

## 📋 全场景实战与使用方式示例

### 场景一：工作区数据源指定 (登船装载)

Ark 提供 3 种指定源路径的方式，满足从自动化运维到精细化配置的所有需求。

#### 1. 方式 A：通过 `.env` 指定（🌟 强烈推荐，免写 config.json）
在工作区 `.env` 中配置 `ARK_SOURCE_DIR`（亦兼容 `ARK_BACKUP_DIR`）：

- **单目录模式**（系统自动分析子目录，并将根下同级文件平铺打包）：
  ```env
  ARK_SOURCE_DIR="/root/workspace"
  ```
- **多目录模式**（支持逗号或分号分隔多个离散目录，系统自动构建多个独立舱位）：
  ```env
  ARK_SOURCE_DIR="/data/web, /var/lib/postgresql/data, /etc/nginx"
  ```

**执行命令**：
```bash
# 1. 试运行演练 (Dry Run)：查看舱位清单与 UUID 命名，不实际上传
./ark dry

# 2. 正式登船交付：系统自动读取 .env 中的路径并完成分层打包直推至远端注册表
./ark board
```

#### 2. 方式 B：通过 `ark scan` 命令行即时探测并固化
如果您希望对某个目录进行全自动冷热度分析，并将分析结果保存为 `config.json`：
```bash
# 扫描指定目录并按冷热度排序生成 config.json
./ark scan /root/workspace

# 若已在 .env 中配置了 ARK_SOURCE_DIR，直接执行不带参数亦可自动对标：
./ark scan
```
生成配置文件后，直接执行 `./ark board` 即可。

#### 3. 方式 C：在 `config.json` 中精细化配置（支持环境变量动态展开）
在 `config.json` 中配置每个舱位的路径，原生支持 `${ARK_SOURCE_DIR}`（或 `${ARK_BACKUP_DIR}`）动态语法：
```json
{
  "repository": "ghcr.io/duorameng/ark",
  "category": "vps",
  "retention_count": 5,
  "encrypt": true,
  "sources": [
    {
      "id": "71c038f0-c62e-457b-9768-95b72e004fd4",
      "name": "Root Files (根级配置与同级文件)",
      "path": "${ARK_SOURCE_DIR}",
      "files_only": true,
      "priority": 10
    },
    {
      "id": "backend",
      "name": "backend",
      "path": "${ARK_SOURCE_DIR}/backend",
      "priority": 30
    },
    {
      "id": "database",
      "name": "database",
      "path": "/data/postgres",
      "priority": 70
    }
  ]
}
```

---

### 场景二：多样化登船与推送控制 (`ark board`)

#### 1. 按周期与标签精度登船
```bash
# 默认秒级精度 (例如: vps-20260912-201922)
./ark board

# 按天生成标签 (例如: vps-20260912，适合每天跑一次的 Crontab 任务)
./ark board day
# 或使用标准选项:
./ark board --day    # 或 -d

# 按分钟精度生成标签 (例如: vps-20260912-2019)
./ark board --precision minute
```

#### 2. 多业务场景分类隔离 (Category)
同一个镜像仓库可以承载不同服务器或不同业务工作负载，彼此配额独立管理，互不干扰：
```bash
# 交付网站业务分类 (生成: web-20260912-201922)
./ark board web

# 交付数据库分类并按天打标 (生成: db-20260912)
./ark board db day
```

#### 3. 固定 Tag 覆盖模式 (免手动清理旧 Tag，适合单版本保鲜)
如果您不需要保留多份历史版本，只希望远端始终保持最新的一份快照并自动覆盖：
```bash
# 方式 A: 命令行参数临时指定
./ark board --latest          # 默认推送到 :latest，自动覆写上一版本
./ark board ali latest        # 直推阿里云 ACR 并覆写 :latest
./ark board both --latest     # 一次打包，双推至阿里云与 GitHub 覆盖 :latest

# 靠岸拉取部署固定最新版本
./ark land latest             # 从默认港位拉取 :latest
./ark land ali latest         # 从阿里云拉取 :latest 部署落地

# 方式 B: 在 .env 中持久化配置
ARK_TAG="latest"              # 配置后后续执行 ./ark board 默认覆盖 :latest
```

#### 4. 自定义固定版本标签
```bash
# 显式指定航次标签
./ark board --tag v1.0.0-release
```

#### 5. 小磁盘极致干净模式 (推送后全量重置)
推送完成后，不仅清理镜像构型，同时连同本地 `cache/` 缓存集装箱与临时文件也彻底清空，彻底重置为 0 字节初始状态：
```bash
./ark board --clean-all    # 别名: --purge / --reset
```

#### 6. 网络重试与交付引擎控制
```bash
# 网络偶发抖动时自动指数退避重试 5 次 (默认 3 次)
./ark board --retry 5      # 或 -r 5

# 切换为传统 Docker BuildKit 引擎 (默认: oci 纯 Go 原生引擎)
./ark board --engine=docker
```

---

### 场景三：落地装配与解封路径指定 (`ark land` 与 `ark unpack`)

工作区就绪包括两种途径：**从云端港口调取装配 (`ark land`)** 与 **本地离线快照解封 (`ark unpack`)**。

#### 1. 从云端港口调取装配 (`ark land`)

- **方式 1：使用 `-o` 或 `--dest` 标志（🌟 推荐，顺序任意）**
  ```bash
  # 自动检索云端最新航次并落地部署到 /root/target_workspace
  ./ark land -o /root/target_workspace

  # 调取指定分类 (如 db) 的最新航次到指定目录
  ./ark land db -o /data/postgres_data

  # 调取特定历史时间戳航次到指定目录
  ./ark land vps-20260912-201922 -o /root/target_workspace
  ```

- **方式 2：使用位置参数**
  ```bash
  # 格式: ./ark land <分类或具体标签> <目标目录>
  ./ark land vps /root/target_workspace
  ./ark land vps-20260912-201922 /root/target_workspace

  # 智能单参数：直接传入路径 (包含 / 或 \ 时系统智能识别为目标路径，分类自动取默认)
  ./ark land /root/target_workspace
  ```

- **方式 3：通过 `.env` 自动化落地**
  在 `.env` 中配置 `ARK_DEST_DIR`（亦兼容 `ARK_RESTORE_DIR`）：
  ```env
  ARK_DEST_DIR="/root/workspace"
  ```
  后续执行 `./ark land`（未显式提供路径时），全自动解包部署至该目录！

- **方式 4：全新节点免配置文件一键就地装配**
  在一台全新刚安装的服务器上，即使没有任何 `config.json` 或 `.env` 文件，只要一条命令即可完成全自动部署落地：
  ```bash
  ./ark land --repo ghcr.io/duorameng/ark --key "您的安全口令" -o /root/workspace
  ```

---

#### 2. 本地离线快照文件独立解封 (`ark unpack`)
在没有网络、不需要 Docker 环境的隔离机器上，直接利用本地 `.dat` / `.tar` 离线文件还原就绪：

```bash
# 批量解封整个 cache/ 目录中的所有货物至目标路径
./ark unpack cache -o /root/target_workspace
# 或位置参数:
./ark unpack cache /root/target_workspace

# 解封单个集装箱 (例如装配 postgres 数据，UID/GID 严格还原)
./ark unpack cache/backend.dat -o /root/target_workspace/backend

# 单独解封根级同级文件并原位平铺展开
./ark unpack cache/71c038f0-c62e-457b-9768-95b72e004fd4.dat -o /root/target_workspace
```

---

### 场景四：安全封条密钥管理 (`ark keygen`)

Ark 默认采用 AES-256-CBC + PBKDF2 高强度端到端加密，即使远端仓库完全公开，外界也只能看到密文：

```bash
# 方式 A (推荐)：指定自定义口令 (自动使用密码学哈希单向派生为工作密文，跨机器免拷贝密钥文件)
./ark keygen "MyVoyagePassword2026!#"

# 方式 B：自动生成 32 字节高强度真随机密钥
./ark keygen

# 方式 C：直接在 .env 中声明 (免交互全自动执行)
# ARK_SEAL_KEY="MyVoyagePassword2026!#"
```

---

### 场景五：港口航次查验 (`ark list`)

```bash
# 查看远端仓库的所有分类与历史航次清单
./ark list

# 仅检索指定分类 (例如 vps) 的历史航次
./ark list vps

# 配合管道提取最新航次名称
./ark list vps | head -n 5
```

---

### 场景六：系统配置与健康体检 (`ark doctor` / `ark check`)

在执行交付登船或部署落地前，随时运行诊断检查：
```bash
# 全面自检：检查工作区、磁盘权限、.env 配置、物理路径存在性、加密封条与云端凭据
./ark doctor    # 或 ./ark check

# 指定密钥测试端到端加密闭环
./ark doctor --key "MyVoyagePassword2026!#"
```

---

### 场景七：本地重置与远端悬空镜像清理 (`ark clean`)

```bash
# 1. 本地清运：一键清空 cache/ 缓存、tmp/ 临时文件与历史展开目录
./ark clean

# 2. 远端清理：自动扫描并清理镜像仓库中散落孤立的未打标 (untagged) 版本
./ark clean --untagged

# 3. 本地与远端全量彻底重置
./ark clean --all
```

---

### 场景八：程序自更新 (`ark update`)

Ark 具备内置自更新引擎，自带国内 CDN 镜像源故障自动转移（Failover）：

```bash
# 检查最新官方发布并自动平滑升级替换自身二进制
./ark update

# 查看当前程序版本号、Git Commit 与编译时间
./ark version
```

---

### 场景九：Shell 命令自动补全 (`ark completion`)

```bash
# 一键自动安装补全配置到当前用户环境 (~/.bashrc 或 ~/.zshrc)
./ark completion install

# 临时启用 (当前 Bash 会话):
source <(./ark completion bash)

# 临时启用 (当前 Zsh 会话):
source <(./ark completion zsh)

# 临时启用 (Windows PowerShell):
./ark.exe completion powershell | Out-String | Invoke-Expression
```

---

## ⏰ 定时自动化任务与生产级脚本 (Cron / Task Scheduler)

仓库内置了针对生产级 Linux VPS 与 Windows 环境深度优化的开箱即用自动化脚本：
- **Linux / VPS**: [`scripts/sync.sh`](file:///f:/workspace/ark/scripts/sync.sh) (亦兼容 `scripts/backup.sh`)
- **Windows**: [`scripts/sync.ps1`](file:///f:/workspace/ark/scripts/sync.ps1) (亦兼容 `scripts/backup.ps1`)

### 脚本核心亮点
1. **严格保留航次配额**：通过 `.env` 中的 `ARK_RETENTION_COUNT=5` 或命令行参数 `--keep 5` 自定义保留数量，超出上限时自动归档轮转旧航次；
2. **顺带彻底清除远端 Untagged**：每次交付成功后，自动顺带扫描并清理远端镜像仓库中由于轮转或旧推送残留的孤立未打标版本（`untagged`），杜绝远端镜像散落堆积；
3. **进程并发文件锁保护 (flock)**：网络偶发波动或耗时增加时，杜绝定时任务重复并发启动干烧；
4. **结构化时间戳日志与自动截断**：所有操作记录在 `logs/sync.log`，超过 5000 行自动滚动截断，保护 VPS 磁盘容量；
5. **小磁盘极致干净模式**：自动附带 `--clean-all`，推送后本地 0 缓存占用。

### Linux Crontab 定时配置示例 (每日凌晨 03:00 执行)
```bash
# 编辑定时任务
crontab -e

# 添加如下任务 (每天凌晨 03:00 自动执行脚本，带日志记录与防重叠锁):
0 3 * * * /root/ark/scripts/sync.sh >> /dev/null 2>&1
```

### Windows 任务计划程序注册示例 (每日凌晨 03:00 执行)
在 PowerShell 7 中运行以下命令直接注册任务：
```powershell
$Action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument "-ExecutionPolicy Bypass -File C:\ark\scripts\sync.ps1"
$Trigger = New-ScheduledTaskTrigger -Daily -At 3am
Register-ScheduledTask -TaskName "ArkDailySync" -Action $Action -Trigger $Trigger -Description "Ark 每日自动航次交付与远端维护" -User "SYSTEM"
```

---

## 🛠️ 源码构建与动态编译 (Build from Source)

Ark 支持在编译时通过 Go ldflags 动态注入版本号、Git Commit 与构建日期：

```bash
# Linux / macOS 原生极速构建
go build -ldflags "-s -w -X main.Version=v1.7.1 -X main.GitCommit=$(git rev-parse --short HEAD) -X main.BuildDate=$(date +%Y-%m-%d)" -o ark .

# Windows PowerShell 一键构建
& 'C:\Program Files\PowerShell\7\pwsh.exe' -Command "mise exec -- go build -ldflags '-s -w -X main.Version=v1.7.1' -o ark.exe ."
```
