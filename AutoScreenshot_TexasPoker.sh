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
# 特性（与 AutoScreenshot_Werewolf.sh 同形态，文件名特化）：
#   - 工作目录与 Agent 启动目录均为 /usr/local/LsmAgentGame/LsmAgentGame
#   - 多 Agent 随机选择逻辑在公共库 agent_cli_common.sh 中（可 source 复用）；
#     AGENT_CLI 环境变量可强制指定某个 Agent（claude|opencode|hermes|openclaw）
#   - 通过 nohup + setsid + & + disown 脱离调用者，**不阻塞**调用者进程
#   - 日志输出到 ./logs/auto_screenshot_texas_<timestamp>.log
#   - AutoScreenshot_TexasPoker.md 优先取当前目录，其次仓库根；都不存在则立即报错退出
#   - Agent 退出后自动执行 `git add` + `git commit`（中文提交信息）
#   - 脚本本身赋予 755 权限
# ---------------------------------------------------------------

set -u

# ---------- 配置 ----------
PROJECT_DIR="/usr/local/LsmAgentGame/LsmAgentGame"
LOG_DIR="${PROJECT_DIR}/logs"
PROMPT_FILE_NAME="AutoScreenshot_TexasPoker.md"
TS="$(date +%Y%m%d_%H%M%S)"
LOG_FILE="${LOG_DIR}/auto_screenshot_texas_${TS}.log"

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
pick_agent "AutoScreenshot_TexasPoker"
AGENT_BIN_PATH="$(command -v "$(agent_binary_of "${SELECTED_AGENT}")" 2>/dev/null || echo 'NOT FOUND')"

{
    echo "============================================================"
    echo "[AutoScreenshot_TexasPoker] 启动时间 : $(date '+%F %T')"
    echo "[AutoScreenshot_TexasPoker] 工作目录 : ${PROJECT_DIR}"
    echo "[AutoScreenshot_TexasPoker] 提示词文件: ${PROMPT_FILE}"
    echo "[AutoScreenshot_TexasPoker] 日志文件  : ${LOG_FILE}"
    echo "[AutoScreenshot_TexasPoker] 选中 Agent : ${SELECTED_AGENT}"
    echo "[AutoScreenshot_TexasPoker] Agent 二进制: ${AGENT_BIN_PATH}"
    echo "============================================================"
} >> "${LOG_FILE}"

# ---------- 启动（后台脱离，不阻塞调用者）----------
nohup setsid bash -c "
    cd '${PROJECT_DIR}'
    source '${COMMON_LIB}'

    # ------- 1. 运行自动化截图 -------
    run_agent_with_prompt '${SELECTED_AGENT}' '${PROMPT_FILE}' '${PROJECT_DIR}'
    AGENT_EXIT=\$?
    echo '[AutoScreenshot_TexasPoker] ${SELECTED_AGENT} 退出码 : '\${AGENT_EXIT}
    echo '[AutoScreenshot_TexasPoker] ${SELECTED_AGENT} 结束时间 : '\"\$(date '+%F %T')\"

    # ------- 2. 用中文 git 自动提交截图 -------
    echo '[AutoScreenshot_TexasPoker] 开始 git 自动提交...'

    # 暂存截图与报告（避免误暂存业务代码的未预期改动）
    # 德扑专用：仅 add texaspoker-*.png 与德扑相关文件；狼人杀截图由 Werewolf 脚本单独处理。
    git add -- ProjectPic/texaspoker-*.png \
            'TestReport/德州扑克*.md' \
            'AutoScreenshotProgress/德州扑克*.md' \
            scripts/screenshot/texasholdem_highlight_capture.py \
            AutoScreenshot_TexasPoker.md \
            AutoScreenshot_TexasPoker.sh \
            README.md README.en.md README.ja.md \
        2>/dev/null || true

    # 检查是否有需要提交的变更
    if git diff --cached --quiet; then
        echo '[AutoScreenshot_TexasPoker] 暂存区无变更，跳过提交。'
    else
        COMMIT_TS=\"\$(date '+%Y%m%d_%H%M%S')\"
        # 使用中文提交信息（UTF-8）
        git commit -m \"截图: 德州扑克 2-6 人局实机截图 \${COMMIT_TS} 已完成\" \
                 -m \"自动提交由 AutoScreenshot_TexasPoker.sh 生成\" \
                 -m \"重点: 1 名人类玩家 + N Agent 混合对局\" \
            && echo '[AutoScreenshot_TexasPoker] git 提交成功: '\"\$(git rev-parse --short HEAD)\" \
            || echo '[AutoScreenshot_TexasPoker] git 提交失败，请人工检查。'
    fi

    echo '[AutoScreenshot_TexasPoker] 全流程结束时间 : '\"\$(date '+%F %T')\"
" >> "${LOG_FILE}" 2>&1 </dev/null &

AGENT_PID=$!
disown "${AGENT_PID}" 2>/dev/null || true

echo "[AutoScreenshot_TexasPoker] 已后台启动 Agent [${SELECTED_AGENT}]，PID=${AGENT_PID}"
echo "[AutoScreenshot_TexasPoker] 日志 : ${LOG_FILE}"
echo "[AutoScreenshot_TexasPoker] Agent 退出后会自动 git add + git commit（中文提交信息）。"
echo "[AutoScreenshot_TexasPoker] 调用者可继续执行其他操作，不会被阻塞。"
