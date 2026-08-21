#!/usr/bin/env bash
# AutoTestAndSaveReport_Werewolf.sh
# ---------------------------------------------------------------
# 用途：
#   狼人杀 13 人局专用自动化测试入口。随机选择一个可用的编程 Agent CLI
#   （Claude Code / OpenCode / Hermes / OpenClaw），读取当前目录或仓库根
#   的 AutoTestAndSaveReport_Werewolf.md 作为提示词执行自动化测试；Agent
#   退出后自动将 TestReport 中以「狼人杀自动化测试报告_」开头的狼人杀报告
#   文件以中文 git 提交（兼容旧 `自动化测试报告_*.md`），子模块 UseReport
#   在子仓库内单独提交，随后由 shell 层确定性接力启动 AutoDebugTestReport.sh
#   进入自动修复流程，全程 bypass 权限。
#
# 特性（与 AutoDebugTestReport.sh 同根，文件名特化）：
#   - 工作目录与 Agent 启动目录均为 /usr/local/LsmAgentGame/LsmAgentGame
#   - 多 Agent 随机选择逻辑在公共库 agent_cli_common.sh 中（可 source 复用）；
#     AGENT_CLI 环境变量可强制指定某个 Agent（claude|opencode|hermes|openclaw）
#   - 通过 nohup + setsid + & + disown 脱离调用者，**不阻塞**调用者进程
#   - 日志体系（§20260821-01 增强）：
#     * 单次运行日志 ./logs/auto_test_werewolf_<Agent程序名>_<timestamp>.log
#       （文件名含启动的 Agent 程序名；日志头记录启动时间/工作目录/提示词/
#        Agent 二进制路径/Git HEAD，正文为 Agent 全程 stdout/stderr +
#        git 提交过程 + 接力启动过程，关键节点均带时间戳）
#     * 运行索引日志 ./logs/auto_run_index.log：每次运行追加 key=value 单行
#       （脚本名/Agent 程序名/事件/时间/PID/退出码），便于事后 grep 审计
#     * 旧运行日志超过保留期（缺省 30 天，LOG_RETAIN_DAYS 可覆盖）自动清理
#   - AutoTestAndSaveReport_Werewolf.md 优先取当前目录，其次仓库根；都不存在则立即报错退出
#   - Agent 退出后自动执行 `git add` + `git commit`（中文提交信息，逐路径容错）
#   - git 提交后由 shell **确定性接力**启动 AutoDebugTestReport.sh
#     （不依赖测试 Agent 自觉执行，避免「声明了却从不接线」断链；含待处理报告预检）
#     接力阶段会再次随机选择 Agent（每次脚本运行独立随机）
#   - **狼人杀专用**：仅扫描 `狼人杀自动化测试报告_*.md` 主报告 glob
#     （兼容旧 `自动化测试报告_*.md`）+ `狼人杀测试工具使用报告_*.md` 工具报告 glob
#     （兼容旧 `测试工具使用报告_*.md`）；德扑报告由 AutoTestAndSaveReport_TexasPoker.sh 单独提交
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
SCRIPT_TAG="AutoTestAndSaveReport_Werewolf"
PROMPT_FILE_NAME="AutoTestAndSaveReport_Werewolf.md"
# 狼人杀主报告 glob(新前缀 + 兼容旧)— 来自 auto_run_common.sh::GAME_GLOBS
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
pick_agent "${SCRIPT_TAG}"

# ---------- 日志文件名含 Agent 程序名（§20260821-01） ----------
LOG_FILE="${LOG_DIR}/auto_test_werewolf_${SELECTED_AGENT}_${TS}.log"

# ---------- 启动日志头 + 运行索引 ----------
print_section_header "${SCRIPT_TAG}" "${PROMPT_FILE}" "${LOG_FILE}" "${PROJECT_DIR}" "${SELECTED_AGENT}"
append_run_index "script=${SCRIPT_TAG}" "agent=${SELECTED_AGENT}" "event=start" "log=logs/$(basename "${LOG_FILE}")"

