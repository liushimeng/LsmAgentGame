#!/usr/bin/env bash
# AutoTestAndSaveReport_TexasPoker.sh
# ---------------------------------------------------------------
# 用途：
#   德州扑克 2-6 人 Agent 专用自动化测试入口。随机选择一个可用的编程
#   Agent CLI（Claude Code / OpenCode / Hermes / OpenClaw），读取当前目录
#   或仓库根的 AutoTestAndSaveReport_TexasPoker.md 作为提示词执行自动化
#   测试；Agent 退出后自动将 TestReport 中以「德州扑克自动化测试报告_」
#   开头的德扑报告文件以中文 git 提交（子模块 UseReport 在子仓库内单独
#   提交），随后由 shell 层确定性接力启动 AutoDebugTestReport.sh 进入
#   自动修复流程，全程 bypass 权限。
#
# 特性（与 AutoTestAndSaveReport_Werewolf.sh 同形态，文件名特化）：
#   - 工作目录与 Agent 启动目录均为 /usr/local/LsmAgentGame/LsmAgentGame
#   - 多 Agent 随机选择逻辑在公共库 agent_cli_common.sh 中（可 source 复用）；
#     AGENT_CLI 环境变量可强制指定某个 Agent（claude|opencode|hermes|openclaw）
#   - 通过 nohup + setsid + & + disown 脱离调用者，**不阻塞**调用者进程
#   - 日志输出到 ./logs/auto_test_texas_<timestamp>.log
#   - AutoTestAndSaveReport_TexasPoker.md 优先取当前目录，其次仓库根；都不存在则立即报错退出
#   - Agent 退出后自动执行 `git add` + `git commit`（中文提交信息，逐路径容错）
#   - git 提交后由 shell **确定性接力**启动 AutoDebugTestReport.sh
#     （不依赖测试 Agent 自觉执行，避免「声明了却从不接线」断链；含待处理报告预检）
#     接力阶段会再次随机选择 Agent（每次脚本运行独立随机）
#   - **德扑专用**：仅扫描 `德州扑克自动化测试报告_*.md` 前缀的德扑报告
#     （狼人杀报告由 AutoTestAndSaveReport_Werewolf.sh 单独提交）
#   - 脚本本身赋予 755 权限
# ---------------------------------------------------------------

set -u

# ---------- 配置 ----------
PROJECT_DIR="/usr/local/LsmAgentGame/LsmAgentGame"
LOG_DIR="${PROJECT_DIR}/logs"
PROMPT_FILE_NAME="AutoTestAndSaveReport_TexasPoker.md"
# 德扑报告文件名前缀（与狼人杀区分：后者用 `自动化测试报告_*.md`）
TEXAS_REPORT_GLOB="德州扑克自动化测试报告_*.md"
TEXAS_USAGE_GLOB="德州扑克测试工具使用报告_*.md"
TS="$(date +%Y%m%d_%H%M%S)"
LOG_FILE="${LOG_DIR}/auto_test_texas_${TS}.log"

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
pick_agent "AutoTestAndSaveReport_TexasPoker"
AGENT_BIN_PATH="$(command -v "$(agent_binary_of "${SELECTED_AGENT}")" 2>/dev/null || echo 'NOT FOUND')"

