#!/usr/bin/env bash
# AutoTestAndSaveReport_Werewolf.sh
# ---------------------------------------------------------------
# 用途：
#   狼人杀 13 人局专用自动化测试入口。随机选择一个可用的编程 Agent CLI
#   （Claude Code / OpenCode / Hermes / OpenClaw），读取当前目录或仓库根
#   的 AutoTestAndSaveReport_Werewolf.md 作为提示词执行自动化测试；Agent
#   退出后自动将 TestReport 中以「自动化测试报告_」开头的狼人杀报告文件以
#   中文 git 提交（子模块 UseReport 在子仓库内单独提交），随后由 shell 层
#   确定性接力启动 AutoDebugTestReport.sh 进入自动修复流程，全程 bypass 权限。
#
# 特性（与 AutoTestAndSaveReport.sh 同，文件名特化）：
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
#   - **狼人杀专用**：仅扫描 `自动化测试报告_*.md` 前缀的狼人杀报告
#     （德州扑克报告由 AutoTestAndSaveReport_TexasPoker.sh 单独提交）
#   - 脚本本身赋予 755 权限
# ---------------------------------------------------------------

set -u

# ---------- 配置 ----------
PROJECT_DIR="/usr/local/LsmAgentGame/LsmAgentGame"
LOG_DIR="${PROJECT_DIR}/logs"
PROMPT_FILE_NAME="AutoTestAndSaveReport_Werewolf.md"
# 狼人杀报告文件名前缀（与德州扑克区分：后者用 `德州扑克自动化测试报告_*.md`）
WEREWOLF_REPORT_GLOB="自动化测试报告_*.md"
TS="$(date +%Y%m%d_%H%M%S)"
LOG_FILE="${LOG_DIR}/auto_test_werewolf_${TS}.log"

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

cd "${PROJECT_DIR}" || { echo "[ERROR] 无法进入 ${PROJECT_DIR}"; exit 1; }

# ---------- 随机选择 Agent ----------
pick_agent "AutoTestAndSaveReport_Werewolf"
AGENT_BIN_PATH="$(command -v "$(agent_binary_of "${SELECTED_AGENT}")" 2>/dev/null || echo 'NOT FOUND')"

{
    echo "============================================================"
    echo "[AutoTestAndSaveReport_Werewolf] 启动时间 : $(date '+%F %T')"
    echo "[AutoTestAndSaveReport_Werewolf] 工作目录 : ${PROJECT_DIR}"
    echo "[AutoTestAndSaveReport_Werewolf] 提示词文件: ${PROMPT_FILE}"
    echo "[AutoTestAndSaveReport_Werewolf] 日志文件  : ${LOG_FILE}"
    echo "[AutoTestAndSaveReport_Werewolf] 选中 Agent : ${SELECTED_AGENT}"
    echo "[AutoTestAndSaveReport_Werewolf] Agent 二进制: ${AGENT_BIN_PATH}"
    echo "============================================================"
} >> "${LOG_FILE}"

