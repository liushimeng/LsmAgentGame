#!/usr/bin/env bash
# AutoTestAndDebug_Debate.sh
# ---------------------------------------------------------------
# 用途：
#   辩论比赛 Agent 专用自动化测试入口。随机选择一个可用的编程 Agent CLI
#   （Claude Code / OpenCode / Codex），读取当前目录或仓库根
#   的 AutoTestAndDebug_Debate.md 作为提示词执行自动化测试；Agent
#   退出后自动将 TestReport 中以「辩论比赛自动化测试报告_」开头的辩论
#   报告文件以中文 git 提交，子模块 UseReport 在子仓库内单独提交。
#   提示词已内嵌「自动修复流程」章节，Agent 测试完成后在同一会话内
#   直接执行修复 + 提交推送。
#
# 特性：
#   - 工作目录与 Agent 启动目录均为 /usr/local/LsmAgentGame/LsmAgentGame
#   - 支持全部编程 Agent CLI（Claude Code / OpenCode / Codex）随机选择执行
#   - 通过 nohup + setsid + & + disown 脱离调用者，**不阻塞**调用者进程
#   - 日志体系：
#     * 单次运行日志 ./logs/auto_test_debug_debate_<Agent程序名>_<timestamp>.log
#     * 运行索引日志 ./logs/auto_run_index.log
#     * 旧运行日志超过保留期自动清理
#   - AutoTestAndDebug_Debate.md 优先取当前目录，其次仓库根
#   - Agent 退出后自动执行 `git add` + `git commit`（中文提交信息，逐路径容错）
#   - **辩论比赛专用**：仅扫描 `辩论比赛自动化测试报告_*.md` 主报告 glob
#     + `辩论比赛测试工具使用报告_*.md` 工具报告 glob
#   - 提示词内嵌「自动修复流程」，Agent 测试完成后直接修复 + 提交推送
#   - 脚本本身赋予 755 权限
#
# 公共库依赖:
#   - agent_cli_common.sh: Agent CLI 选择 + 调用
#   - auto_run_common.sh:   多游戏 glob 单一事实源 + 启动日志头 + 后台启动封装
# ---------------------------------------------------------------

set -u

# ---------- 配置 ----------
PROJECT_DIR="/usr/local/LsmAgentGame/LsmAgentGame"
LOG_DIR="${PROJECT_DIR}/logs"
SCRIPT_TAG="AutoTestAndDebug_Debate"
PROMPT_FILE_NAME="AutoTestAndDebug_Debate.md"
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

# ---------- 随机选择 Agent ----------
# claude|opencode|codex 随机选取；AGENT_CLI 环境变量可强制指定
pick_agent "${SCRIPT_TAG}"

# ---------- 日志文件名含 Agent 程序名 ----------
LOG_FILE="${LOG_DIR}/auto_test_debug_debate_${SELECTED_AGENT}_${TS}.log"

# ---------- 启动日志头 + 运行索引 ----------
print_section_header "${SCRIPT_TAG}" "${PROMPT_FILE}" "${LOG_FILE}" "${PROJECT_DIR}" "${SELECTED_AGENT}"
append_run_index "script=${SCRIPT_TAG}" "agent=${SELECTED_AGENT}" "event=start" "log=logs/$(basename "${LOG_FILE}")"

# ---------- 启动（后台脱离，不阻塞调用者）----------
BG_PID="$(start_agent_in_background "${LOG_FILE}" "
    cd '${PROJECT_DIR}'
    source '${AGENT_LIB}'
    source '${AUTO_LIB}'

    # ------- 1. 运行自动化测试（提示词内嵌自动修复流程）-------
    bg_log '${SCRIPT_TAG}' '开始运行 ${SELECTED_AGENT}，提示词文件: ${PROMPT_FILE}'
    run_agent_with_prompt '${SELECTED_AGENT}' '${PROMPT_FILE}' '${PROJECT_DIR}'
    AGENT_EXIT=\$?
    bg_log '${SCRIPT_TAG}' '${SELECTED_AGENT} 退出码 : '\${AGENT_EXIT}
    append_run_index script=${SCRIPT_TAG} agent=${SELECTED_AGENT} event=agent_done exit=\"\${AGENT_EXIT}\" log=logs/$(basename "${LOG_FILE}")

    # ------- 2. 用中文 git 自动提交测试报告 -------
    bg_log '${SCRIPT_TAG}' '开始 git 自动提交...'

    # 注(§20260820-03)：TestReport/* 已整目录入 .gitignore(报告处理完即删,不在仓库
    # 堆积),本节 git add 通常无暂存内容、提交自动跳过,保留以兼容未来策略调整。
    # 辩论比赛专用：仅 add 辩论比赛主报告 + 协议抓包报告 glob
    DEBATE_MAIN_GLOB=\"\$(enqueue_game_glob debate main)\"
    DEBATE_PROTOCOL_GLOB=\"\$(enqueue_game_glob debate protocol)\"
    git_add_safe \"TestReport/\${DEBATE_MAIN_GLOB}\" || bg_log '${SCRIPT_TAG}' '警告: TestReport/辩论比赛主报告无可暂存内容(已忽略)'
    git_add_safe \"TestReport/\${DEBATE_PROTOCOL_GLOB}\" 2>/dev/null || true

    # 子模块 UseReport 需在子仓库内先提交，再回主仓库暂存 gitlink
    DEBATE_USAGE_GLOB=\"\$(enqueue_game_glob debate usage)\"
    if [[ -d go-web-debug-tool/UseReport ]]; then
        DEBATE_USAGE_FILES=\$(find go-web-debug-tool/UseReport -maxdepth 1 -name \"\${DEBATE_USAGE_GLOB}\" ! -name '*_无问题.md' 2>/dev/null)
        if [[ -n \"\${DEBATE_USAGE_FILES}\" ]]; then
            git -C go-web-debug-tool add -- UseReport/ 2>/dev/null || true
            if ! git -C go-web-debug-tool diff --cached --quiet 2>/dev/null; then
                if git -C go-web-debug-tool commit -m \"测试: 辩论比赛工具使用报告自动提交 ${TS}\" 2>/dev/null; then
                    bg_log '${SCRIPT_TAG}' '子模块 UseReport 提交成功'
                else
                    bg_log '${SCRIPT_TAG}' '子模块提交失败(不阻塞主流程)'
                fi
            fi
            git_add_safe 'go-web-debug-tool'
        fi
    fi

    # 检查是否有需要提交的变更
    if git diff --cached --quiet; then
        bg_log '${SCRIPT_TAG}' '暂存区无变更，跳过提交。'
        append_run_index script=${SCRIPT_TAG} agent=${SELECTED_AGENT} event=commit_skip
    else
        COMMIT_TS=\"\$(date '+%Y%m%d_%H%M%S')\"
        if git_commit_chinese '测试' 'debate' \"\${COMMIT_TS}\" '${SCRIPT_TAG}.sh' 'TestReport/辩论比赛报告 + go-web-debug-tool 子模块 gitlink(如有)'; then
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
echo "[${SCRIPT_TAG}] Agent 退出后会自动 git add + git commit（中文提交信息），"
echo "[${SCRIPT_TAG}] 提示词内嵌「自动修复流程」，Agent 测试完成后直接修复 + 提交推送。"
echo "[${SCRIPT_TAG}] 调用者可继续执行其他操作，不会被阻塞。"
