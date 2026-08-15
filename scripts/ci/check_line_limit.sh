#!/usr/bin/env bash
# =============================================================================
#  check_line_limit.sh — CLAUDE.md §4「单文件 ≤ 1800 行」硬上限自检
#
#  2026-08-14 §20260814-01 U4 新增。
#
#  待实施项文档 §1 早就写了「CI 加自定义 lint 可自动化拦截此类缺陷 ——
#  此项工程化改进本身待做」。本脚本 + .github/workflows/ci.yml 把它落地。
#
#  扫描范围与 CLAUDE.md §4 一致:
#    ServerGo/**/*.go
#    ClientWeb/src/**/*.{ts,tsx,css}
#
#  # 为什么需要 baseline 白名单
#
#  引入本检查时仓库已有 4 个文件超限(见 BASELINE)。若不设白名单,CI 从第一天
#  就是红的 —— 而「长期红的 CI」等于没有 CI,团队会学会忽略它。
#
#  白名单纪律(与 ServerGo/agent/wwplayer/wiring_lint_test.go:83 的既有约定一致):
#    ⚠️ 只允许变短,不允许变长。
#  白名单文件仍会被检查「有没有变得更长」—— 超过登记的行数即 fail,
#  这样既不阻塞现状,又保证债务不会继续恶化(棘轮效应)。
#
#  用法: bash scripts/ci/check_line_limit.sh
#  退出码: 0 = 通过;1 = 有文件违规
# =============================================================================

set -uo pipefail

# scripts/ci/ 上两级为项目根(2026-08-15 自 scripts/ 迁入,层级加深一级)
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

LIMIT=1800

# ---------------------------------------------------------------------------
# BASELINE — 引入检查时已超限的文件: "<相对路径>:<当时行数>"
#
# 这些是历史债务,留待独立重构提交(纯代码搬移,不与功能改动混在一起)。
# 每一行的数字是**允许的上限**:文件可以变短(甚至降到 1800 以下后从本表删除),
# 但一旦变长就会 fail。
#
# §20260814-01 U4 已经拆掉了原本最大的一个:
#   ServerGo/agent/wwplayer/run.go  2154 → 1684(搬到 run_config.go)
# ---------------------------------------------------------------------------
BASELINE=(
  "ServerGo/agent/wwplayer/agent.go:2111"
  "ServerGo/game/werewolf/room_agent.go:2105"
  "ServerGo/game/werewolf/agent_runner.go:1982"
  "ServerGo/game/werewolf/room.go:1896"
)

baseline_limit_for() {
  local path="$1"
  local entry
  for entry in "${BASELINE[@]}"; do
    if [[ "${entry%:*}" == "$path" ]]; then
      echo "${entry##*:}"
      return 0
    fi
  done
  return 1
}

violations=0
regressions=0

while IFS= read -r file; do
  lines=$(wc -l < "$file" | tr -d ' ')
  [[ "$lines" -le "$LIMIT" ]] && continue

  if allowed=$(baseline_limit_for "$file"); then
    if [[ "$lines" -gt "$allowed" ]]; then
      echo "❌ [baseline 恶化] $file: $lines 行 > 登记上限 $allowed 行"
      echo "   白名单只允许变短。请把新增代码放到同 package 的新文件里(§4)。"
      regressions=$((regressions + 1))
    else
      echo "⚠️  [已登记债务] $file: $lines 行(上限 $LIMIT,登记 $allowed)"
    fi
    continue
  fi

  echo "❌ [超出 §4 上限] $file: $lines 行 > $LIMIT 行"
  echo "   请按 CLAUDE.md §4 拆分:Go → 同 package 多个 snake_case.go(纯搬移);"
  echo "   TS/TSX → 按职责拆模块;CSS → 拆主题文件并保持 @import 顺序。"
  violations=$((violations + 1))
done < <(
  {
    find ServerGo -name '*.go' -not -path '*/node_modules/*'
    find ClientWeb/src \( -name '*.ts' -o -name '*.tsx' -o -name '*.css' \) \
      -not -path '*/node_modules/*'
  } | sort
)

echo
if [[ "$violations" -gt 0 || "$regressions" -gt 0 ]]; then
  echo "§4 行数检查失败:新增超限 $violations 个,baseline 恶化 $regressions 个。"
  exit 1
fi
echo "✅ §4 行数检查通过(baseline 债务 ${#BASELINE[@]} 个,均未恶化)。"
