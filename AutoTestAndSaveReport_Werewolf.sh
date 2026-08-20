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
#   - 日志输出到 ./logs/auto_test_werewolf_<timestamp>.log
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
# ---------------------------------------------------------------

set -u

# ---------- 配置 ----------
PROJECT_DIR="/usr/local/LsmAgentGame/LsmAgentGame"
LOG_DIR="${PROJECT_DIR}/logs"
PROMPT_FILE_NAME="AutoTestAndSaveReport_Werewolf.md"
# 狼人杀主报告 glob(新前缀 + 兼容旧)— 来自 auto_run_common.sh::GAME_GLOBS
TS="$(date +%Y%m%d_%H%M%S)"
LOG_FILE="${LOG_DIR}/auto_test_werewolf_${TS}.log"

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

# ---------- 定位提示词文件 ----------
PROMPT_FILE="$(locate_prompt_file "${PROMPT_FILE_NAME}")" || exit 1

cd "${PROJECT_DIR}" || { echo "[ERROR] 无法进入 ${PROJECT_DIR}"; exit 1; }

# ---------- 随机选择 Agent ----------
pick_agent "AutoTestAndSaveReport_Werewolf"

# ---------- 启动日志头 ----------
print_section_header "AutoTestAndSaveReport_Werewolf" "${PROMPT_FILE}" "${LOG_FILE}" "${PROJECT_DIR}" "${SELECTED_AGENT}"