# ---------- 启动（后台脱离，不阻塞调用者）----------
# 整个生命周期在同一个 bash -c 子 shell 中顺序执行：
#   1. 选中的 Agent CLI 读取提示词文件执行测试（放开权限，全程自动化）
#   2. Agent 退出后自动 git add / git commit（中文提交信息）
#   3. shell 层确定性接力启动 AutoDebugTestReport.sh
# setsid  : 创建新会话，调用者退出也不会被 SIGHUP
# nohup   : 忽略 SIGHUP
# & + disown: 与当前 shell 解除关系
# stdout/stderr → 日志文件
nohup setsid bash -c "
    cd '${PROJECT_DIR}'
    source '${COMMON_LIB}'

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
    # 狼人杀专用：仅 add `自动化测试报告_*.md` 前缀；德扑报告由 TexasPoker 脚本单独 add。
    git add -- "TestReport/自动化测试报告_*.md" 2>/dev/null \\
        || echo '[AutoTestAndSaveReport_Werewolf] 警告: TestReport/狼人杀报告无可暂存内容(已忽略)'

    # 子模块 UseReport 需在子仓库内先提交，再回主仓库暂存 gitlink
    if [[ -d go-web-debug-tool/UseReport ]]; then
        git -C go-web-debug-tool add -- UseReport/ 2>/dev/null || true
        if ! git -C go-web-debug-tool diff --cached --quiet 2>/dev/null; then
            git -C go-web-debug-tool commit -m \"测试: 工具使用报告自动提交 ${TS}\" 2>/dev/null \\
                && echo '[AutoTestAndSaveReport] 子模块 UseReport 提交成功' \\
                || echo '[AutoTestAndSaveReport] 子模块提交失败(不阻塞主流程)'
        fi
        git add -- go-web-debug-tool 2>/dev/null || true
    fi

    # 检查是否有需要提交的变更
    if git diff --cached --quiet; then
        echo '[AutoTestAndSaveReport_Werewolf] 暂存区无变更，跳过提交。'
    else
        COMMIT_TS=\"\$(date '+%Y%m%d_%H%M%S')\"
        # 使用中文提交信息（UTF-8）
        git commit -m \"测试: 狼人杀自动化测试报告 \${COMMIT_TS} 已完成\" \\
                 -m \"自动提交由 AutoTestAndSaveReport_Werewolf.sh 生成\" \\
                 -m \"包含: TestReport/狼人杀报告 + go-web-debug-tool 子模块 gitlink(如有)\" \\
            && echo '[AutoTestAndSaveReport_Werewolf] git 提交成功: '\"\$(git rev-parse --short HEAD)\" \\
            || echo '[AutoTestAndSaveReport_Werewolf] git 提交失败，请人工检查。'
    fi

    # ------- 3. 确定性接力：shell 层自动启动自动修复流程 -------
    # 旧设计靠测试 Agent 自觉执行 AutoDebugTestReport.sh（prompt §8.3），实测会被
    # 跳过（logs/ 无任何 auto_debug_*.log），属「声明了却从不接线」反模式；
    # 改为脚本层接力，先预检待处理报告，避免空跑 Agent 会话。
    # 注：接力脚本内部会再次随机选择 Agent（每次脚本运行独立随机）。
    PENDING_MAIN=\$(find TestReport -maxdepth 1 -name '自动化测试报告_*.md' ! -name '*_无问题.md' 2>/dev/null | head -1)
    PENDING_TEXAS=\$(find TestReport -maxdepth 1 -name '德州扑克自动化测试报告_*.md' ! -name '*_无问题.md' 2>/dev/null | head -1)
    PENDING_SUB=\$(find go-web-debug-tool/UseReport -maxdepth 1 -name '测试工具使用报告_*.md' ! -name '*_无问题.md' 2>/dev/null | head -1)
    # 狼人杀接力：仅扫描本游戏前缀；德扑报告留给德扑接力脚本处理。
    if [[ -n \"\${PENDING_MAIN}\${PENDING_SUB}\" ]]; then
        echo '[AutoTestAndSaveReport_Werewolf] 检测到狼人杀/子模块待处理报告，接力启动 AutoDebugTestReport.sh ...'
        bash ./AutoDebugTestReport.sh || echo '[AutoTestAndSaveReport_Werewolf] 接力启动失败，请人工检查。'
    elif [[ -n \"\${PENDING_TEXAS}\" ]]; then
        echo '[AutoTestAndSaveReport_Werewolf] 德扑有未接力报告(由 TexasPoker 脚本处理)，跳过。'
    else
        echo '[AutoTestAndSaveReport_Werewolf] 无待处理报告，跳过自动修复流程。'
    fi

    echo '[AutoTestAndSaveReport_Werewolf] 全流程结束时间 : '\"\$(date '+%F %T')\"
" >> "${LOG_FILE}" 2>&1 </dev/null &

AGENT_PID=$!
disown "${AGENT_PID}" 2>/dev/null || true

echo "[AutoTestAndSaveReport_Werewolf] 已后台启动 Agent [${SELECTED_AGENT}]，PID=${AGENT_PID}"
echo "[AutoTestAndSaveReport_Werewolf] 日志 : ${LOG_FILE}"
echo "[AutoTestAndSaveReport_Werewolf] Agent 退出后会自动 git add + git commit（中文提交信息），"
echo "[AutoTestAndSaveReport_Werewolf] 并由 shell 层确定性接力启动 AutoDebugTestReport.sh（有待处理报告时）。"
echo "[AutoTestAndSaveReport_Werewolf] 调用者可继续执行其他操作，不会被阻塞。"
