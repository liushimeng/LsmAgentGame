#!/usr/bin/env bash
# AutoScreenshot_TexasPoker.sh
# ---------------------------------------------------------------
# 用途：
#   德州扑克 2-6 人 Agent 专用自动化截图入口。随机选择一个可用的编程
#   Agent CLI（Claude Code / OpenCode / Hermes / OpenClaw），读取当前目录
#   或仓库根的 AutoScreenshot_TexasPoker.md 作为提示词执行德扑精彩画面
#   截图采集；Agent 退出后自动将截图与报告文件以中文 git 提交，全程
#   bypass 权限。
#
# 特性（与 AutoScreenshot_Werewolf.sh 同根，文件名特化）：
#   - 工作目录与 Agent 启动目录均为 /usr/local/LsmAgentGame/LsmAgentGame
#   - 支持全部编程 Agent CLI（Claude Code / OpenCode / Hermes / OpenClaw）
#     随机选择执行(§20260821-02)；选择逻辑在公共库 agent_cli_common.sh 中
#     （可 source 复用）；AGENT_CLI 环境变量可强制指定某个 Agent
#   - 通过 nohup + setsid + & + disown 脱离调用者，**不阻塞**调用者进程
#   - 日志体系（§20260821-01 增强）：
#     * 单次运行日志 ./logs/auto_screenshot_texasholdem_<Agent程序名>_<timestamp>.log
#       （文件名含启动的 Agent 程序名；日志头记录启动时间/工作目录/提示词/
#        Agent 二进制路径/Git HEAD，正文为 Agent 全程 stdout/stderr + git 提交过程）
#     * 运行索引日志 ./logs/auto_run_index.log：每次运行追加 key=value 单行
#       （脚本名/Agent 程序名/事件/时间/PID/退出码），便于事后 grep 审计
#     * 旧运行日志超过保留期（缺省 30 天，LOG_RETAIN_DAYS 可覆盖）自动清理
#   - AutoScreenshot_TexasPoker.md 优先取当前目录，其次仓库根；都不存在则立即报错退出
#   - Agent 退出后自动执行 `git add` + `git commit`（中文提交信息）
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
SCRIPT_TAG="AutoScreenshot_TexasPoker"
PROMPT_FILE_NAME="AutoScreenshot_TexasPoker.md"
TS="$(date +%Y%m%d_%H%M%S)"

mkdir -p "${LOG_DIR}"

# ---------- 加载公共库 ----------
AGENT_LIB="${PROJECT_DIR}/agent_cli_common.sh"
AUTO_LIB="${PROJECT_DIR}/auto_run_common.sh"
for lib in "${AGENT_LIB}" "${AUTO_LIB}"; do
    if [[ ! -f "${lib}" ]]; then
        echo "[ERROR] 缺少公共库 ${lib}，无法启动。" >&2
        exit 1
    fi
    source "${lib}"
done

# ---------- 清理过期日志（保留期缺省 30 天，LOG_RETAIN_DAYS 可覆盖） ----------
cleanup_old_logs

# ---------- 定位提示词文件 ----------
PROMPT_FILE="$(locate_prompt_file "${PROMPT_FILE_NAME}")" || {
    append_run_index "script=${SCRIPT_TAG}" "agent=none" "event=error_prompt_missing" "exit=1"
    exit 1
}

cd "${PROJECT_DIR}" || { echo "[ERROR] 无法进入 ${PROJECT_DIR}"; exit 1; }

# ---------- 随机选择 Agent（§20260821-02：全部可用 Agent 均支持执行） ----------
# claude|opencode|hermes|openclaw 随机选取；AGENT_CLI 环境变量可强制指定
pick_agent "${SCRIPT_TAG}"

# ---------- 日志文件名含 Agent 程序名（§20260821-01） ----------
LOG_FILE="${LOG_DIR}/auto_screenshot_texasholdem_${SELECTED_AGENT}_${TS}.log"

# ---------- 启动日志头 + 运行索引 ----------
print_section_header "${SCRIPT_TAG}" "${PROMPT_FILE}" "${LOG_FILE}" "${PROJECT_DIR}" "${SELECTED_AGENT}"
append_run_index "script=${SCRIPT_TAG}" "agent=${SELECTED_AGENT}" "event=start" "log=logs/$(basename "${LOG_FILE}")"

