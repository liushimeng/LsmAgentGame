#!/usr/bin/env bash
# AutoDebugTestReport.sh
# ---------------------------------------------------------------
# 用途：
#   自动启动 Claude Code，读取当前目录或仓库根的 AutoDebugTestReport.md
#   作为提示词执行，全程 bypass 权限，运行结束后自动退出。
#
# 特性：
#   - 工作目录与 Claude Code 启动目录均为 /usr/local/LsmAgentGame
#   - 通过 nohup + setsid + & + disown 脱离调用者，**不阻塞**调用者进程
#   - 日志输出到 ./logs/auto_debug_<timestamp>.log
#   - AutoDebugTestReport.md 优先取当前目录，其次仓库根；都不存在则立即报错退出
#   - 脚本本身赋予 755 权限
# ---------------------------------------------------------------

set -u

# ---------- 配置 ----------
PROJECT_DIR="/usr/local/LsmAgentGame"
CLAUDE_BIN="${CLAUDE_BIN:-claude}"
LOG_DIR="${PROJECT_DIR}/logs"
PROMPT_FILE_NAME="AutoDebugTestReport.md"
TS="$(date +%Y%m%d_%H%M%S)"
LOG_FILE="${LOG_DIR}/auto_debug_${TS}.log"

mkdir -p "${LOG_DIR}"

# ---------- 定位提示词文件 ----------
PROMPT_FILE=""
for candidate in \
    "${PROJECT_DIR}/${PROMPT_FILE_NAME}" \
    "./${PROMPT_FILE_NAME}" \
    "${PWD}/${PROMPT_FILE_NAME}"; do
    if [[ -f "${candidate}" ]]; then
        PROMPT_FILE="$(readlink -f "${candidate}")"
        break
    fi
done

if [[ -z "${PROMPT_FILE}" ]]; then
    echo "[ERROR] 找不到 ${PROMPT_FILE_NAME}，已检查："
    echo "  - ${PROJECT_DIR}/${PROMPT_FILE_NAME}"
    echo "  - ./${PROMPT_FILE_NAME}"
    echo "  - ${PWD}/${PROMPT_FILE_NAME}"
    exit 1
fi

# ---------- 构造 Claude Code 调用 ----------
# 用 --dangerously-skip-permissions 跳过所有权限弹窗；
# 用 -p <file> 让 prompt 直接来自文件，避免 shell 转义引号问题；
# 参数 --permission-mode bypassPermissions 暂时不用。
PROMPT_ARG="$(cat "${PROMPT_FILE}")"

# ---------- 规则文件守门（防止 prompt 被改回老版本）----------
# AutoDebugTestReport.md 必须显式声明"禁止修改 CLAUDE.md/KILO.md/AGENT.md"，
# 否则视为脚本被误改，提示人工修复并退出。
if ! grep -q "绝对禁止修改 \`CLAUDE\.md\`、\`KILO\.md\`、\`AGENT\.md\`" "${PROMPT_FILE}" 2>/dev/null; then
    echo "[ERROR] ${PROMPT_FILE} 缺少「绝对禁止修改 CLAUDE.md/KILO.md/AGENT.md」守门条款，拒绝启动。" >&2
    echo "[ERROR] 请先修复 ${PROMPT_FILE} 再重跑本脚本。" >&2
    exit 2
fi

cd "${PROJECT_DIR}" || { echo "[ERROR] 无法进入 ${PROJECT_DIR}"; exit 1; }

{
    echo "============================================================"
    echo "[AutoDebugTestReport] 启动时间 : $(date '+%F %T')"
    echo "[AutoDebugTestReport] 工作目录 : ${PROJECT_DIR}"
    echo "[AutoDebugTestReport] 提示词文件: ${PROMPT_FILE}"
    echo "[AutoDebugTestReport] 日志文件  : ${LOG_FILE}"
    echo "[AutoDebugTestReport] claude 二进制: $(command -v ${CLAUDE_BIN} || echo 'NOT FOUND')"
    echo "============================================================"
} >> "${LOG_FILE}"

# ---------- 启动（后台脱离，不阻塞调用者）----------
# setsid  : 创建新会话，调用者退出也不会被 SIGHUP
# nohup   : 忽略 SIGHUP
# & + disown: 与当前 shell 解除关系
# stdout/stderr → 日志文件
nohup setsid bash -c "
    cd '${PROJECT_DIR}'
    ${CLAUDE_BIN} --dangerously-skip-permissions -p \"\$(cat '${PROMPT_FILE}')\"
    echo '[AutoDebugTestReport] claude 退出码 : '\$?
    echo '[AutoDebugTestReport] 结束时间 : '\"\$(date '+%F %T')\"
" >> "${LOG_FILE}" 2>&1 </dev/null &

CLAUDE_PID=$!
disown "${CLAUDE_PID}" 2>/dev/null || true

echo "[AutoDebugTestReport] 已后台启动 Claude Code，PID=${CLAUDE_PID}"
echo "[AutoDebugTestReport] 日志 : ${LOG_FILE}"
echo "[AutoDebugTestReport] 调用者可继续执行其他操作，不会被阻塞。"