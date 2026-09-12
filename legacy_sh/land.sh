#!/usr/bin/env bash
# ==============================================================================
# Ark 班轮货物靠岸卸载与下船系统 (支持分类指定与指定航次还原)
# ==============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

CONFIG_FILE="${1:-$WORKSPACE_ROOT/config.json}"
PARAM_TAG="${2:-}"
PARAM_DEST="${3:-}"

if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "[-] 错误: 清单配置不存在: $CONFIG_FILE" >&2
    exit 1
fi

REPO=$(jq -r '.repository' "$CONFIG_FILE")
ENCRYPT_ENABLED=$(jq -r '.encrypt // true' "$CONFIG_FILE")
DEFAULT_CATEGORY=$(jq -r '.category // "core"' "$CONFIG_FILE")

# 解析分类与 Tag
if [[ -z "$PARAM_TAG" ]]; then
    CATEGORY="$DEFAULT_CATEGORY"
    TAG="${CATEGORY}-latest"
elif [[ "$PARAM_TAG" == *-* && "$PARAM_TAG" != *-latest ]]; then
    # 传入了具体的分类航次标签，如 vps-20260912-140000
    TAG="$PARAM_TAG"
    CATEGORY="${TAG%%-*}"
elif [[ "$PARAM_TAG" == *-latest ]]; then
    TAG="$PARAM_TAG"
    CATEGORY="${TAG%%-*}"
else
    # 仅传入了分类名，如 vps
    CATEGORY="$PARAM_TAG"
    TAG="${CATEGORY}-latest"
fi

OUTPUT_DIR="${PARAM_DEST:-$WORKSPACE_ROOT/cargo_landed_${CATEGORY}}"
TMP_LAND="$WORKSPACE_ROOT/tmp/land_$(date +%Y%m%d_%H%M%S)"
KEYS_DIR="$WORKSPACE_ROOT/keys"
SEAL_KEY="$KEYS_DIR/seal.key"

echo "================================================================"
echo "          ⚓ Ark 班轮靠岸下船系统 (Landing System)              "
echo "================================================================"

echo "[港位] 来源港位: $REPO"
echo "[场景] 所属分类: $CATEGORY"
echo "[航次] 检索标签: $TAG"
echo "[卸货] 交付目的地: $OUTPUT_DIR"
echo "[安全] 封条状态: $ENCRYPT_ENABLED"

if [[ "$ENCRYPT_ENABLED" == "true" && ! -f "$SEAL_KEY" ]]; then
    echo "[-] 错误: 未找到货运封条密钥 $SEAL_KEY，无法开启密闭集装箱！" >&2
    exit 1
fi

mkdir -p "$OUTPUT_DIR" "$TMP_LAND"

# 1. 班轮进港
echo ""
echo "==> 正在靠岸进港，调取班轮快照 $REPO:$TAG..."
if ! docker pull "$REPO:$TAG"; then
    echo "[!] 提示: 远端调取未成功，将尝试使用本地停泊的快照 $REPO:$TAG..."
fi

# 2. 吊装卸载集装箱 (零运行时开销)
echo "==> 正在吊装卸载集装箱..."
CID=$(docker create "$REPO:$TAG")
docker cp "$CID:/cargo/." "$TMP_LAND/"
docker rm "$CID" >/dev/null

# 3. 开封并归位各舱货物
echo ""
echo "------------------- 正在开封集装箱并归位货物 -------------------"

for FILE in "$TMP_LAND"/*; do
    [[ -f "$FILE" ]] || continue
    FILENAME=$(basename "$FILE")
    MOD_NAME="${FILENAME%.*}"

    DEST_DIR="$OUTPUT_DIR/$MOD_NAME"
    mkdir -p "$DEST_DIR"

    echo "-> 正在卸载舱位: $MOD_NAME..."

    TAR_FILE="$FILE"
    if [[ "$FILENAME" == *.dat || "$FILENAME" == *.tar.enc ]]; then
        MOD_NAME="${FILENAME%.*}"
        MOD_NAME="${MOD_NAME%.tar}"
        DEST_DIR="$OUTPUT_DIR/$MOD_NAME"
        mkdir -p "$DEST_DIR"

        DECRYPTED_TAR="$TMP_LAND/$MOD_NAME.tar"
        echo "   正在开启安全封条 ($FILENAME)..."
        openssl enc -d -aes-256-cbc -pbkdf2 -iter 100000 -in "$FILE" -out "$DECRYPTED_TAR" -pass "file:$SEAL_KEY"
        TAR_FILE="$DECRYPTED_TAR"
    fi

    if [[ "$TAR_FILE" == *.tar ]]; then
        echo "   正在还原货物到: $DEST_DIR (保留所有者与访问权限)..."
        tar --same-owner --numeric-owner -pxf "$TAR_FILE" -C "$DEST_DIR"
        COUNT=$(find "$DEST_DIR" -type f | wc -l)
        echo "   ✓ 货物已完整归位！包含 $COUNT 项物品"
    fi
done

# 清理作业临时区
rm -rf "$TMP_LAND"

echo ""
echo "================================================================"
echo "          🎉 下船清关完毕，所有货物已交付至: $OUTPUT_DIR        "
echo "================================================================"
