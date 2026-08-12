#!/usr/bin/env bash
# AutoTestAndSaveReport.sh
# ---------------------------------------------------------------
# 用途：
#   自动启动 Claude Code，读取当前目录或仓库根的 AutoTestAndSaveReport.md
#   作为提示词执行自动化测试；Claude 退出后自动将 TestReport 中的报告文件
#   以中文 git 提交（子模块 UseReport 在子仓库内单独提交），随后由 shell 层
#   确定性接力启动 AutoDebugTestReport.sh 进入自动修复流程，全程 bypass 权限。
#
# 特性：
#   - 工作目录与 Claude Code 启动目录均为 /usr/local/LsmAgentGame/LsmAgentGame
#   - 通过 nohup + setsid + & + disown 脱离调用者，**不阻塞**调用者进程
#   - 日志输出到 ./logs/auto_test_<timestamp>.log
#   - AutoTestAndSaveReport.md 优先取当前目录，其次仓库根；都不存在则立即报错退出
#   - Claude 退出后自动执行 `git add` + `git commit`（中文提交信息，逐路径容错）
#   - git 提交后由 shell **确定性接力**启动 AutoDebugTestReport.sh
#     （不依赖测试 Agent 自觉执行，避免「声明了却从不接线」断链；含待处理报告预检）
#   - 脚本本身赋予 755 权限
# ---------------------------------------------------------------

set -u

# ---------- 配置 ----------
PROJECT_DIR="/usr/local/LsmAgentGame/LsmAgentGame"
CLAUDE_BIN="${CLAUDE_BIN:-claude}"
LOG_DIR="${PROJECT_DIR}/logs"
PROMPT_FILE_NAME="AutoTestAndSaveReport.md"
TS="$(date +%Y%m%d_%H%M%S)"
LOG_FILE="${LOG_DIR}/auto_test_${TS}.log"

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
PROMPT_ARG="$(cat "${PROMPT_FILE}")"

cd "${PROJECT_DIR}" || { echo "[ERROR] 无法进入 ${PROJECT_DIR}"; exit 1; }

{
    echo "============================================================"
    echo "[AutoTestAndSaveReport] 启动时间 : $(date '+%F %T')"
    echo "[AutoTestAndSaveReport] 工作目录 : ${PROJECT_DIR}"
    echo "[AutoTestAndSaveReport] 提示词文件: ${PROMPT_FILE}"
    echo "[AutoTestAndSaveReport] 日志文件  : ${LOG_FILE}"
    echo "[AutoTestAndSaveReport] claude 二进制: $(command -v ${CLAUDE_BIN} || echo 'NOT FOUND')"
    echo "============================================================"
} >> "${LOG_FILE}"

# ---------- 启动（后台脱离，不阻塞调用者）----------
# 整个生命周期在同一个 bash -c 子 shell 中顺序执行：
#   1. Claude Code 读取提示词文件执行测试
#   2. Claude 退出后自动 git add / git commit（中文提交信息）
# setsid  : 创建新会话，调用者退出也不会被 SIGHUP
# nohup   : 忽略 SIGHUP
# & + disown: 与当前 shell 解除关系
# stdout/stderr → 日志文件
nohup setsid bash -c "
    cd '${PROJECT_DIR}'

    # ------- 1. 运行自动化测试 -------
    ${CLAUDE_BIN} --dangerously-skip-permissions -p \"\$(cat '${PROMPT_FILE}')\"
    CLAUDE_EXIT=\$?
    echo '[AutoTestAndSaveReport] claude 退出码 : '\${CLAUDE_EXIT}
    echo '[AutoTestAndSaveReport] claude 结束时间 : '\"\$(date '+%F %T')\"

    # ------- 2. 用中文 git 自动提交测试报告 -------
    echo '[AutoTestAndSaveReport] 开始 git 自动提交...'

    # 逐路径暂存：任一目录不存在 / 被 .gitignore 忽略时不阻塞其它目录。
    # 教训(20260812)：旧写法把三个目录塞在同一条 git add 里 —— AutoTestProgress/
    # 被 .gitignore 整体忽略 + 子模块内路径在父仓库 add 直接 fatal(exit=128)，
    # 任一失败都让整条 add 原子性落空，报告从未被暂存，日志却显示「暂存区无变更」。
    # 注：AutoTestProgress/ 按 .gitignore 策略为本地进度文件，不入库。
    git add -- TestReport/ 2>/dev/null \\
        || echo '[AutoTestAndSaveReport] 警告: TestReport/ 无可暂存内容或暂存失败(已忽略)'

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
        echo '[AutoTestAndSaveReport] 暂存区无变更，跳过提交。'
    else
        COMMIT_TS=\"\$(date '+%Y%m%d_%H%M%S')\"
        # 使用中文提交信息（UTF-8）
        git commit -m \"测试: 自动化测试报告 \${COMMIT_TS} 已完成\" \\
                 -m \"自动提交由 AutoTestAndSaveReport.sh 生成\" \\
                 -m \"包含: TestReport/ 及 go-web-debug-tool 子模块 gitlink(如有)\" \\
            && echo '[AutoTestAndSaveReport] git 提交成功: '\"\$(git rev-parse --short HEAD)\" \\
            || echo '[AutoTestAndSaveReport] git 提交失败，请人工检查。'
    fi

    # ------- 3. 确定性接力：shell 层自动启动自动修复流程 -------
    # 旧设计靠测试 Agent 自觉执行 AutoDebugTestReport.sh（prompt §8.3），实测会被
    # 跳过（logs/ 无任何 auto_debug_*.log），属「声明了却从不接线」反模式；
    # 改为脚本层接力，先预检待处理报告，避免空跑 Claude 会话。
    PENDING_MAIN=\$(find TestReport -maxdepth 1 -name '自动化测试报告_*.md' ! -name '*_无问题.md' 2>/dev/null | head -1)
    PENDING_SUB=\$(find go-web-debug-tool/UseReport -maxdepth 1 -name '测试工具使用报告_*.md' ! -name '*_无问题.md' 2>/dev/null | head -1)
    if [[ -n \"\${PENDING_MAIN}\${PENDING_SUB}\" ]]; then
        echo '[AutoTestAndSaveReport] 检测到待处理报告，接力启动 AutoDebugTestReport.sh ...'
        bash ./AutoDebugTestReport.sh || echo '[AutoTestAndSaveReport] 接力启动失败，请人工检查。'
    else
        echo '[AutoTestAndSaveReport] 无待处理报告，跳过自动修复流程。'
    fi

    echo '[AutoTestAndSaveReport] 全流程结束时间 : '\"\$(date '+%F %T')\"
" >> "${LOG_FILE}" 2>&1 </dev/null &

CLAUDE_PID=$!
disown "${CLAUDE_PID}" 2>/dev/null || true

echo "[AutoTestAndSaveReport] 已后台启动 Claude Code，PID=${CLAUDE_PID}"
echo "[AutoTestAndSaveReport] 日志 : ${LOG_FILE}"
echo "[AutoTestAndSaveReport] Claude 退出后会自动 git add + git commit（中文提交信息），"
echo "[AutoTestAndSaveReport] 并由 shell 层确定性接力启动 AutoDebugTestReport.sh（有待处理报告时）。"
echo "[AutoTestAndSaveReport] 调用者可继续执行其他操作，不会被阻塞。"
