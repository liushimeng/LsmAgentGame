#!/usr/bin/env bash
# AutoScreenshotWerewolf.sh
# ---------------------------------------------------------------
# 用途：
#   自动启动 Claude Code，读取当前目录或仓库根的 AutoScreenshotWerewolf.md
#   作为提示词执行狼人杀 13 人局精彩画面截图采集；Claude 退出后自动将
#   截图与报告文件以中文 git 提交，全程 bypass 权限。
#
# 特性：
#   - 工作目录与 Claude Code 启动目录均为 /usr/local/LsmAgentGame/LsmAgentGame
#   - 通过 nohup + setsid + & + disown 脱离调用者，**不阻塞**调用者进程
#   - 日志输出到 ./logs/auto_screenshot_<timestamp>.log
#   - AutoScreenshotWerewolf.md 优先取当前目录，其次仓库根；都不存在则立即报错退出
#   - Claude 退出后自动执行 `git add` + `git commit`（中文提交信息）
#   - 脚本本身赋予 755 权限
# ---------------------------------------------------------------

set -u

# ---------- 配置 ----------
PROJECT_DIR="/usr/local/LsmAgentGame/LsmAgentGame"
CLAUDE_BIN="${CLAUDE_BIN:-claude}"
LOG_DIR="${PROJECT_DIR}/logs"
PROMPT_FILE_NAME="AutoScreenshotWerewolf.md"
TS="$(date +%Y%m%d_%H%M%S)"
LOG_FILE="${LOG_DIR}/auto_screenshot_${TS}.log"

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
    echo "[AutoScreenshotWerewolf] 启动时间 : $(date '+%F %T')"
    echo "[AutoScreenshotWerewolf] 工作目录 : ${PROJECT_DIR}"
    echo "[AutoScreenshotWerewolf] 提示词文件: ${PROMPT_FILE}"
    echo "[AutoScreenshotWerewolf] 日志文件  : ${LOG_FILE}"
    echo "[AutoScreenshotWerewolf] claude 二进制: $(command -v ${CLAUDE_BIN} || echo 'NOT FOUND')"
    echo "============================================================"
} >> "${LOG_FILE}"

# ---------- 启动（后台脱离，不阻塞调用者）----------
nohup setsid bash -c "
    cd '${PROJECT_DIR}'

    # ------- 1. 运行自动化截图 -------
    ${CLAUDE_BIN} --dangerously-skip-permissions -p \"\$(cat '${PROMPT_FILE}')\"
    CLAUDE_EXIT=\$?
    echo '[AutoScreenshotWerewolf] claude 退出码 : '\${CLAUDE_EXIT}
    echo '[AutoScreenshotWerewolf] claude 结束时间 : '\"\$(date '+%F %T')\"

    # ------- 2. 用中文 git 自动提交截图 -------
    echo '[AutoScreenshotWerewolf] 开始 git 自动提交...'

    # 暂存截图与报告（避免误暂存业务代码的未预期改动）
    git add ProjectPic/werewolf-*.png \
            TestReport/ AutoScreenshotProgress/ \
            scripts/werewolf_screenshot.py \
            AutoScreenshotWerewolf.md \
            AutoScreenshotWerewolf.sh \
            README.md README.en.md README.ja.md \
        2>/dev/null || true

    # 检查是否有需要提交的变更
    if git diff --cached --quiet; then
        echo '[AutoScreenshotWerewolf] 暂存区无变更，跳过提交。'
    else
        COMMIT_TS=\"\$(date '+%Y%m%d_%H%M%S')\"
        # 使用中文提交信息（UTF-8）
        git commit -m \"截图: 狼人杀 13 人局实机截图 \${COMMIT_TS} 已完成\" \\
                 -m \"自动提交由 AutoScreenshotWerewolf.sh 生成\" \\
                 -m \"重点: 1 名人类玩家 + 12 Agent 混合 13 人局\" \\
            && echo '[AutoScreenshotWerewolf] git 提交成功: '\"\$(git rev-parse --short HEAD)\" \\
            || echo '[AutoScreenshotWerewolf] git 提交失败，请人工检查。'
    fi

    echo '[AutoScreenshotWerewolf] 全流程结束时间 : '\"\$(date '+%F %T')\"
" >> "${LOG_FILE}" 2>&1 </dev/null &

CLAUDE_PID=$!
disown "${CLAUDE_PID}" 2>/dev/null || true

echo "[AutoScreenshotWerewolf] 已后台启动 Claude Code，PID=${CLAUDE_PID}"
echo "[AutoScreenshotWerewolf] 日志 : ${LOG_FILE}"
echo "[AutoScreenshotWerewolf] Claude 退出后会自动 git add + git commit（中文提交信息）。"
echo "[AutoScreenshotWerewolf] 调用者可继续执行其他操作，不会被阻塞。"