# ---------- 启动（后台脱离，不阻塞调用者）----------
# 整个生命周期在同一个 bash -c 子 shell 中顺序执行：
#   1. 选中的 Agent CLI 读取提示词文件执行测试（放开权限，全程自动化）
#   2. Agent 退出后自动 git add / git commit（中文提交信息）
#   3. shell 层确定性接力启动 AutoDebugTestReport.sh
BG_PID="$(start_agent_in_background "${LOG_FILE}" "
    cd '${PROJECT_DIR}'
    source '${AGENT_LIB}'
    source '${AUTO_LIB}'

    # ------- 1. 运行自动化测试 -------
    bg_log '${SCRIPT_TAG}' '开始运行 ${SELECTED_AGENT}，提示词文件: ${PROMPT_FILE}'
    run_agent_with_prompt '${SELECTED_AGENT}' '${PROMPT_FILE}' '${PROJECT_DIR}'
    AGENT_EXIT=\$?
    bg_log '${SCRIPT_TAG}' '${SELECTED_AGENT} 退出码 : '\${AGENT_EXIT}
    append_run_index script=${SCRIPT_TAG} agent=${SELECTED_AGENT} event=agent_done exit=\"\${AGENT_EXIT}\" log=logs/$(basename "${LOG_FILE}")

    # ------- 2. 用中文 git 自动提交测试报告 -------
    bg_log '${SCRIPT_TAG}' '开始 git 自动提交...'

    # 逐路径暂存：任一目录不存在 / 被 .gitignore 忽略时不阻塞其它目录。
    # 教训(20260812)：旧写法把三个目录塞在同一条 git add 里 —— AutoTestProgress/
    # 被 .gitignore 整体忽略 + 子模块内路径在父仓库 add 直接 fatal(exit=128)，
    # 任一失败都让整条 add 原子性落空，报告从未被暂存，日志却显示「暂存区无变更」。
    # 注：AutoTestProgress/ 按 .gitignore 策略为本地进度文件，不入库。
    # 狼人杀专用：仅 add `狼人杀自动化测试报告_*.md`(新) + 兼容旧 `自动化测试报告_*.md`；
    # 德扑报告由 TexasPoker 脚本单独 add，互不影响。
    # 修复(§20260820-03)：enqueue_game_glob 多 kind 输出的是 find -o 串联形式,仅供 find
    # 未加引号使用;quoted git add 必须逐 glob 调用,否则整串成为永不命中的字面路径。
    # 注(§20260820-03)：TestReport/* 已整目录入 .gitignore(报告处理完即删,不在仓库堆积),
    # 本节 git add 通常无暂存内容、提交自动跳过,保留以兼容未来策略调整。
    git_add_safe \"TestReport/\$(enqueue_game_glob werewolf main)\" || bg_log '${SCRIPT_TAG}' '警告: TestReport/狼人杀报告无可暂存内容(已忽略)'
    git_add_safe \"TestReport/\$(enqueue_game_glob werewolf main_legacy)\" 2>/dev/null || true

    # 子模块 UseReport 需在子仓库内先提交，再回主仓库暂存 gitlink
    # 教训(20260819)：用 find -name 动态发现狼人杀工具报告文件名（避免硬编码时间戳）
    # 注(§20260821-01)：此注释位于双引号 BG_SCRIPT 串内，反引号会触发构造期命令替换，禁用。
    # 修复(§20260820-03)：多 glob 需逐 kind 调用并用 -o 串联;quoted -name 只接受单 glob,
    # 旧写法把 \"glob1 -o -name glob2\" 整串当一个 pattern,永远匹配不到文件。
    if [[ -d go-web-debug-tool/UseReport ]]; then
        WEREWOLF_USAGE_FILES=\$(find go-web-debug-tool/UseReport -maxdepth 1 \( -name \"\$(enqueue_game_glob werewolf usage)\" -o -name \"\$(enqueue_game_glob werewolf usage_legacy)\" \) ! -name '*_无问题.md' 2>/dev/null)
        if [[ -n \"\${WEREWOLF_USAGE_FILES}\" ]]; then
            git -C go-web-debug-tool add -- UseReport/ 2>/dev/null || true
            if ! git -C go-web-debug-tool diff --cached --quiet 2>/dev/null; then
                if git -C go-web-debug-tool commit -m \"测试: 狼人杀工具使用报告自动提交 ${TS}\" 2>/dev/null; then
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
        if git_commit_chinese '测试' 'werewolf' \"\${COMMIT_TS}\" '${SCRIPT_TAG}.sh' 'TestReport/狼人杀报告 + go-web-debug-tool 子模块 gitlink(如有)'; then
            COMMIT_HASH=\"\$(git rev-parse --short HEAD 2>/dev/null)\"
            bg_log '${SCRIPT_TAG}' 'git 提交成功: '\${COMMIT_HASH}
            append_run_index script=${SCRIPT_TAG} agent=${SELECTED_AGENT} event=commit_done commit=\"\${COMMIT_HASH}\"
        else
            bg_log '${SCRIPT_TAG}' 'git 提交失败，请人工检查。'
            append_run_index script=${SCRIPT_TAG} agent=${SELECTED_AGENT} event=commit_failed
        fi
    fi

    # ------- 3. 确定性接力：shell 层自动启动自动修复流程 -------
    # 旧设计靠测试 Agent 自觉执行 AutoDebugTestReport.sh（prompt §8.3），实测会被
    # 跳过（logs/ 无任何 auto_debug_*.log），属「声明了却从不接线」反模式；
    # 改为脚本层接力，先预检待处理报告，避免空跑 Agent 会话。
    # 注：接力脚本内部会再次随机选择 Agent（每次脚本运行独立随机）。
    PENDING_WEREWOLF=\$(scan_game_report TestReport werewolf main)
    PENDING_SUB=\$(find go-web-debug-tool/UseReport -maxdepth 1 \( -name \"\$(enqueue_game_glob werewolf usage)\" -o -name \"\$(enqueue_game_glob werewolf usage_legacy)\" \) ! -name '*_无问题.md' 2>/dev/null | head -1)
    PENDING_TEXAS=\$(scan_game_report TestReport texasholdem main)
    # 狼人杀接力：仅扫描本游戏 glob + 子模块；德扑报告留给德扑接力脚本处理。
    if [[ -n \"\${PENDING_WEREWOLF}\${PENDING_SUB}\" ]]; then
        bg_log '${SCRIPT_TAG}' '检测到狼人杀/子模块待处理报告，接力启动 AutoDebugTestReport.sh ...'
        if bash ./AutoDebugTestReport.sh; then
            bg_log '${SCRIPT_TAG}' '接力启动 AutoDebugTestReport.sh 成功'
        else
            bg_log '${SCRIPT_TAG}' '接力启动失败，请人工检查。'
        fi
    elif [[ -n \"\${PENDING_TEXAS}\" ]]; then
        bg_log '${SCRIPT_TAG}' '德扑有未接力报告(由 TexasPoker 脚本处理)，跳过。'
    else
        bg_log '${SCRIPT_TAG}' '无待处理报告，跳过自动修复流程。'
    fi

    bg_log '${SCRIPT_TAG}' '全流程结束'
    append_run_index script=${SCRIPT_TAG} agent=${SELECTED_AGENT} event=done exit=\"\${AGENT_EXIT}\"
")"
append_run_index "script=${SCRIPT_TAG}" "agent=${SELECTED_AGENT}" "event=launched" "pid=${BG_PID}"

echo "[${SCRIPT_TAG}] 已后台启动 Agent [${SELECTED_AGENT}] (PID: ${BG_PID})"
echo "[${SCRIPT_TAG}] 日志 : ${LOG_FILE}"
echo "[${SCRIPT_TAG}] 运行索引日志 : ${LOG_DIR}/auto_run_index.log"
echo "[${SCRIPT_TAG}] Agent 退出后会自动 git add + git commit（中文提交信息），"
echo "[${SCRIPT_TAG}] 并由 shell 层确定性接力启动 AutoDebugTestReport.sh（有待处理报告时）。"
echo "[${SCRIPT_TAG}] 调用者可继续执行其他操作，不会被阻塞。"
