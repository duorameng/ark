#!/usr/bin/env bash
# ==============================================================================
# Ark 货舱全自动扫描探测与智能排序工具 (Scanner & Auto-Discovery)
# ==============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

CONFIG_FILE="${1:-$WORKSPACE_ROOT/config.json}"
SCAN_ROOT="${2:-/root/workspace}"
APPLY_CHANGES="${APPLY_CHANGES:-1}"

if [[ ! -d "$SCAN_ROOT" ]]; then
    # 如果指定路径不存在，尝试使用父目录
    SCAN_ROOT="$(dirname "$WORKSPACE_ROOT")"
fi

echo "================================================================"
echo "        🔍 Ark 货舱全自动扫描探测与变动率排序工具               "
echo "================================================================"
echo "[扫描目标] 总目录: $SCAN_ROOT"

if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "[-] 配置文件不存在，将创建初始配置: $CONFIG_FILE"
    REPO="ghcr.io/duorameng/ark"
    CATEGORY="vps"
    RETENTION=5
    ENCRYPT=true
else
    REPO=$(jq -r '.repository // "ghcr.io/duorameng/ark"' "$CONFIG_FILE")
    CATEGORY=$(jq -r '.category // "vps"' "$CONFIG_FILE")
    RETENTION=$(jq -r '.retention_count // 5' "$CONFIG_FILE")
    ENCRYPT=$(jq -r '.encrypt // true' "$CONFIG_FILE")
fi

echo "[现存配置] 仓库: $REPO | 分类: $CATEGORY | 保留: $RETENTION"
echo ""
echo "正在分析各子目录的活跃度与数据特征 (计算冷热度评分)..."

# 临时存放扫描结果
DISCOVERED="[]"

# 遍历扫描根目录下的所有直接子目录
for SUBDIR in "$SCAN_ROOT"/*; do
    [[ -d "$SUBDIR" ]] || continue
    DIR_NAME=$(basename "$SUBDIR")

    # 排除自身以及隐藏目录
    if [[ "$DIR_NAME" == "ark" || "$DIR_NAME" == .* || "$DIR_NAME" == "tmp" || "$DIR_NAME" == "cache" ]]; then
        continue
    fi

    # 规范化 ID (连字符转下划线，纯小写字母数字)
    ID=$(echo "$DIR_NAME" | tr '-' '_' | tr ' ' '_' | tr -cd 'a-zA-Z0-9_')

    # 计算文件数量与体积
    FILE_COUNT=$(find "$SUBDIR" -type f 2>/dev/null | wc -l)
    TOTAL_SIZE_KB=$(du -s "$SUBDIR" 2>/dev/null | cut -f1 || echo 0)

    # 智能评估变动率 (Volatility Score, 1-100)
    # 规则：
    # 基础分 30 (普通目录)
    # 含有数据库文件 (*.db, *.sqlite, *.sql, data/ 目录) -> +40 分 (高频热数据)
    # 24小时内有文件写入 -> +20 分
    # 仅含有纯文本配置/脚本 (如 compose.yml, conf) -> -20 分 (静态冷数据)
    SCORE=30

    # 1. 检查数据库特征
    if find "$SUBDIR" -maxdepth 3 \( -name "*.db" -o -name "*.sqlite*" -o -name "*.sql" -o -type d -name "data" -o -type d -name "db" \) 2>/dev/null | grep -q .; then
        SCORE=$((SCORE + 40))
    fi

    # 2. 检查近期改动 (24小时内)
    if find "$SUBDIR" -type f -mtime -1 2>/dev/null | grep -q .; then
        SCORE=$((SCORE + 20))
    fi

    # 3. 检查是否为纯静态微型目录 (少于 5 个文件且体积小于 100KB)
    if [[ "$FILE_COUNT" -le 5 && "$TOTAL_SIZE_KB" -le 100 ]]; then
        SCORE=$((SCORE - 20))
    fi

    # 限制分数在 10 ~ 90 之间
    if [[ "$SCORE" -lt 10 ]]; then SCORE=10; fi
    if [[ "$SCORE" -gt 90 ]]; then SCORE=90; fi

    VOLATILITY="中频"
    if [[ "$SCORE" -le 20 ]]; then
        VOLATILITY="极少变动 (冷数据)"
    elif [[ "$SCORE" -ge 60 ]]; then
        VOLATILITY="频繁变动 (热数据)"
    fi

    DISCOVERED=$(echo "$DISCOVERED" | jq --arg id "$ID" \
        --arg name "$DIR_NAME" \
        --arg path "$SUBDIR" \
        --argjson priority "$SCORE" \
        --arg vol "$VOLATILITY" \
        --arg count "$FILE_COUNT" \
        --arg size "${TOTAL_SIZE_KB}K" \
        '. + [{
            id: $id,
            name: $name,
            path: $path,
            priority: $priority,
            _desc: $vol,
            _files: ($count | tonumber),
            _size: $size
        }]')
done

# 关键核心：按照 priority (冷热度) 从小到大排序！
# 越稳定的冷数据 priority 越小，排在最前面 (Layer 1, Layer 2)；
# 变动越频繁的热数据 priority 越大，排在最后面 (Layer 3, Layer 4)！
SORTED_DISCOVERED=$(echo "$DISCOVERED" | jq 'sort_by(.priority)')

echo ""
printf "%-15s %-12s %-20s %-8s %s\n" "舱位 ID" "优先级" "变动特征" "文件数" "路径"
echo "-------------------------------------------------------------------------------"

echo "$SORTED_DISCOVERED" | jq -c '.[]' | while read -r item; do
    ID=$(echo "$item" | jq -r '.id')
    PRIORITY=$(echo "$item" | jq -r '.priority')
    VOL=$(echo "$item" | jq -r '._desc')
    FILES=$(echo "$item" | jq -r '._files')
    PATH_STR=$(echo "$item" | jq -r '.path')
    printf "%-15s %-12s %-20s %-8s %s\n" "[$ID]" "优先级: $PRIORITY" "$VOL" "$FILES" "$PATH_STR"
done
echo "-------------------------------------------------------------------------------"
echo "【排序策略说明】：数值小的静态舱位放前面，数值大的高频变动舱位放后面，以最大化 Docker 缓存命中！"

# 清除展示用的临时内部字段，生成纯净的 sources 数组
CLEAN_SOURCES=$(echo "$SORTED_DISCOVERED" | jq '[.[] | {id: .id, name: .name, path: .path, priority: .priority}]')

# 组装完整的全新配置
FINAL_CONFIG=$(jq -n \
    --arg repo "$REPO" \
    --arg cat "$CATEGORY" \
    --argjson ret "$RETENTION" \
    --argjson enc "$ENCRYPT" \
    --argjson src "$CLEAN_SOURCES" \
    '{
        repository: $repo,
        category: $cat,
        retention_count: $ret,
        encrypt: $enc,
        sources: $src
    }')

if [[ "$APPLY_CHANGES" == "1" ]]; then
    echo "$FINAL_CONFIG" > "$CONFIG_FILE"
    echo ""
    echo "✓ 已成功自动更新清单配置文件: $CONFIG_FILE"
else
    echo ""
    echo "[预览模式] 待更新的 config.json 内容如下:"
    echo "$FINAL_CONFIG" | jq .
fi
