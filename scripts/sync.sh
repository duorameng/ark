#!/usr/bin/env bash
# ==============================================================================
#                      ⚓ Ark 班轮自动化定时同步交付脚本 (Linux / VPS)
# ==============================================================================
# 适用场景:
#   - Linux VPS / 云服务器 Crontab 每日定时自动化航次交付与同步
#   - Systemd Timer 守护定时任务
#
# 核心特性:
#   1. 进程防并发锁保护 (防止网络波动导致定时任务重叠执行)
#   2. 自动环境变量与工作区定位 (无需手动切换 cd 路径)
#   3. 带时间戳的结构化运行日志输出与自动容量截断 (防日志占满磁盘)
#   4. 严格航次保留个数配额 (--keep / ARK_RETENTION_COUNT 自动淘汰旧版本)
#   5. 交付成功后自动顺带清除远端未打标孤立版本 (untagged)，杜绝垃圾残留
#   6. 默认采用 "按天归档 + 极致干净清理" 策略 (适合小磁盘 VPS 极致稳定运行)
#
# Crontab 配置示例 (每天凌晨 03:00 自动执行):
#   crontab -e
#   0 3 * * * /root/ark/scripts/sync.sh >> /dev/null 2>&1
# ==============================================================================

set -uo pipefail

# 1. 自动定位 Ark 安装目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -f "${SCRIPT_DIR}/../ark" ]]; then
    ARK_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
elif [[ -f "${SCRIPT_DIR}/ark" ]]; then
    ARK_DIR="${SCRIPT_DIR}"
else
    # 尝试在当前目录寻找
    ARK_DIR="$(pwd)"
fi

ARK_BIN="${ARK_DIR}/ark"
LOG_DIR="${ARK_DIR}/logs"
LOG_FILE="${LOG_DIR}/sync.log"
LOCK_FILE="/tmp/ark_sync.lock"
MAX_LOG_LINES=5000 # 日志保留的最大行数

mkdir -p "${LOG_DIR}"

log() {
    local timestamp
    timestamp=$(date "+%Y-%m-%d %H:%M:%S")
    echo "[${timestamp}] $1" | tee -a "${LOG_FILE}"
}

# 2. 进程文件锁保护 (避免定时任务重复并发)
exec 200>"${LOCK_FILE}"
if ! flock -n 200; then
    log "[-] 检测到已有 Ark 同步进程正在运行，跳过本次调度。"
    exit 0
fi

# 确保退出时释放锁
trap 'rm -f "${LOCK_FILE}"' EXIT

# 3. 检查可执行文件
if [[ ! -x "${ARK_BIN}" ]]; then
    log "[-] 错误: 未找到可执行文件 ${ARK_BIN}，请先编译或赋予执行权限 (chmod +x ark)"
    exit 1
fi

log "================================================================="
log "⚓ 正在启动 Ark 每日自动化巡航同步交付任务..."
log "================================================================="

# 4. 执行航次交付任务 (默认策略: 按天精度归档 + 彻底清空临时缓存)
# 用户可通过传入自定义参数覆盖，例如: ./sync.sh web day --retry 5
CLI_ARGS=("$@")
if [[ ${#CLI_ARGS[@]} -eq 0 ]]; then
    # 无参数时采用生产默认策略: 按天打标 + 全量重置本地缓存
    CLI_ARGS=("day" "--clean-all")
fi

cd "${ARK_DIR}"

log "-> 执行指令: ${ARK_BIN} board ${CLI_ARGS[*]}"
if "${ARK_BIN}" board "${CLI_ARGS[@]}" >> "${LOG_FILE}" 2>&1; then
    log "✓ 航次交付任务执行成功！"
else
    EXIT_CODE=$?
    log "[-] 航次交付任务异常终止 (退出码: ${EXIT_CODE})，详情请检查日志。"
fi

# 5. 日志自动截断保护 (保留最新 MAX_LOG_LINES 行)
if [[ -f "${LOG_FILE}" ]]; then
    LINE_COUNT=$(wc -l < "${LOG_FILE}")
    if [[ ${LINE_COUNT} -gt ${MAX_LOG_LINES} ]]; then
        tail -n "${MAX_LOG_LINES}" "${LOG_FILE}" > "${LOG_FILE}.tmp" && mv "${LOG_FILE}.tmp" "${LOG_FILE}"
        log "💡 日志已自动截断至最新 ${MAX_LOG_LINES} 行。"
    fi
fi

log "⚓ 本次巡航任务结束。"
log "================================================================="
