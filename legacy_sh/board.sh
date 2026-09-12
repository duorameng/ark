#!/usr/bin/env bash
# ==============================================================================
# Ark 班轮集装箱装载与登船系统 (支持分类+日期与精准范围轮转)
# ==============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

CONFIG_FILE="${1:-$WORKSPACE_ROOT/config.json}"
CATEGORY_PARAM="${2:-}"
FORCE_FULL="${FORCE_FULL:-0}"
DRY_RUN="${DRY_RUN:-0}"

CACHE_DIR="$WORKSPACE_ROOT/cache"
TMP_DIR="$WORKSPACE_ROOT/tmp"
KEYS_DIR="$WORKSPACE_ROOT/keys"
MANIFEST_FILE="$CACHE_DIR/manifest.json"

mkdir -p "$CACHE_DIR" "$TMP_DIR" "$KEYS_DIR"

echo "================================================================"
echo "          🚢 Ark 班轮装载登船系统 (Boarding System)             "
echo "================================================================"

# 1. 读取配置文件
if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "[-] 错误: 清单配置不存在: $CONFIG_FILE" >&2
    exit 1
fi

REPO=$(jq -r '.repository' "$CONFIG_FILE")
RETENTION_COUNT=$(jq -r '.retention_count // 5' "$CONFIG_FILE")
ENCRYPT_ENABLED=$(jq -r '.encrypt // true' "$CONFIG_FILE")
CONFIG_CATEGORY=$(jq -r '.category // "core"' "$CONFIG_FILE")

# 2. 计算分类 (Category) 与 航次编号 (Tag = 分类+日期)
# 如果参数指定了分类或完整 Tag
CATEGORY="$CONFIG_CATEGORY"
if [[ -n "$CATEGORY_PARAM" ]]; then
    if [[ "$CATEGORY_PARAM" =~ ^[a-zA-Z0-9_-]+-[0-9]{8}-[0-9]{6}$ ]]; then
        TAG="$CATEGORY_PARAM"
        CATEGORY="${TAG%%-*}"
    else
        CATEGORY="$CATEGORY_PARAM"
        TAG="${CATEGORY}-$(date +%Y%m%d-%H%M%S)"
    fi
else
    TAG="${CATEGORY}-$(date +%Y%m%d-%H%M%S)"
fi

CATEGORY_LATEST="${CATEGORY}-latest"

echo "[航次] 目的港位: $REPO"
echo "[场景] 所属分类: $CATEGORY"
echo "[航次] 班次编号: $TAG 与 $CATEGORY_LATEST"
echo "[航次] 舱位配额: 该分类下保留最新 $RETENTION_COUNT 个航次"
echo "[安全] 货运封条: $ENCRYPT_ENABLED"

# 3. 读取通行凭据
GH_TOKEN="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
if [[ -z "$GH_TOKEN" && -f "$WORKSPACE_ROOT/.env" ]]; then
    GH_TOKEN=$(grep -E '^\s*(GH_TOKEN|GITHUB_TOKEN)=' "$WORKSPACE_ROOT/.env" | head -n1 | cut -d'=' -f2- | tr -d '"' | tr -d "'")
fi

REGISTRY_HOST=$(echo "$REPO" | cut -d'/' -f1)
REG_USER=$(echo "$REPO" | cut -d'/' -f2)
PKG_NAME=$(echo "$REPO" | rev | cut -d'/' -f1 | rev)

# 4. 检查安全封条密钥 (本地密封)
SEAL_KEY="$KEYS_DIR/seal.key"
if [[ "$ENCRYPT_ENABLED" == "true" ]]; then
    if [[ ! -f "$SEAL_KEY" ]]; then
        echo "[安全] 首次装载，生成货运专属封条密钥: $SEAL_KEY"
        openssl rand -base64 32 > "$SEAL_KEY"
        chmod 600 "$SEAL_KEY"
    fi
fi

# 5. 加载历史装载清单
if [[ ! -f "$MANIFEST_FILE" || "$FORCE_FULL" == "1" ]]; then
    echo "{}" > "$MANIFEST_FILE"
fi

