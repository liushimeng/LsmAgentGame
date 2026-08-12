#!/bin/bash
# ───────────────────────────────────────────────────
#  Protocol Buffers 代码生成脚本
#
#  用法: ./proto/gen.sh
#
#  生成:
#    Go  : ServerGo/proto/pb/**/*.pb.go
#    TS  : ClientWeb/src/proto/**/*.ts
#
#  工具依赖:
#    - protoc (>= 3.20)
#    - protoc-gen-go (google.golang.org/protobuf)
#    - @protobuf-ts/plugin (npm)
# ───────────────────────────────────────────────────

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROTO_DIR="$ROOT/proto"
GO_OUT="$ROOT/ServerGo/proto/pb"
TS_OUT="$ROOT/ClientWeb/src/proto"

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

# ── 检查 protoc-gen-go ──
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

# ── 检查 @protobuf-ts/plugin ──
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

# ── 清理旧生成文件 ──
echo ""
echo "🧹  清理旧生成文件..."
rm -rf "$GO_OUT"
rm -rf "$TS_OUT"
mkdir -p "$GO_OUT"
mkdir -p "$TS_OUT"

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
echo ""
echo "🔧  生成 Go 代码 → $GO_OUT..."
protoc \
  --proto_path="$PROTO_DIR" \
  --go_out="$GO_OUT" \
  --go_opt=paths=source_relative \
  "${PROTO_FILES[@]}"

GO_COUNT=$(find "$GO_OUT" -name "*.pb.go" | wc -l)
echo "✅  Go: 生成 $GO_COUNT 个 .pb.go 文件"

# ── 生成 TypeScript 代码 ──
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

# ── 统计汇总 ──
echo ""
echo "────────────────────────────────────────────"
echo " ✅  全部生成完成"
echo ""
echo "  Go  输出: $GO_OUT  ($GO_COUNT files)"
echo "  TS  输出: $TS_OUT   ($TS_COUNT files)"
echo "────────────────────────────────────────────"
