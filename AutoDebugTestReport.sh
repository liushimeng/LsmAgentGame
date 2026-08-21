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
#   - 日志体系（§20260821-01 增强）：
#     * 单次运行日志 ./logs/auto_debug_<Agent程序名>_<timestamp>.log
#       （文件名含启动的 Agent 程序名；日志头记录启动时间/工作目录/提示词/
#        Agent 二进制路径/Git HEAD，正文为 Agent 全程 stdout/stderr）
#     * 运行索引日志 ./logs/auto_run_index.log：每次运行追加 key=value 单行
#       （脚本名/Agent 程序名/事件/时间/PID/退出码），便于事后 grep 审计
#     * 旧运行日志超过保留期（缺省 30 天，LOG_RETAIN_DAYS 可覆盖）自动清理
#   - AutoDebugTestReport.md 优先取当前目录，其次仓库根；都不存在则立即报错退出
#   - 启动前预检：无待处理报告时直接退出（并在索引日志记录 skip），不空跑 Agent 会话
#   - flock 防重入：同一时刻只允许一个自动修复实例（锁由 Agent 所在进程持有）
#   - 脚本本身赋予 755 权限
#
# 公共库依赖(§20260820-01 重构):
#   - agent_cli_common.sh: Agent CLI 选择 + 调用
#   - auto_run_common.sh:   多游戏 glob 单一事实源 + 启动日志头 + 后台启动封装
#                           + 统一日志工具(§20260821-01)
# ---------------------------------------------------------------

set -u

# ---------- 配置 ----------
PROJECT_DIR="/usr/local/LsmAgentGame/LsmAgentGame"
LOG_DIR="${PROJECT_DIR}/logs"
SCRIPT_TAG="AutoDebugTestReport"
PROMPT_FILE_NAME="AutoDebugTestReport.md"
TS="$(date +%Y%m%d_%H%M%S)"
LOCK_FILE="${LOG_DIR}/auto_debug.lock"

mkdir -p "${LOG_DIR}"

# ---------- 加载多 Agent 公共库 + 自动化脚本公共库 ----------
AGENT_LIB="${PROJECT_DIR}/agent_cli_common.sh"
AUTO_LIB="${PROJECT_DIR}/auto_run_common.sh"
for lib in "${AGENT_LIB}" "${AUTO_LIB}"; do
    if [[ ! -f "${lib}" ]]; then
        echo "[ERROR] 缺少公共库 ${lib}，无法启动。" >&2
        exit 1
    fi
    # shellcheck source=agent_cli_common.sh
    # shellcheck source=auto_run_common.sh
    source "${lib}"
done

# ---------- 清理过期日志（保留期缺省 30 天，LOG_RETAIN_DAYS 可覆盖） ----------
cleanup_old_logs

# ---------- 定位提示词文件 ----------
PROMPT_FILE="$(locate_prompt_file "${PROMPT_FILE_NAME}")" || {
    append_run_index "script=${SCRIPT_TAG}" "agent=none" "event=error_prompt_missing" "exit=1"
    exit 1
}

# ---------- 规则文件守门（防止 prompt 被改回老版本）----------
# AutoDebugTestReport.md 必须显式声明"禁止修改 CLAUDE.md/AGENTS.md"，
# 否则视为脚本被误改，提示人工修复并退出。
# 注：该守门条款对四种 Agent 同样有效（均读取同一份提示词文件）。
if ! grep -qF "绝对禁止修改 \`CLAUDE.md\`、\`AGENTS.md\`" "${PROMPT_FILE}" 2>/dev/null; then
    echo "[ERROR] ${PROMPT_FILE} 缺少「绝对禁止修改 CLAUDE.md/AGENTS.md」守门条款，拒绝启动。" >&2
    echo "[ERROR] 请先修复 ${PROMPT_FILE} 再重跑本脚本。" >&2
    append_run_index "script=${SCRIPT_TAG}" "agent=none" "event=error_prompt_guard" "exit=2"
    exit 2
fi