# 6. 辅助函数：计算货物特征指纹 (Tree Hash)
get_dir_tree_hash() {
    local dir="$1"
    if [[ ! -d "$dir" ]]; then
        echo "empty"
        return
    fi
    (cd "$dir" && find . -type f -not -path '*/.*' -print0 | sort -z | xargs -0 sha256sum 2>/dev/null | sha256sum | awk '{print $1}')
}

# 7. 处理各个货舱货物 (按 priority 升序排序：静态低频在前，高频变动在后)
SORTED_SOURCES=$(jq '.sources | sort_by(.priority // 50)' "$CONFIG_FILE")
SOURCE_COUNT=$(echo "$SORTED_SOURCES" | jq 'length')
echo ""
echo "------------------- 正在清点各货舱集装箱 (按变动频率排序) -------------------"

DOCKERFILE_PATH="$TMP_DIR/Dockerfile"
cat << 'EOF' > "$DOCKERFILE_PATH"
# syntax=docker/dockerfile:1.4
FROM scratch
EOF

NEW_MANIFEST="{}"

for ((i=0; i<SOURCE_COUNT; i++)); do
    ID=$(echo "$SORTED_SOURCES" | jq -r ".[$i].id")
    NAME=$(echo "$SORTED_SOURCES" | jq -r ".[$i].name")
    SRC_PATH=$(echo "$SORTED_SOURCES" | jq -r ".[$i].path")
    PRIORITY=$(echo "$SORTED_SOURCES" | jq -r ".[$i].priority // 50")

    if [[ "$SRC_PATH" != /* ]]; then
        SRC_PATH="$WORKSPACE_ROOT/$SRC_PATH"
    fi

    if [[ ! -d "$SRC_PATH" ]]; then
        echo "[!] 警告: 货源路径不存在，跳过: $SRC_PATH ($NAME)"
        continue
    fi

    echo "-> 正在清点舱位: [$ID] $NAME ($SRC_PATH)"
    CURRENT_HASH=$(get_dir_tree_hash "$SRC_PATH")
    CACHED_HASH=$(jq -r --arg id "$ID" '.[$id].tree_hash // empty' "$MANIFEST_FILE")

    TAR_FILE="$CACHE_DIR/$ID.tar"
    LAYER_FILE="$TAR_FILE"

    if [[ "$ENCRYPT_ENABLED" == "true" ]]; then
        LAYER_FILE="$CACHE_DIR/$ID.dat"
    fi

    IS_CACHED=0
    if [[ "$FORCE_FULL" != "1" && "$CURRENT_HASH" == "$CACHED_HASH" && -f "$LAYER_FILE" ]]; then
        IS_CACHED=1
    fi

    if [[ "$IS_CACHED" == "1" ]]; then
        echo "   [封条完好 ✓] 舱位货物无变化，直接复用已有集装箱 (指纹: ${CURRENT_HASH:0:12}...)"
    else
        echo "   [重新装箱 ⚡] 舱位货物有变动或首次装载，开始打包加封..."
        # 严格保留文件原始 numeric UID/GID 与权限
        tar --numeric-owner -p -cf "$TAR_FILE" -C "$SRC_PATH" .

        if [[ "$ENCRYPT_ENABLED" == "true" ]]; then
            echo "   正在施加安全密封 (AES-256 密闭处理)..."
            openssl enc -aes-256-cbc -salt -pbkdf2 -iter 100000 -in "$TAR_FILE" -out "$LAYER_FILE" -pass "file:$SEAL_KEY"
            rm -f "$TAR_FILE"
        fi
        echo "   装箱完毕: $(basename "$LAYER_FILE") ($(du -h "$LAYER_FILE" | cut -f1))"
    fi

    # 动态组装班轮独立装运快照 (COPY --link)
    LAYER_BASENAME=$(basename "$LAYER_FILE")
    echo "COPY --link cache/$LAYER_BASENAME /cargo/$LAYER_BASENAME" >> "$DOCKERFILE_PATH"

    LAYER_SHA256=$(sha256sum "$LAYER_FILE" | awk '{print $1}')
    NEW_MANIFEST=$(echo "$NEW_MANIFEST" | jq --arg id "$ID" \
        --arg name "$NAME" \
        --arg path "$SRC_PATH" \
        --arg hash "$CURRENT_HASH" \
        --arg layer "$LAYER_BASENAME" \
        --arg sha "$LAYER_SHA256" \
        --arg time "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        '. + {($id): {name: $name, path: $path, tree_hash: $hash, layer_file: $layer, layer_sha256: $sha, updated_at: $time}}')
done

echo 'CMD ["ark-voyage"]' >> "$DOCKERFILE_PATH"
echo "$NEW_MANIFEST" > "$MANIFEST_FILE"

echo ""
echo "------------------- 装载构型 (Dockerfile) -------------------"
cat "$DOCKERFILE_PATH"
echo "-------------------------------------------------------------"

if [[ "$DRY_RUN" == "1" ]]; then
    echo "[DRY RUN] 模拟登船完毕，跳过实际航行与推送。"
    exit 0
fi

# 8. 构建班轮镜像
echo ""
echo "==> 正在使用 BuildKit 进行班轮集装箱独立分层快照构建..."
export DOCKER_BUILDKIT=1
docker build -f "$DOCKERFILE_PATH" -t "$REPO:$TAG" -t "$REPO:$CATEGORY_LATEST" "$WORKSPACE_ROOT"

echo "✓ 班轮快照封装成功！"
docker images "$REPO:$TAG"

# 9. 凭据认证并启航推送
if [[ -n "$GH_TOKEN" ]]; then
    echo "正在校验港口通行证 $REGISTRY_HOST..."
    echo "$GH_TOKEN" | docker login "$REGISTRY_HOST" -u "$REG_USER" --password-stdin
else
    echo "[!] 提示: 未检测到通行凭据。如需启航，请在 .env 中配置 GH_TOKEN。"
fi

echo ""
echo "==> 班轮正在出港登船: $REPO:$TAG 与 $REPO:$CATEGORY_LATEST..."
echo "【免复传机制】：封条未变动的集装箱将显示 'Layer already exists'，0 流量瞬间交付！"
docker push "$REPO:$TAG"
docker push "$REPO:$CATEGORY_LATEST"

echo "✓ 航次交付登船成功！"

# 10. 精准按分类与日期执行历史航次轮转 (Category-Scoped Retention Policy)
echo ""
echo "------------------- 正在维护 [$CATEGORY] 分类的历史航次配额 -------------------"
if [[ -n "$GH_TOKEN" && "$RETENTION_COUNT" -gt 0 ]]; then
    API_URL="https://api.github.com/user/packages/container/$PKG_NAME/versions"
    VERSIONS_JSON=$(curl -s -H "Authorization: Bearer $GH_TOKEN" \
        -H "Accept: application/vnd.github+json" \
        "$API_URL" || true)

    # 仅筛选出属于当前 CATEGORY 的版本 (tags 包含 category-*)
    MATCHED_VERSIONS=$(echo "$VERSIONS_JSON" | jq --arg cat "$CATEGORY" '
        if type=="array" then
            map(select(.metadata.container.tags // [] | any(startswith($cat + "-"))))
        else
            []
        end
    ' 2>/dev/null || echo "[]")

    MATCHED_COUNT=$(echo "$MATCHED_VERSIONS" | jq 'length')
    echo "当前 [$CATEGORY] 分类已记录航次: $MATCHED_COUNT，保留上限: $RETENTION_COUNT"

    if [[ "$MATCHED_COUNT" -gt "$RETENTION_COUNT" ]]; then
        # 超出上限的最早版本
        DELETE_IDS=$(echo "$MATCHED_VERSIONS" | jq -r ".[$RETENTION_COUNT:][].id")
        for VER_ID in $DELETE_IDS; do
            echo "-> 正在归档 [$CATEGORY] 历史过期航次 ID: $VER_ID..."
            curl -s -X DELETE \
                -H "Authorization: Bearer $GH_TOKEN" \
                -H "Accept: application/vnd.github+json" \
                "https://api.github.com/user/packages/container/$PKG_NAME/versions/$VER_ID" >/dev/null || true
            echo "   归档指令已送达"
        done
        echo "✓ [$CATEGORY] 历史航次维护完毕 (其他分类完全不受影响)。"
    else
        echo "✓ 泊位充足，无需归档旧航次。"
    fi
else
    echo "未提供通行凭据，跳过远端航次轮转维护。"
fi

echo ""
echo "================================================================"
echo "          ⚓ 登船航次全流程圆满完成 (Ark Voyage Ready)            "
echo "================================================================"