# ---------- 启动（后台脱离，不阻塞调用者）----------
BG_PID="$(start_agent_in_background "${LOG_FILE}" "
    cd '${PROJECT_DIR}'
    source '${AGENT_LIB}'
    source '${AUTO_LIB}'

    # ------- 1. 运行自动化截图 -------
    bg_log '${SCRIPT_TAG}' '开始运行 ${SELECTED_AGENT}，提示词文件: ${PROMPT_FILE}'
    run_agent_with_prompt '${SELECTED_AGENT}' '${PROMPT_FILE}' '${PROJECT_DIR}'
    AGENT_EXIT=\$?
    bg_log '${SCRIPT_TAG}' '${SELECTED_AGENT} 退出码 : '\${AGENT_EXIT}
    append_run_index script=${SCRIPT_TAG} agent=${SELECTED_AGENT} event=agent_done exit=\"\${AGENT_EXIT}\" log=logs/$(basename "${LOG_FILE}")

    # ------- 2. 用中文 git 自动提交截图 -------
    bg_log '${SCRIPT_TAG}' '开始 git 自动提交...'

    # 暂存截图与报告（避免误暂存业务代码的未预期改动）
    # 德扑专用：仅 add texaspoker-*.png 与德扑相关文件；狼人杀截图由 Werewolf 脚本单独处理。
    TEXAS_SHOT_GLOB=\"\$(enqueue_game_glob texasholdem screenshot)\"
    TEXAS_SHOT_PROG_GLOB=\"\$(enqueue_game_glob texasholdem screenshot_progress)\"
    TEXAS_MAIN_GLOB=\"\$(enqueue_game_glob texasholdem main)\"
    git_add_safe 'ProjectPic/texaspoker-*.png'
    git_add_safe \"TestReport/\${TEXAS_SHOT_GLOB}\" 2>/dev/null || true
    git_add_safe \"TestReport/\${TEXAS_MAIN_GLOB}\" 2>/dev/null || true
    git_add_safe \"AutoScreenshotProgress/\${TEXAS_SHOT_PROG_GLOB}\" 2>/dev/null || true
    git_add_safe 'scripts/screenshot/texasholdem_highlight_capture.py'
    git_add_safe 'AutoScreenshot_TexasPoker.md'
    git_add_safe 'AutoScreenshot_TexasPoker.sh'
    git_add_safe 'README.md'
    git_add_safe 'README.en.md'
    git_add_safe 'README.ja.md'

    # 检查是否有需要提交的变更
    if git diff --cached --quiet; then
        bg_log '${SCRIPT_TAG}' '暂存区无变更，跳过提交。'
        append_run_index script=${SCRIPT_TAG} agent=${SELECTED_AGENT} event=commit_skip
    else
        COMMIT_TS=\"\$(date '+%Y%m%d_%H%M%S')\"
        # 使用中文提交信息（UTF-8）
        if git commit -m \"截图: 德州扑克 2-6 人局实机截图 \${COMMIT_TS} 已完成\" \\
                -m \"自动提交由 ${SCRIPT_TAG}.sh 生成\" \\
                -m \"重点: 1 名人类玩家 + N Agent 混合对局\"; then
            COMMIT_HASH=\"\$(git rev-parse --short HEAD 2>/dev/null)\"
            bg_log '${SCRIPT_TAG}' 'git 提交成功: '\${COMMIT_HASH}
            append_run_index script=${SCRIPT_TAG} agent=${SELECTED_AGENT} event=commit_done commit=\"\${COMMIT_HASH}\"
        else
            bg_log '${SCRIPT_TAG}' 'git 提交失败，请人工检查。'
            append_run_index script=${SCRIPT_TAG} agent=${SELECTED_AGENT} event=commit_failed
        fi
    fi

    bg_log '${SCRIPT_TAG}' '全流程结束'
    append_run_index script=${SCRIPT_TAG} agent=${SELECTED_AGENT} event=done exit=\"\${AGENT_EXIT}\"
")"
append_run_index "script=${SCRIPT_TAG}" "agent=${SELECTED_AGENT}" "event=launched" "pid=${BG_PID}"

echo "[${SCRIPT_TAG}] 已后台启动 Agent [${SELECTED_AGENT}] (PID: ${BG_PID})"
echo "[${SCRIPT_TAG}] 日志 : ${LOG_FILE}"
echo "[${SCRIPT_TAG}] 运行索引日志 : ${LOG_DIR}/auto_run_index.log"
echo "[${SCRIPT_TAG}] Agent 退出后会自动 git add + git commit（中文提交信息）。"
echo "[${SCRIPT_TAG}] 调用者可继续执行其他操作，不会被阻塞。"
