#!/usr/bin/env bash
# ==============================================================================
#                      ⚓ Ark 班轮自动化定时备份脚本 (Linux / VPS)
# ==============================================================================
# 适用场景:
#   - Linux VPS / 云服务器 Crontab 每日定时自动化备份
#   - Systemd Timer 守护定时任务
#
# 核心特性:
#   1. 进程防并发锁保护 (防止网络波动导致定时任务重叠执行)
#   2. 自动环境变量与工作区定位 (无需手动切换 cd 路径)
#   3. 带时间戳的结构化运行日志输出与自动容量截断 (防日志占满磁盘)
#   4. 严格备份保留个数配额 (--keep / ARK_RETENTION_COUNT 自动淘汰旧版本)
#   5. 备份成功后自动顺带清除远端未打标孤立版本 (untagged)，杜绝垃圾残留
#   6. 默认采用 "按天归档 + 极致干净清理" 策略 (适合小磁盘 VPS 极致稳定运行)
#
# Crontab 配置示例 (每天凌晨 03:00 自动执行):
#   crontab -e
#   0 3 * * * /root/ark/scripts/backup.sh >> /dev/null 2>&1
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
LOG_FILE="${LOG_DIR}/backup.log"
LOCK_FILE="/tmp/ark_backup.lock"
MAX_LOG_LINES=5000 # 日志保留的最大行数

# 检查二进制程序是否存在
if [[ ! -x "${ARK_BIN}" ]]; then
    if [[ -f "${ARK_BIN}" ]]; then
        chmod +x "${ARK_BIN}"
    else
        echo "[ERROR] [$(date '+%Y-%m-%d %H:%M:%S')] 未在 ${ARK_DIR} 找到可执行文件 ark，请先下载或编译！" >&2
        exit 1
    fi
fi

# 创建日志目录
mkdir -p "${LOG_DIR}"

# 2. 并发文件锁保护 (防止上一次备份未结束导致重复运行)
exec 200>"${LOCK_FILE}"
if ! flock -n 200; then
    echo "[WARN] [$(date '+%Y-%m-%d %H:%M:%S')] 检测到另一个 Ark 备份进程正在运行中，本次定时任务自动跳过！" | tee -a "${LOG_FILE}" >&2
    exit 0
fi

# 日志轮转截断函数
trim_log() {
    if [[ -f "${LOG_FILE}" ]]; then
        local line_count
        line_count=$(wc -l < "${LOG_FILE}" 2>/dev/null || echo 0)
        if (( line_count > MAX_LOG_LINES )); then
            local tmp_log="${LOG_FILE}.tmp"
            tail -n "${MAX_LOG_LINES}" "${LOG_FILE}" > "${tmp_log}" && mv "${tmp_log}" "${LOG_FILE}"
        fi
    fi
}

# 3. 开始执行定时航运备份
START_TIME=$(date +%s)
echo "================================================================================" >> "${LOG_FILE}"
echo "[START] [$(date '+%Y-%m-%d %H:%M:%S')] 班轮自动定时航运开始..." >> "${LOG_FILE}"
echo "[INFO]  工作区根目录: ${ARK_DIR}" >> "${LOG_FILE}"

# 切换至 Ark 工作目录执行 (确保自动载入同目录 .env 与 config.json)
cd "${ARK_DIR}"

# 尝试从 .env 预载入环境变量 (如 ARK_RETENTION_COUNT)
if [[ -f "${ARK_DIR}/.env" ]]; then
    # 导出非注释的环境变量供脚本环境读取
    set -a
    # shellcheck disable=SC1091
    source "${ARK_DIR}/.env" 2>/dev/null || true
    set +a
fi

# 备份保留个数配额 (优先从 .env 读取，默认保留 5 个历史版本)
RETENTION_COUNT="${ARK_RETENTION_COUNT:-5}"

# 默认航运策略参数:
# - day: 生成按天归档标签 (例如: vps-20260912，每天固定版本)
# - --keep <N>: 严格保留最近 N 个航次备份，超出自动归档淘汰
# - --clean-all: 推送完成后彻底重置本地 cache 与临时文件，恢复 0 字节初始状态
# 用户亦可在运行脚本时传入自定义参数覆盖，如: ./backup.sh db day --keep 7
BACKUP_ARGS=("day" "--keep" "${RETENTION_COUNT}" "--clean-all")
if [[ $# -gt 0 ]]; then
    BACKUP_ARGS=("$@")
fi

echo "[CONFIG] 备份保留配额: 最近 ${RETENTION_COUNT} 个版本" >> "${LOG_FILE}"
echo "[EXEC]   执行命令: ${ARK_BIN} ${BACKUP_ARGS[*]}" >> "${LOG_FILE}"

# 执行备份并将标准输出与错误双向记录至日志
if "${ARK_BIN}" "${BACKUP_ARGS[@]}" >> "${LOG_FILE}" 2>&1; then
    STATUS="SUCCESS"
    EXIT_CODE=0
else
    STATUS="FAILED"
    EXIT_CODE=$?
fi

END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

if [[ "${STATUS}" == "SUCCESS" ]]; then
    echo "[FINISH] [$(date '+%Y-%m-%d %H:%M:%S')] ✓ 定时备份成功完成！(耗时: ${DURATION} 秒)" >> "${LOG_FILE}"

    # 核心：顺带彻底清除远端未打标孤立版本 (untagged)，杜绝远端无标签悬空镜像堆积
    echo "[CLEAN]  [$(date '+%Y-%m-%d %H:%M:%S')] 正在顺带扫描并清理远端孤立未打标版本 (Untagged Versions)..." >> "${LOG_FILE}"
    if "${ARK_BIN}" clean --untagged >> "${LOG_FILE}" 2>&1; then
        echo "[CLEAN]  [$(date '+%Y-%m-%d %H:%M:%S')] ✓ 远端未打标版本 (untagged) 清理巡检完毕！" >> "${LOG_FILE}"
    else
        echo "[WARN]   [$(date '+%Y-%m-%d %H:%M:%S')] 远端 untagged 清理执行提示如上，不影响主备份结果。" >> "${LOG_FILE}"
    fi
else
    echo "[ERROR]  [$(date '+%Y-%m-%d %H:%M:%S')] ✗ 定时备份执行失败 (退出码: ${EXIT_CODE}, 耗时: ${DURATION} 秒)，请检查上方日志详情！" >> "${LOG_FILE}"
fi

echo "================================================================================" >> "${LOG_FILE}"

# 控制日志大小
trim_log

exit "${EXIT_CODE}"
