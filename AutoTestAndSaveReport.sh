#!/usr/bin/env bash
# AutoTestAndSaveReport.sh
# ---------------------------------------------------------------
# 用途：
#   自动启动 Claude Code，读取当前目录或仓库根的 AutoTestAndSaveReport.md
#   作为提示词执行自动化测试；Claude 退出后自动将 TestReport 与
#   AutoTestProgress 中的报告文件以中文 git 提交，全程 bypass 权限。
#
# 特性：
#   - 工作目录与 Claude Code 启动目录均为 /usr/local/LsmWebGame
#   - 通过 nohup + setsid + & + disown 脱离调用者，**不阻塞**调用者进程
#   - 日志输出到 ./logs/auto_test_<timestamp>.log
#   - AutoTestAndSaveReport.md 优先取当前目录，其次仓库根；都不存在则立即报错退出
#   - Claude 退出后自动执行 `git add` + `git commit`（中文提交信息）
#   - 脚本本身赋予 755 权限
# ---------------------------------------------------------------

set -u

# ---------- 配置 ----------
PROJECT_DIR="/usr/local/LsmWebGame"
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

    # 暂存测试报告与进度文件（避免误暂存业务代码的未预期改动）
    git add TestReport/ AutoTestProgress/ go-web-debug-tool/UseReport/ 2>/dev/null || true

    # 检查是否有需要提交的变更
    if git diff --cached --quiet; then
        echo '[AutoTestAndSaveReport] 暂存区无变更，跳过提交。'
    else
        COMMIT_TS=\"\$(date '+%Y%m%d_%H%M%S')\"
        # 使用中文提交信息（UTF-8）
        git commit -m \"测试: 自动化测试报告 \${COMMIT_TS} 已完成\" \\
                 -m \"自动提交由 AutoTestAndSaveReport.sh 生成\" \\
                 -m \"包含目录: TestReport/ AutoTestProgress/ go-web-debug-tool/UseReport/\" \\
            && echo '[AutoTestAndSaveReport] git 提交成功: '\"\$(git rev-parse --short HEAD)\" \\
            || echo '[AutoTestAndSaveReport] git 提交失败，请人工检查。'
    fi

    echo '[AutoTestAndSaveReport] 全流程结束时间 : '\"\$(date '+%F %T')\"
" >> "${LOG_FILE}" 2>&1 </dev/null &

CLAUDE_PID=$!
disown "${CLAUDE_PID}" 2>/dev/null || true

echo "[AutoTestAndSaveReport] 已后台启动 Claude Code，PID=${CLAUDE_PID}"
echo "[AutoTestAndSaveReport] 日志 : ${LOG_FILE}"
echo "[AutoTestAndSaveReport] Claude 退出后会自动 git add + git commit（中文提交信息）。"
echo "[AutoTestAndSaveReport] 调用者可继续执行其他操作，不会被阻塞。"
