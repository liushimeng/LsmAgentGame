#!/usr/bin/env bash
# scripts/check_auto_scripts_consistency.sh
# ---------------------------------------------------------------
# 用途：
#   一致性自检 — 验证 Auto* 脚本与提示词文件命名/功能的契约。
#   执行: bash ./scripts/check_auto_scripts_consistency.sh
#
# 校验项：
#   1. 每个 Auto*.sh 必须有配对的 .md 提示词文件
#   2. 所有 shell 脚本必须 source agent_cli_common.sh(共四种 Agent 选择)
#   3. 公共库 agent_cli_common.sh 必须存在
#   4. (可选) each Auto*.sh 必须含「禁止修改 CLAUDE.md/AGENTS.md」守门
#      (仅对 AutoDebug*.sh 强制 — 游戏级测试脚本不强制)
#
# 返回值：
#   0 = 全部通过;非 0 = 有违反,逐项列出。
# ---------------------------------------------------------------

set -u

PROJECT_DIR="/usr/local/LsmAgentGame/LsmAgentGame"
FAIL=0

echo "============================================================"
echo "[check_auto_scripts_consistency] 启动时间 : $(date '+%F %T')"
echo "[check_auto_scripts_consistency] 项目根   : ${PROJECT_DIR}"
echo "============================================================"

cd "${PROJECT_DIR}" || { echo "[ERROR] 无法进入 ${PROJECT_DIR}"; exit 2; }

# ---------- §1 公共库存在性 ----------
if [[ ! -f "agent_cli_common.sh" ]]; then
    echo "[FAIL] agent_cli_common.sh 公共库不存在"
    FAIL=$((FAIL + 1))
else
    echo "[OK] agent_cli_common.sh 公共库存在"
fi

# ---------- §2 配对校验 ----------
# 只扫描 *.sh(非 _test,非 check_*),每个 .sh 必须有 .md 兄弟文件
sh_files=$(find . -maxdepth 1 -type f \( -name 'Auto*.sh' -o -name 'Auto*_*.sh' \) 2>/dev/null | sort)

if [[ -z "${sh_files}" ]]; then
    echo "[FAIL] 未发现任何 Auto*.sh 脚本"
    FAIL=$((FAIL + 1))
fi

while IFS= read -r sh; do
    [[ -z "${sh}" ]] && continue
    md="${sh%.sh}.md"
    if [[ ! -f "${md}" ]]; then
        echo "[FAIL] ${sh} 缺少配对提示词文件 ${md}"
        FAIL=$((FAIL + 1))
    else
        echo "[OK] ${sh} ↔ ${md}"
    fi
done <<< "${sh_files}"

# ---------- §3 公共库 source 校验 ----------
while IFS= read -r sh; do
    [[ -z "${sh}" ]] && continue
    # skip main.sh / rebuild_* / 其他服务性脚本(此自检只对 Auto* 系列有意义)
    [[ "${sh}" != ./Auto*.sh && "${sh}" != ./Auto*_*.sh ]] && continue
    if ! grep -q 'source.*agent_cli_common.sh' "${sh}" 2>/dev/null; then
        echo "[FAIL] ${sh} 未 source agent_cli_common.sh"
        FAIL=$((FAIL + 1))
    else
        echo "[OK] ${sh} 已 source agent_cli_common.sh"
    fi
done <<< "${sh_files}"

# ---------- §4 AutoDebug*.sh 守门条款校验(仅对自动修复类)----------
# 守门规则：.sh 内的 hard-grep 校验它出现在 .md 中;只校验 .sh 里是否调用了 hard-grep 即可,
# .md 里是否含此条款单独校验。
DEBUG_SH=$(find . -maxdepth 1 -type f -name 'AutoDebug*.sh' | head -1)
if [[ -n "${DEBUG_SH}" ]]; then
    # AutoDebug*.sh 必须调用 hard-grep 检查守门字符串(不允许被改回老版本)
    if ! grep -qF '绝对禁止修改 \`CLAUDE.md\`、\`AGENTS.md\`' "${DEBUG_SH}" 2>/dev/null; then
        echo "[FAIL] ${DEBUG_SH} 缺少 hard-grep 守门调用(防止提示词被改回老版本)"
        FAIL=$((FAIL + 1))
    else
        echo "[OK] ${DEBUG_SH} 已含 hard-grep 守门"
    fi
    # 同时校验配套 .md 也有此条款
    DEBUG_MD="${DEBUG_SH%.sh}.md"
    if [[ -f "${DEBUG_MD}" ]]; then
        if ! grep -qF "绝对禁止修改 \`CLAUDE.md\`、\`AGENTS.md\`" "${DEBUG_MD}" 2>/dev/null; then
            echo "[FAIL] ${DEBUG_MD} 缺少「绝对禁止修改 CLAUDE.md/AGENTS.md」守门"
            FAIL=$((FAIL + 1))
        else
            echo "[OK] ${DEBUG_MD} 已含守门条款"
        fi
    fi
fi

echo "============================================================"
if [[ "${FAIL}" -eq 0 ]]; then
    echo "[check_auto_scripts_consistency] 全部通过 ✓"
    exit 0
else
    echo "[check_auto_scripts_consistency] 失败 ${FAIL} 项,见上"
    exit 1
fi
