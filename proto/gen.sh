#!/bin/bash
# ───────────────────────────────────────────────────
#  Protocol Buffers 代码生成脚本
#
#  用法: ./proto/gen.sh [go|ts|all]
#
#    all(默认) : 生成 Go + TS(本地开发用)
#    go        : 仅生成 Go(CI backend Job 用,无需 node_modules)
#    ts        : 仅生成 TS(CI frontend Job 用,无需 protoc-gen-go)
#
#  生成:
#    Go  : ServerGo/proto/pb/**/*.pb.go
#    TS  : ClientWeb/src/proto/**/*.ts
#
#  工具依赖:
#    - protoc (>= 3.20)
#    - protoc-gen-go (google.golang.org/protobuf)  —— go/all 需要
#    - @protobuf-ts/plugin (npm)                    —— ts/all 需要
# ───────────────────────────────────────────────────

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROTO_DIR="$ROOT/proto"
GO_OUT="$ROOT/ServerGo/proto/pb"
TS_OUT="$ROOT/ClientWeb/src/proto"

# ── 解析目标参数(向后兼容:无参数 = all) ──
TARGET="${1:-all}"
case "$TARGET" in
  go|ts|all) ;;
  *)
    echo "❌ 未知目标: $TARGET (可选: go | ts | all)"
    exit 1
    ;;
esac
DO_GO=0; DO_TS=0
[[ "$TARGET" == "go" || "$TARGET" == "all" ]] && DO_GO=1
[[ "$TARGET" == "ts" || "$TARGET" == "all" ]] && DO_TS=1

echo "────────────────────────────────────────────"
echo " Protocol Buffers 代码生成"
echo "────────────────────────────────────────────"

# ── 检查 protoc ──
if ! command -v protoc &>/dev/null; then
  if [[ -x "/tmp/protoc/bin/protoc" ]]; then
    export PATH="/tmp/protoc/bin:$PATH"
    echo "ℹ️  使用 /tmp/protoc/bin/protoc"
  else
    echo "❌  未找到 protoc，请先安装 Protocol Buffers 编译器"
    echo "    下载: https://github.com/protocolbuffers/protobuf/releases"
    exit 1
  fi
fi
echo "📦  protoc 版本: $(protoc --version)"

# ── 检查 protoc-gen-go(仅 go/all 需要) ──
if [[ "$DO_GO" == "1" ]]; then
  if ! command -v protoc-gen-go &>/dev/null; then
    GO_BIN="$(go env GOPATH)/bin"
    if [[ -x "$GO_BIN/protoc-gen-go" ]]; then
      export PATH="$GO_BIN:$PATH"
      echo "ℹ️  使用 $GO_BIN/protoc-gen-go"
    else
      echo "❌  未找到 protoc-gen-go，请安装: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
      exit 1
    fi
  fi
  echo "📦  protoc-gen-go 就绪"
fi

# ── 检查 @protobuf-ts/plugin(仅 ts/all 需要) ──
if [[ "$DO_TS" == "1" ]]; then
  TS_PLUGIN="$ROOT/ClientWeb/node_modules/.bin/protoc-gen-ts"
  if [[ ! -x "$TS_PLUGIN" ]]; then
    # 尝试全局
    if command -v protoc-gen-ts &>/dev/null; then
      TS_PLUGIN="$(which protoc-gen-ts)"
    else
      echo "❌  未找到 @protobuf-ts/plugin，请在 ClientWeb 中: npm install --save-dev @protobuf-ts/plugin"
      exit 1
    fi
  fi
  echo "📦  @protobuf-ts/plugin 就绪"
fi

# ── 清理旧生成文件(仅清理本次目标,避免 go/ts 单目标误删另一侧产物) ──
echo ""
echo "🧹  清理旧生成文件..."
if [[ "$DO_GO" == "1" ]]; then rm -rf "$GO_OUT"; mkdir -p "$GO_OUT"; fi
if [[ "$DO_TS" == "1" ]]; then rm -rf "$TS_OUT"; mkdir -p "$TS_OUT"; fi

# ── 收集所有 proto 文件（排除 legacy 目录） ──
PROTO_FILES=()
while IFS= read -r -d '' f; do
  PROTO_FILES+=("$f")
done < <(find "$PROTO_DIR" -name "*.proto" -not -path "*/legacy/*" -print0 | sort -z)

echo "📄  找到 ${#PROTO_FILES[@]} 个 .proto 文件"
for f in "${PROTO_FILES[@]}"; do
  echo "    $(realpath --relative-to="$ROOT" "$f")"
done

# ── 生成 Go 代码 ──
GO_COUNT=0
if [[ "$DO_GO" == "1" ]]; then
  echo ""
  echo "🔧  生成 Go 代码 → $GO_OUT..."
  protoc \
    --proto_path="$PROTO_DIR" \
    --go_out="$GO_OUT" \
    --go_opt=paths=source_relative \
    "${PROTO_FILES[@]}"

  GO_COUNT=$(find "$GO_OUT" -name "*.pb.go" | wc -l)
  echo "✅  Go: 生成 $GO_COUNT 个 .pb.go 文件"
fi

# ── 生成 TypeScript 代码 ──
TS_COUNT=0
if [[ "$DO_TS" == "1" ]]; then
  echo ""
  echo "🔧  生成 TypeScript 代码 → $TS_OUT..."
  protoc \
    --proto_path="$PROTO_DIR" \
    --plugin=protoc-gen-ts="$TS_PLUGIN" \
    --ts_out="$TS_OUT" \
    --ts_opt=long_type_number \
    --ts_opt=optimize_code_size \
    --ts_opt=ts_nocheck \
    "${PROTO_FILES[@]}"

  TS_COUNT=$(find "$TS_OUT" -name "*.ts" | wc -l)
  echo "✅  TypeScript: 生成 $TS_COUNT 个 .ts 文件"
fi

# ── 统计汇总 ──
echo ""
echo "────────────────────────────────────────────"
echo " ✅  全部生成完成 (target=$TARGET)"
echo ""
[[ "$DO_GO" == "1" ]] && echo "  Go  输出: $GO_OUT  ($GO_COUNT files)"
[[ "$DO_TS" == "1" ]] && echo "  TS  输出: $TS_OUT   ($TS_COUNT files)"
echo "────────────────────────────────────────────"