# ---------- 启动（后台脱离，不阻塞调用者）----------
# 整个生命周期在同一个 bash -c 子 shell 中顺序执行：
#   1. 选中的 Agent CLI 读取提示词文件执行测试（放开权限，全程自动化）
#   2. Agent 退出后自动 git add / git commit（中文提交信息）
#   3. shell 层确定性接力启动 AutoDebugTestReport.sh
start_agent_in_background "${LOG_FILE}" "
    cd '${PROJECT_DIR}'
    source '${AGENT_LIB}'
    source '${AUTO_LIB}'

    # ------- 1. 运行自动化测试 -------
    run_agent_with_prompt '${SELECTED_AGENT}' '${PROMPT_FILE}' '${PROJECT_DIR}'
    AGENT_EXIT=\$?
    echo '[AutoTestAndSaveReport_Werewolf] ${SELECTED_AGENT} 退出码 : '\${AGENT_EXIT}
    echo '[AutoTestAndSaveReport_Werewolf] ${SELECTED_AGENT} 结束时间 : '\"\$(date '+%F %T')\"

    # ------- 2. 用中文 git 自动提交测试报告 -------
    echo '[AutoTestAndSaveReport_Werewolf] 开始 git 自动提交...'

    # 逐路径暂存：任一目录不存在 / 被 .gitignore 忽略时不阻塞其它目录。
    # 教训(20260812)：旧写法把三个目录塞在同一条 git add 里 —— AutoTestProgress/
    # 被 .gitignore 整体忽略 + 子模块内路径在父仓库 add 直接 fatal(exit=128)，
    # 任一失败都让整条 add 原子性落空，报告从未被暂存，日志却显示「暂存区无变更」。
    # 注：AutoTestProgress/ 按 .gitignore 策略为本地进度文件，不入库。
    # 狼人杀专用：仅 add `狼人杀自动化测试报告_*.md`(新) + 兼容旧 `自动化测试报告_*.md`；
    # 德扑报告由 TexasPoker 脚本单独 add，互不影响。
    # 主 glob 与 legacy glob 用 find -o 串联，自动覆盖新旧文件。
    WEREWOLF_MAIN_GLOB=\"\$(enqueue_game_glob werewolf main main_legacy)\"
    git_add_safe \"TestReport/\${WEREWOLF_MAIN_GLOB}\" || echo '[AutoTestAndSaveReport_Werewolf] 警告: TestReport/狼人杀报告无可暂存内容(已忽略)'

    # 子模块 UseReport 需在子仓库内先提交，再回主仓库暂存 gitlink
    # 教训(20260819)：用 `find -name` 动态发现德扑工具报告文件名（避免硬编码时间戳）
    WEREWOLF_USAGE_GLOB=\"\$(enqueue_game_glob werewolf usage usage_legacy)\"
    if [[ -d go-web-debug-tool/UseReport ]]; then
        WEREWOLF_USAGE_FILES=\$(find go-web-debug-tool/UseReport -maxdepth 1 \( -name \"\${WEREWOLF_USAGE_GLOB}\" \) ! -name '*_无问题.md' 2>/dev/null)
        if [[ -n \"\${WEREWOLF_USAGE_FILES}\" ]]; then
            git -C go-web-debug-tool add -- UseReport/ 2>/dev/null || true
            if ! git -C go-web-debug-tool diff --cached --quiet 2>/dev/null; then
                git -C go-web-debug-tool commit -m \"测试: 狼人杀工具使用报告自动提交 ${TS}\" 2>/dev/null \\
                    && echo '[AutoTestAndSaveReport_Werewolf] 子模块 UseReport 提交成功' \\
                    || echo '[AutoTestAndSaveReport_Werewolf] 子模块提交失败(不阻塞主流程)'
            fi
            git_add_safe 'go-web-debug-tool'
        fi
    fi

    # 检查是否有需要提交的变更
    if git diff --cached --quiet; then
        echo '[AutoTestAndSaveReport_Werewolf] 暂存区无变更，跳过提交。'
    else
        COMMIT_TS=\"\$(date '+%Y%m%d_%H%M%S')\"
        git_commit_chinese '测试' 'werewolf' \"\${COMMIT_TS}\" 'TestReport/狼人杀报告 + go-web-debug-tool 子模块 gitlink(如有)' \\
            && echo '[AutoTestAndSaveReport_Werewolf] git 提交成功: '\"\$(git rev-parse --short HEAD)\" \\
            || echo '[AutoTestAndSaveReport_Werewolf] git 提交失败，请人工检查。'
    fi

    # ------- 3. 确定性接力：shell 层自动启动自动修复流程 -------
    # 旧设计靠测试 Agent 自觉执行 AutoDebugTestReport.sh（prompt §8.3），实测会被
    # 跳过（logs/ 无任何 auto_debug_*.log），属「声明了却从不接线」反模式；
    # 改为脚本层接力，先预检待处理报告，避免空跑 Agent 会话。
    # 注：接力脚本内部会再次随机选择 Agent（每次脚本运行独立随机）。
    PENDING_WEREWOLF=\$(scan_game_report TestReport werewolf main)
    PENDING_SUB=\$(find go-web-debug-tool/UseReport -maxdepth 1 \( -name \"\$(enqueue_game_glob werewolf usage usage_legacy)\" \) ! -name '*_无问题.md' 2>/dev/null | head -1)
    PENDING_TEXAS=\$(scan_game_report TestReport texasholdem main)
    # 狼人杀接力：仅扫描本游戏 glob + 子模块；德扑报告留给德扑接力脚本处理。
    if [[ -n \"\${PENDING_WEREWOLF}\${PENDING_SUB}\" ]]; then
        echo '[AutoTestAndSaveReport_Werewolf] 检测到狼人杀/子模块待处理报告，接力启动 AutoDebugTestReport.sh ...'
        bash ./AutoDebugTestReport.sh || echo '[AutoTestAndSaveReport_Werewolf] 接力启动失败，请人工检查。'
    elif [[ -n \"\${PENDING_TEXAS}\" ]]; then
        echo '[AutoTestAndSaveReport_Werewolf] 德扑有未接力报告(由 TexasPoker 脚本处理)，跳过。'
    else
        echo '[AutoTestAndSaveReport_Werewolf] 无待处理报告，跳过自动修复流程。'
    fi

    echo '[AutoTestAndSaveReport_Werewolf] 全流程结束时间 : '\"\$(date '+%F %T')\"
"

echo "[AutoTestAndSaveReport_Werewolf] 已后台启动 Agent [${SELECTED_AGENT}]"
echo "[AutoTestAndSaveReport_Werewolf] 日志 : ${LOG_FILE}"
echo "[AutoTestAndSaveReport_Werewolf] Agent 退出后会自动 git add + git commit（中文提交信息），"
echo "[AutoTestAndSaveReport_Werewolf] 并由 shell 层确定性接力启动 AutoDebugTestReport.sh（有待处理报告时）。"
echo "[AutoTestAndSaveReport_Werewolf] 调用者可继续执行其他操作，不会被阻塞。"