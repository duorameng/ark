# Ark 班轮货运管理终端 (Golang Single Binary Edition)

基于 Docker BuildKit `COPY --link` 独立分层快照特性与 GitHub Packages (GHCR) 的零依赖、隐蔽化班轮装载、登船与下船管理系统。

---

## 核心特性

1. **Golang 独立单二进制 (Zero External Dependencies)**：
   - 纯 Go 编写打包与加解密，免装 `tar`、`openssl`、`jq`、`curl`。
   - 编译为极简单一二进制文件（~6.3MB），跨平台即放即用。

2. **数字 UID/GID 与文件权限严格保持 (Postgres 等服务无缝保障)**：
   - 纯 Go 通过底层系统调用原生提取与还原数字 UID/GID。
   - 彻底避免传统 `--owner=0 --group=0` 破坏容器运行用户权限（如 PostgreSQL `70:0`）的问题，恢复即可直接正常启动运行。

3. **全自动扫描探测与冷热变动率排序 (Smart Hot/Cold Sorting)**：
   - 支持一键扫描指定父目录（`./ark 扫描 /root/workspace`），自动发现所有项目并评估变动频率（评分 10~90 分）。
   - **冷数据在前、热数据在后**：变动少的配置/静态数据排在基础镜像层，高频变动的数据排在顶部，最大化镜像层与本地 Tree Hash 缓存复用率。

4. **秒级 Tree Hash 状态感知（封条未动 0 耗时）**：
   - 基于多 Goroutine 并发树形哈希算法，极速计算货舱指纹。
   - 无变动目录显示 `[封条完好 ✓]`，完全跳过打包与加密过程，秒级完成就绪。

5. **班轮集装箱独立分层快照（BuildKit `COPY --link`）**：
   - 采用 Docker BuildKit `# syntax=docker/dockerfile:1.4` 与 **`COPY --link`**。
   - 每个舱位封装为完全独立的集装箱 Snapshot。未变动的舱位推送到 GHCR 时远端返回 `Layer already exists`，**0 流量出海，远端 0 额外存储开销**。

6. **场景分类与日期版本控制 (Category + Date Tags)**：
   - 航次标签规范为：`{分类}-{年月日-时分秒}`（例如 `vps-20260912-140000`），并同步更新 `{分类}-latest`。
   - 同一个镜像仓库（`ghcr.io/duorameng/ark`）可并行容纳多个独立业务场景（如 `vps`、`nas`、`db`），互不覆盖干扰。

7. **分类作用域历史轮转 (Category-Scoped Pruning)**：
   - 自动轮转历史航次时，仅筛选匹配当前 `{分类}-*` 的航次进行保留数控制，绝不误触其他分类。

8. **高强度密闭封条 (适配免费公开港口)**：
   - 默认启用与 OpenSSL 完全兼容的 AES-256-CBC + PBKDF2 (100,000 次哈希) 安全封条（`.dat` 密闭二进制块）。
   - 仓库完全公开，外界看到的也只是加密二进制块；享受 GitHub 公开包**永久免费、无限存储、无限流量**。

9. **无需 Docker 的灾难独立解封 (Zero-Docker Restore)**：
   - 支持 `./ark unpack`，在宿主机无 Docker 或网络故障的极端场景下，也能单二进制一键解密还原所有 `.dat` 货物。

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
    ├── archive/              # 纯 Go Tar 打包/解包与 AES-256 密闭加解密 (含 UID/GID 保持)
    ├── config/               # 清单配置与冷热优先级排序
    ├── docker/               # BuildKit COPY --link 构型生成与容器调取
    ├── github/               # GHCR 远端港口航次查询与分类轮转清理
    ├── hash/                 # 多并发树形哈希算法
    └── scanner/              # 自动工程探测与变动率评分引擎
```

---

## 命令行操作指南

### 1. 全自动扫描与清单生成
```bash
# 全自动探测总目录下所有子工程，按冷热度排序并写入 config.json
./ark 扫描 /root/workspace
```

### 2. 登船 (打包、封条与推送)
```bash
# 模拟试航 (DRY RUN): 验证哈希对比、打包加密与 Dockerfile 生成，不实际上传
./ark 试航

# 使用默认分类 (vps) 登船 -> 生成 vps-YYYYMMDD-HHMMSS 与 vps-latest
./ark 登船

# 临时指定分类登船 (例如 db) -> 生成 db-YYYYMMDD-HHMMSS 与 db-latest
./ark 登船 db
```

### 3. 查验 (按分类检索港口航次)
```bash
# 查验港口所有航次
./ark 查验

# 仅检索指定分类 (例如 vps) 的历史航次
./ark 查验 vps
```

### 4. 下船 (靠岸卸载与还原)
```bash
# 卸载指定分类的最新班次 (从 vps-latest 还原):
./ark 下船 vps

# 卸载指定日期的历史班次到指定目录:
./ark 下船 vps-20260912-140000 /root/workspace/restored_vps
```

### 5. 独立解封 (无 Docker 极速还原)
```bash
# 直接解封单个集装箱 (例如恢复 postgres 数据，UID/GID 严格还原为 70:0)
./ark 解封 cache/postgres.dat /root/workspace/pg_restored

# 批量解封 cache 目录下所有货物
./ark 解封 cache /root/workspace/all_restored
```

---

## 自动定时班轮 (Crontab)

使用 `crontab -e` 配置每日凌晨 3:00 自动登船出海：
```bash
0 3 * * * cd /root/workspace/ark && ./ark 登船 >> /var/log/ark_voyage.log 2>&1
```