{
    echo "============================================================"
    echo "[AutoTestAndSaveReport_TexasPoker] 启动时间 : $(date '+%F %T')"
    echo "[AutoTestAndSaveReport_TexasPoker] 工作目录 : ${PROJECT_DIR}"
    echo "[AutoTestAndSaveReport_TexasPoker] 提示词文件: ${PROMPT_FILE}"
    echo "[AutoTestAndSaveReport_TexasPoker] 日志文件  : ${LOG_FILE}"
    echo "[AutoTestAndSaveReport_TexasPoker] 选中 Agent : ${SELECTED_AGENT}"
    echo "[AutoTestAndSaveReport_TexasPoker] Agent 二进制: ${AGENT_BIN_PATH}"
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
    echo '[AutoTestAndSaveReport_TexasPoker] ${SELECTED_AGENT} 退出码 : '\${AGENT_EXIT}
    echo '[AutoTestAndSaveReport_TexasPoker] ${SELECTED_AGENT} 结束时间 : '\"\$(date '+%F %T')\"

    # ------- 2. 用中文 git 自动提交德扑测试报告 -------
    echo '[AutoTestAndSaveReport_TexasPoker] 开始 git 自动提交...'

    # 逐路径暂存：任一目录不存在 / 被 .gitignore 忽略时不阻塞其它目录。
    # 教训(20260812)：旧写法把多个目录塞在同一条 git add 里 —— AutoTestProgress/
    # 被 .gitignore 整体忽略 + 子模块内路径在父仓库 add 直接 fatal(exit=128)，
    # 任一失败都让整条 add 原子性落空，报告从未被暂存，日志却显示「暂存区无变更」。
    # 注：AutoTestProgress/ 按 .gitignore 策略为本地进度文件，不入库。
    # 德扑专用：仅 add `德州扑克自动化测试报告_*.md` 与 `德州扑克协议抓包分析报告_*.md` 前缀；
    # 狼人杀报告由 Werewolf 脚本单独 add，互不影响。
    git add -- 'TestReport/德州扑克*.md' 2>/dev/null \\
        || echo '[AutoTestAndSaveReport_TexasPoker] 警告: TestReport/德扑报告无可暂存内容(已忽略)'

    # 子模块 UseReport 需在子仓库内先提交，再回主仓库暂存 gitlink
    # 教训(20260819)：用 `find -name` 动态发现德扑工具报告文件名（避免硬编码时间戳）
    if [[ -d go-web-debug-tool/UseReport ]]; then
        TEXAS_USAGE_FILES=\$(find go-web-debug-tool/UseReport -maxdepth 1 -name '德州扑克测试工具使用报告_*.md' ! -name '*_无问题.md' 2>/dev/null)
        if [[ -n \"\${TEXAS_USAGE_FILES}\" ]]; then
            git -C go-web-debug-tool add -- UseReport/ 2>/dev/null || true
            if ! git -C go-web-debug-tool diff --cached --quiet 2>/dev/null; then
                git -C go-web-debug-tool commit -m \"测试: 德州扑克工具使用报告自动提交 ${TS}\" 2>/dev/null \\
                    && echo '[AutoTestAndSaveReport_TexasPoker] 子模块 UseReport 提交成功' \\
                    || echo '[AutoTestAndSaveReport_TexasPoker] 子模块提交失败(不阻塞主流程)'
            fi
            git add -- go-web-debug-tool 2>/dev/null || true
        fi
    fi

    # 检查是否有需要提交的变更
    if git diff --cached --quiet; then
        echo '[AutoTestAndSaveReport_TexasPoker] 暂存区无变更，跳过提交。'
    else
        COMMIT_TS=\"\$(date '+%Y%m%d_%H%M%S')\"
        # 使用中文提交信息（UTF-8）
        git commit -m \"测试: 德州扑克自动化测试报告 \${COMMIT_TS} 已完成\" \\
                 -m \"自动提交由 AutoTestAndSaveReport_TexasPoker.sh 生成\" \\
                 -m \"包含: TestReport/德扑报告 + go-web-debug-tool 子模块 gitlink(如有)\" \\
            && echo '[AutoTestAndSaveReport_TexasPoker] git 提交成功: '\"\$(git rev-parse --short HEAD)\" \\
            || echo '[AutoTestAndSaveReport_TexasPoker] git 提交失败，请人工检查。'
    fi

    # ------- 3. 确定性接力：shell 层自动启动自动修复流程 -------
    # 旧设计靠测试 Agent 自觉执行 AutoDebugTestReport.sh（prompt §8.3），实测会被
    # 跳过（logs/ 无任何 auto_debug_*.log），属「声明了却从不接线」反模式；
    # 改为脚本层接力，先预检待处理报告，避免空跑 Agent 会话。
    # 注：接力脚本内部会再次随机选择 Agent（每次脚本运行独立随机）。
    # 德扑接力：仅扫描德扑前缀；狼人杀 + 子模块 UseReport 由 Werewolf 脚本接力。
    PENDING_TEXAS=\$(find TestReport -maxdepth 1 -name '德州扑克自动化测试报告_*.md' ! -name '*_无问题.md' 2>/dev/null | head -1)
    PENDING_WEREWOLF=\$(find TestReport -maxdepth 1 -name '自动化测试报告_*.md' ! -name '*_无问题.md' 2>/dev/null | head -1)
    if [[ -n \"\${PENDING_TEXAS}\${PENDING_WEREWOLF}\" ]]; then
        echo '[AutoTestAndSaveReport_TexasPoker] 检测到待处理报告(德扑+狼人杀 共存)，接力启动 AutoDebugTestReport.sh ...'
        bash ./AutoDebugTestReport.sh || echo '[AutoTestAndSaveReport_TexasPoker] 接力启动失败，请人工检查。'
    elif [[ -n \"\${PENDING_WEREWOLF}\" ]]; then
        # 当前德扑脚本发现狼人杀报告，跳过接力（留给 Werewolf 脚本接力）
        echo '[AutoTestAndSaveReport_TexasPoker] 狼人杀有未接力报告(由 Werewolf 脚本处理)，跳过。'
    else
        echo '[AutoTestAndSaveReport_TexasPoker] 无待处理报告，跳过自动修复流程。'
    fi

    echo '[AutoTestAndSaveReport_TexasPoker] 全流程结束时间 : '\"\$(date '+%F %T')\"
" >> "${LOG_FILE}" 2>&1 </dev/null &

AGENT_PID=$!
disown "${AGENT_PID}" 2>/dev/null || true

echo "[AutoTestAndSaveReport_TexasPoker] 已后台启动 Agent [${SELECTED_AGENT}]，PID=${AGENT_PID}"
echo "[AutoTestAndSaveReport_TexasPoker] 日志 : ${LOG_FILE}"
echo "[AutoTestAndSaveReport_TexasPoker] Agent 退出后会自动 git add + git commit（中文提交信息），"
echo "[AutoTestAndSaveReport_TexasPoker] 并由 shell 层确定性接力启动 AutoDebugTestReport.sh（有待处理报告时）。"
echo "[AutoTestAndSaveReport_TexasPoker] 调用者可继续执行其他操作，不会被阻塞。"