# ---------- 待处理报告预检（多游戏 union） ----------
# 无待处理报告（全部已删除或已加 _无问题 后缀）时直接退出，避免空跑一个 Agent 会话。
# 与 AutoDebugTestReport.md §1「无任何待处理报告 → 静默结束」同语义，但省掉会话开销。
#
# 多游戏 union glob 来源(§20260820-01 改为单一事实源):
#   - 主工程: auto_run_common.sh::GAME_GLOBS 中所有 *_main glob(去重 _legacy)
#   - 子工程: go-web-debug-tool/UseReport/ 下测试工具使用报告_*.md(固定)
#   - 新增游戏: 只需在 auto_run_common.sh::GAME_GLOBS 加一行,本脚本零改动。
PENDING_MAIN=""
PENDING_MAIN="$(any_pending_report "${PROJECT_DIR}")"
PENDING_SUB="$(find "${PROJECT_DIR}/go-web-debug-tool/UseReport" -maxdepth 1 \( -name '测试工具使用报告_*.md' -o -name '*测试工具使用报告_*.md' \) ! -name '*_无问题.md' 2>/dev/null | head -1)"
if [[ -z "${PENDING_MAIN}${PENDING_SUB}" ]]; then
    echo "[${SCRIPT_TAG}] 无待处理报告（TestReport/ 与 UseReport/ 均为空或已归档），静默结束。"
    append_run_index "script=${SCRIPT_TAG}" "agent=none" "event=skip_no_pending_report" "exit=0"
    exit 0
fi

cd "${PROJECT_DIR}" || { echo "[ERROR] 无法进入 ${PROJECT_DIR}"; exit 1; }

# ---------- 随机选择 Agent ----------
pick_agent "${SCRIPT_TAG}"

# ---------- 日志文件名含 Agent 程序名（§20260821-01） ----------
LOG_FILE="${LOG_DIR}/auto_debug_${SELECTED_AGENT}_${TS}.log"

# ---------- 启动日志头 + 运行索引 ----------
print_section_header "${SCRIPT_TAG}" "${PROMPT_FILE}" "${LOG_FILE}" "${PROJECT_DIR}" "${SELECTED_AGENT}"
append_run_index "script=${SCRIPT_TAG}" "agent=${SELECTED_AGENT}" "event=start" "log=logs/$(basename "${LOG_FILE}")"

# ---------- 启动（后台脱离，不阻塞调用者）----------
# setsid  : 创建新会话，调用者退出也不会被 SIGHUP
# nohup   : 忽略 SIGHUP
# & + disown: 与当前 shell 解除关系
# stdout/stderr → 日志文件（文件名已含 Agent 程序名）
# flock 防重入：锁由本进程持有至 Agent 退出；接力/手动重复触发时多余实例直接退出
BG_PID="$(start_agent_in_background "${LOG_FILE}" "
    cd '${PROJECT_DIR}'
    source '${AGENT_LIB}'
    source '${AUTO_LIB}'

    # ------- flock 防重入 -------
    exec 9>>'${LOCK_FILE}'
    if ! flock -n 9; then
        bg_log '${SCRIPT_TAG}' '已有自动修复实例在运行，本次退出(防重入)。'
        append_run_index script=${SCRIPT_TAG} agent=${SELECTED_AGENT} event=skip_locked exit=0
        exit 0
    fi

    # ------- 运行自动修复 Agent -------
    bg_log '${SCRIPT_TAG}' '开始运行 ${SELECTED_AGENT}，提示词文件: ${PROMPT_FILE}'
    run_agent_with_prompt '${SELECTED_AGENT}' '${PROMPT_FILE}' '${PROJECT_DIR}'
    AGENT_EXIT=\$?
    bg_log '${SCRIPT_TAG}' '${SELECTED_AGENT} 退出码 : '\${AGENT_EXIT}
    append_run_index script=${SCRIPT_TAG} agent=${SELECTED_AGENT} event=agent_done exit=\"\${AGENT_EXIT}\" log=logs/$(basename "${LOG_FILE}")
    bg_log '${SCRIPT_TAG}' '全流程结束'
    append_run_index script=${SCRIPT_TAG} agent=${SELECTED_AGENT} event=done exit=\"\${AGENT_EXIT}\"
")"
append_run_index "script=${SCRIPT_TAG}" "agent=${SELECTED_AGENT}" "event=launched" "pid=${BG_PID}"

echo "[${SCRIPT_TAG}] 已后台启动 Agent [${SELECTED_AGENT}] (PID: ${BG_PID})"
echo "[${SCRIPT_TAG}] 日志 : ${LOG_FILE}"
echo "[${SCRIPT_TAG}] 运行索引日志 : ${LOG_DIR}/auto_run_index.log"
echo "[${SCRIPT_TAG}] 调用者可继续执行其他操作，不会被阻塞。"
