#!/usr/bin/env bash
# ==============================================================================
# Ark 港口航次与舱位清点查询系统 (Listing / Query System)
# ==============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

CONFIG_FILE="${1:-$WORKSPACE_ROOT/config.json}"
CATEGORY_FILTER="${2:-}"

if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "[-] 错误: 配置文件不存在: $CONFIG_FILE" >&2
    exit 1
fi

REPO=$(jq -r '.repository' "$CONFIG_FILE")
REG_USER=$(echo "$REPO" | cut -d'/' -f2)
PKG_NAME=$(echo "$REPO" | rev | cut -d'/' -f1 | rev)

# 读取 Token
GH_TOKEN="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
if [[ -z "$GH_TOKEN" && -f "$WORKSPACE_ROOT/.env" ]]; then
    GH_TOKEN=$(grep -E '^\s*(GH_TOKEN|GITHUB_TOKEN)=' "$WORKSPACE_ROOT/.env" | head -n1 | cut -d'=' -f2- | tr -d '"' | tr -d "'")
fi

echo "================================================================"
echo "          ⚓ Ark 港口航次查询终端 (Voyage List)                 "
echo "================================================================"
echo "[港位] 目标仓库: $REPO"

if [[ -n "$CATEGORY_FILTER" ]]; then
    echo "[筛选] 指定分类: $CATEGORY_FILTER"
fi

if [[ -z "$GH_TOKEN" ]]; then
    echo "[!] 提示: 未检测到 GH_TOKEN。请在 .env 中配置，否则无法查询远端列表。" >&2
    exit 1
fi

API_URL="https://api.github.com/user/packages/container/$PKG_NAME/versions"
VERSIONS_JSON=$(curl -s -H "Authorization: Bearer $GH_TOKEN" \
    -H "Accept: application/vnd.github+json" \
    "$API_URL" || true)

TOTAL_VERSIONS=$(echo "$VERSIONS_JSON" | jq 'if type=="array" then length else 0 end' 2>/dev/null || echo 0)

if [[ "$TOTAL_VERSIONS" -eq 0 ]]; then
    echo "[-] 当前仓库暂未查询到已记录的航次版本。"
    exit 0
fi

echo ""
printf "%-10s %-25s %-22s %s\n" "分类" "航次标签 (Tag)" "创建日期 (UTC)" "版本 ID"
echo "-------------------------------------------------------------------------------"

echo "$VERSIONS_JSON" | jq -c '.[]' | while read -r ver; do
    VER_ID=$(echo "$ver" | jq -r '.id')
    CREATED_AT=$(echo "$ver" | jq -r '.created_at')
    TAGS=$(echo "$ver" | jq -r '.metadata.container.tags // [] | join(", ")')

    # 从 tags 提取分类
    for TAG in $(echo "$ver" | jq -r '.metadata.container.tags // [] | .[]'); do
        if [[ "$TAG" == *-* && "$TAG" != *-latest ]]; then
            CAT="${TAG%%-*}"
            if [[ -z "$CATEGORY_FILTER" || "$CATEGORY_FILTER" == "$CAT" ]]; then
                printf "%-10s %-25s %-22s %s\n" "[$CAT]" "$TAG" "$CREATED_AT" "$VER_ID"
            fi
        fi
    done
done
echo "-------------------------------------------------------------------------------"
