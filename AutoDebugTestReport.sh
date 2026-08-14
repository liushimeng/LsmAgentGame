#!/usr/bin/env bash
# AutoDebugTestReport.sh
# ---------------------------------------------------------------
# 用途：
#   随机选择一个可用的编程 Agent CLI（Claude Code / OpenCode / Hermes /
#   OpenClaw），读取当前目录或仓库根的 AutoDebugTestReport.md 作为提示词
#   执行，全程 bypass 权限，运行结束后自动退出。
#
# 特性：
#   - 工作目录与 Agent 启动目录均为 /usr/local/LsmAgentGame/LsmAgentGame
#   - 多 Agent 随机选择逻辑在公共库 agent_cli_common.sh 中（可 source 复用）；
#     AGENT_CLI 环境变量可强制指定某个 Agent（claude|opencode|hermes|openclaw）
#   - 通过 nohup + setsid + & + disown 脱离调用者，**不阻塞**调用者进程
#   - 日志输出到 ./logs/auto_debug_<timestamp>.log
#   - AutoDebugTestReport.md 优先取当前目录，其次仓库根；都不存在则立即报错退出
#   - 启动前预检：无待处理报告时直接退出，不空跑 Agent 会话
#   - flock 防重入：同一时刻只允许一个自动修复实例（锁由 Agent 所在进程持有）
#   - 脚本本身赋予 755 权限
# ---------------------------------------------------------------

set -u

# ---------- 配置 ----------
PROJECT_DIR="/usr/local/LsmAgentGame/LsmAgentGame"
LOG_DIR="${PROJECT_DIR}/logs"
PROMPT_FILE_NAME="AutoDebugTestReport.md"
TS="$(date +%Y%m%d_%H%M%S)"
LOG_FILE="${LOG_DIR}/auto_debug_${TS}.log"
LOCK_FILE="${LOG_DIR}/auto_debug.lock"

mkdir -p "${LOG_DIR}"

# ---------- 加载多 Agent 公共库 ----------
COMMON_LIB="${PROJECT_DIR}/agent_cli_common.sh"
if [[ ! -f "${COMMON_LIB}" ]]; then
    echo "[ERROR] 缺少公共库 ${COMMON_LIB}，无法选择 Agent CLI。" >&2
    exit 1
fi
# shellcheck source=agent_cli_common.sh
source "${COMMON_LIB}"

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

# ---------- 规则文件守门（防止 prompt 被改回老版本）----------
# AutoDebugTestReport.md 必须显式声明"禁止修改 CLAUDE.md/AGENTS.md"，
# 否则视为脚本被误改，提示人工修复并退出。
# 注：该守门条款对四种 Agent 同样有效（均读取同一份提示词文件）。
if ! grep -qF "绝对禁止修改 \`CLAUDE.md\`、\`AGENTS.md\`" "${PROMPT_FILE}" 2>/dev/null; then
    echo "[ERROR] ${PROMPT_FILE} 缺少「绝对禁止修改 CLAUDE.md/AGENTS.md」守门条款，拒绝启动。" >&2
    echo "[ERROR] 请先修复 ${PROMPT_FILE} 再重跑本脚本。" >&2
    exit 2
fi

# ---------- 待处理报告预检 ----------
# 无待处理报告（全部已删除或已加 _无问题 后缀）时直接退出，避免空跑一个 Agent 会话。
# 与 AutoDebugTestReport.md §1「无任何待处理报告 → 静默结束」同语义，但省掉会话开销。
PENDING_MAIN="$(find "${PROJECT_DIR}/TestReport" -maxdepth 1 -name '自动化测试报告_*.md' ! -name '*_无问题.md' 2>/dev/null | head -1)"
PENDING_SUB="$(find "${PROJECT_DIR}/go-web-debug-tool/UseReport" -maxdepth 1 -name '测试工具使用报告_*.md' ! -name '*_无问题.md' 2>/dev/null | head -1)"
if [[ -z "${PENDING_MAIN}${PENDING_SUB}" ]]; then
    echo "[AutoDebugTestReport] 无待处理报告（TestReport/ 与 UseReport/ 均为空或已归档），静默结束。"
    exit 0
fi

cd "${PROJECT_DIR}" || { echo "[ERROR] 无法进入 ${PROJECT_DIR}"; exit 1; }

# ---------- 随机选择 Agent ----------
pick_agent "AutoDebugTestReport"
AGENT_BIN_PATH="$(command -v "$(agent_binary_of "${SELECTED_AGENT}")" 2>/dev/null || echo 'NOT FOUND')"

{
    echo "============================================================"
    echo "[AutoDebugTestReport] 启动时间 : $(date '+%F %T')"
    echo "[AutoDebugTestReport] 工作目录 : ${PROJECT_DIR}"
    echo "[AutoDebugTestReport] 提示词文件: ${PROMPT_FILE}"
    echo "[AutoDebugTestReport] 日志文件  : ${LOG_FILE}"
    echo "[AutoDebugTestReport] 选中 Agent : ${SELECTED_AGENT}"
    echo "[AutoDebugTestReport] Agent 二进制: ${AGENT_BIN_PATH}"
    echo "============================================================"
} >> "${LOG_FILE}"

# ---------- 启动（后台脱离，不阻塞调用者）----------
# setsid  : 创建新会话，调用者退出也不会被 SIGHUP
# nohup   : 忽略 SIGHUP
# & + disown: 与当前 shell 解除关系
# stdout/stderr → 日志文件
nohup setsid bash -c "
    # 防重入：锁由本进程持有至 Agent 退出；接力/手动重复触发时多余实例直接退出
    exec 9>>'${LOCK_FILE}'
    if ! flock -n 9; then
        echo '[AutoDebugTestReport] 已有自动修复实例在运行，本次退出(防重入)。'
        exit 0
    fi
    cd '${PROJECT_DIR}'
    source '${COMMON_LIB}'
    run_agent_with_prompt '${SELECTED_AGENT}' '${PROMPT_FILE}' '${PROJECT_DIR}'
    AGENT_EXIT=\$?
    echo '[AutoDebugTestReport] ${SELECTED_AGENT} 退出码 : '\${AGENT_EXIT}
    echo '[AutoDebugTestReport] 结束时间 : '\"\$(date '+%F %T')\"
" >> "${LOG_FILE}" 2>&1 </dev/null &

AGENT_PID=$!
disown "${AGENT_PID}" 2>/dev/null || true

echo "[AutoDebugTestReport] 已后台启动 Agent [${SELECTED_AGENT}]，PID=${AGENT_PID}"
echo "[AutoDebugTestReport] 日志 : ${LOG_FILE}"
echo "[AutoDebugTestReport] 调用者可继续执行其他操作，不会被阻塞。"
