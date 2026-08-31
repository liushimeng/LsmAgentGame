#!/usr/bin/env bash
# auto_run_common.sh
# ---------------------------------------------------------------
# 自动化脚本公共库（被 AutoDebugTestReport.sh /
# AutoTestAndSaveReport_{Werewolf,TexasPoker}.sh /
# AutoScreenshot_{Werewolf,TexasPoker}.sh source 引用）。
#
# 职责（与 agent_cli_common.sh 严格分工）：
#   - agent_cli_common.sh: Agent CLI 选择 + 调用（pick_agent / run_agent_with_prompt）
#   - auto_run_common.sh:   多游戏 glob 单一事实源 + 脚本样板（日志头 / prompt 定位 /
#                           git add/commit 封装 / 后台启动封装 / 报告文件前缀）
#                           + 统一日志工具（§20260821-01）
#
# 关键规约：
#   - 多游戏 glob 在 GAME_GLOBS 关联数组中**单一声明**，新游戏一行新增；
#     任何 .sh / .md 引用 glob 都通过 enqueue_game_glob 派生，禁止就地硬编码。
#   - 旧无游戏名前缀的狼人杀产物（兼容旧）：保留 `_legacy` 后缀的 glob 作为兜底，
#     历史已落盘文件不重命名（避免污染 git 历史）。
#   - 时间戳格式：`YYYYMMDD_HHMMSS`，精度到秒。
#   - 日志规约（§20260821-01）：
#     * 单次运行日志 `logs/auto_<类型>_<Agent名>_<时间戳>.log` —— 文件名含 Agent 程序名，
#       记录启动日志头 + Agent 全程 stdout/stderr + 各阶段时间戳。
#     * 运行索引日志 `logs/auto_run_index.log` —— 每次运行追加 key=value 单行，
#       记录脚本名 / Agent 程序名 / 事件 / 时间 / PID / 退出码，便于事后 grep 审计。
#     * 旧日志保留期缺省 30 天（LOG_RETAIN_DAYS 可覆盖），由 cleanup_old_logs 清理。
#
# 用法（在调用脚本中）：
#   source "${PROJECT_DIR}/auto_run_common.sh"
#   GAME_NAME="werewolf"                     # 或 texasholdem / xiangqi / doudizhu / junqi
#   print_section_header "${TAG}" "${PROMPT_FILE}" "${LOG_FILE}" "${PROJECT_DIR}" "${SELECTED_AGENT}"
#   PROMPT_FILE="$(locate_prompt_file "${PROMPT_FILE_NAME}")"
#   enqueue_game_glob "${GAME_NAME}" "main"   # 输出 glob 给 find 用
#   git_add_safe "TestReport/狼人杀*.md"      # 单路径 add 容错封装
#   git_commit_chinese "测试" "${GAME_NAME}" "${TS}" "${SCRIPT_NAME}"   # 中文 commit 封装
#   start_agent_in_background "<log_file>" "<bash_script>"   # nohup setsid & disown 封装
#   bg_log "${TAG}" "message"                 # 后台段带时间戳日志（stdout 已重定向到日志文件）
#   append_run_index script=X agent=Y event=Z # 运行索引日志追加一行
#   cleanup_old_logs                          # 清理超过保留期的旧日志
# ---------------------------------------------------------------

# ---------- 多游戏 Glob 单一事实源 ----------
# 键命名：<game>_<kind>
#   kind: main(主测试报告) / progress(测试进度) / screenshot(截图报告) /
#         screenshot_progress(截图进度) / usage(子模块工具报告) / protocol(协议抓包分析)
#   _legacy 后缀：兼容旧文件(无游戏名前缀),新文件用主 glob。
declare -A GAME_GLOBS=(
    # 狼人杀(2026-08-20 §20260820-01 起主报告也带"狼人杀"前缀,旧文件名兼容)
    [werewolf_main]="狼人杀自动化测试报告_*.md"
    [werewolf_main_legacy]="自动化测试报告_*.md"
    [werewolf_progress]="狼人杀自动化测试进度_*.md"
    [werewolf_screenshot]="狼人杀截图报告_*.md"
    [werewolf_screenshot_legacy]="截图报告_*.md"
    [werewolf_screenshot_progress]="狼人杀截图进度_*.md"
    [werewolf_usage]="狼人杀测试工具使用报告_*.md"
    [werewolf_usage_legacy]="测试工具使用报告_*.md"
    [werewolf_protocol]="狼人杀协议抓包分析报告_*.md"

    # 德州扑克(2026-08-19 起即带前缀,无 legacy)
    [texasholdem_main]="德州扑克自动化测试报告_*.md"
    [texasholdem_progress]="德州扑克自动化测试进度_*.md"
    [texasholdem_screenshot]="德州扑克截图报告_*.md"
    [texasholdem_screenshot_progress]="德州扑克截图进度_*.md"
    [texasholdem_usage]="德州扑克测试工具使用报告_*.md"
    [texasholdem_protocol]="德州扑克协议抓包分析报告_*.md"

    # 象棋(预留)
    [xiangqi_main]="象棋自动化测试报告_*.md"
    [xiangqi_progress]="象棋自动化测试进度_*.md"
    [xiangqi_screenshot]="象棋截图报告_*.md"
    [xiangqi_screenshot_progress]="象棋截图进度_*.md"
    [xiangqi_usage]="象棋测试工具使用报告_*.md"
    [xiangqi_protocol]="象棋协议抓包分析报告_*.md"

    # 斗地主(预留)
    [doudizhu_main]="斗地主自动化测试报告_*.md"
    [doudizhu_progress]="斗地主自动化测试进度_*.md"
    [doudizhu_screenshot]="斗地主截图报告_*.md"
    [doudizhu_screenshot_progress]="斗地主截图进度_*.md"
    [doudizhu_usage]="斗地主测试工具使用报告_*.md"
    [doudizhu_protocol]="斗地主协议抓包分析报告_*.md"

    # 军棋(预留)
    [junqi_main]="军棋自动化测试报告_*.md"
    [junqi_progress]="军棋自动化测试进度_*.md"
    [junqi_screenshot]="军棋截图报告_*.md"
    [junqi_screenshot_progress]="军棋截图进度_*.md"
    [junqi_usage]="军棋测试工具使用报告_*.md"
    [junqi_protocol]="军棋协议抓包分析报告_*.md"

    # 辩论比赛(2026-08-31 起即带前缀,无 legacy)
    [debate_main]="辩论比赛自动化测试报告_*.md"
    [debate_progress]="辩论比赛自动化测试进度_*.md"
    [debate_screenshot]="辩论比赛截图报告_*.md"
    [debate_screenshot_progress]="辩论比赛截图进度_*.md"
    [debate_usage]="辩论比赛测试工具使用报告_*.md"
    [debate_protocol]="辩论比赛协议抓包分析报告_*.md"
)

# 游戏显示名(commit message / 日志头用)
declare -A GAME_DISPLAY_NAME=(
    [werewolf]="狼人杀"
    [texasholdem]="德州扑克"
    [xiangqi]="象棋"
    [doudizhu]="斗地主"
    [junqi]="军棋"
    [debate]="辩论比赛"
)

# ---------- enqueue_game_glob <game> <kind> [<kind> ...] ----------
# 把指定游戏的 glob 拼接输出（一行一个，兼容旧 _legacy 自动并入）。
# 调用方：find ... -name "$(enqueue_game_glob werewolf main)" -o -name "$(enqueue_game_glob werewolf main legacy)"
# 用法（推荐，配合 find -o）：
#   find TestReport -maxdepth 1 \( -name "$(enqueue_game_glob werewolf main)" -o -name "$(enqueue_game_glob werewolf main_legacy)" \)
enqueue_game_glob() {
    local game="$1"
    shift
    local key out=()
    for kind in "$@"; do
        key="${game}_${kind}"
        if [[ -z "${GAME_GLOBS[${key}]:-}" ]]; then
            echo "[auto_run_common] [WARN] GAME_GLOBS[${key}] 未声明,请在 auto_run_common.sh 顶部补登记" >&2
            continue
        fi
        out+=("${GAME_GLOBS[${key}]}")
    done
    # 多 glob 用 -o 串联,单 glob 直接输出
    if [[ ${#out[@]} -eq 1 ]]; then
        echo "${out[0]}"
    else
        # 多 glob:直接吐出 find -o 串联形式,调用方:
        #   find DIR \( -name "$(enqueue_game_glob g k k_legacy)" \)
        local first=1 piece
        for piece in "${out[@]}"; do
            if [[ ${first} -eq 1 ]]; then
                printf '%s' "${piece}"
                first=0
            else
                printf ' -o -name %s' "${piece}"
            fi
        done
        echo
    fi
}

# list_game_globs_for_kind <kind>
# 跨所有游戏枚举某 kind 的 glob（用于 AutoDebugTestReport 的 union 扫描）。
list_game_globs_for_kind() {
    local kind="$1"
    local key glob
    for key in "${!GAME_GLOBS[@]}"; do
        # 跳过 _legacy 兼容项,留给具体游戏脚本按需启用
        [[ "${key}" == *_legacy ]] && continue
        # 只输出形如 <game>_<kind> 且 kind 匹配
        if [[ "${key}" == *"_${kind}" && "${key}" != *"_${kind}_"* ]]; then
            glob="${GAME_GLOBS[${key}]}"
            echo "${glob}"
        fi
    done
}

# game_display_name <game_key>
game_display_name() {
    local game="$1"
    echo "${GAME_DISPLAY_NAME[${game}]:-${game}}"
}

# ---------- 统一日志工具（§20260821-01 新增） ----------

# bg_log <tag> <message...>
# 后台段日志助手：start_agent_in_background 已把后台 shell 的 stdout/stderr
# 重定向到本次运行日志文件，此处仅负责补上统一时间戳前缀：
#   [2026-08-21 12:00:00] [AutoDebugTestReport] claude 退出码 : 0
bg_log() {
    local tag="$1"
    shift
    echo "[$(date '+%F %T')] [${tag}] $*"
}

# append_run_index <key=value> ...
# 运行索引日志：向 ${PROJECT_DIR}/logs/auto_run_index.log 追加一行，
# 格式 `[YYYY-MM-DD HH:MM:SS] key=value key=value ...`。
# 推荐键：script(脚本名) / agent(Agent 程序名) /
#         event(start|launched|agent_done|commit_done|commit_skip|done|skip_*|error_*) /
#         pid / exit / log / commit
# 用于事后一条 grep 即可审计「哪个脚本、哪个 Agent、何时跑、退出码多少」。
append_run_index() {
    local idx_dir="${PROJECT_DIR:-$(pwd)}/logs"
    local idx_file="${idx_dir}/auto_run_index.log"
    local line="[$(date '+%F %T')]"
    local kv
    for kv in "$@"; do
        line+=" ${kv}"
    done
    mkdir -p "${idx_dir}" 2>/dev/null || true
    printf '%s\n' "${line}" >> "${idx_file}" 2>/dev/null || true
}

# cleanup_old_logs [retain_days]
# 清理 logs/ 下超过保留期的 *.log 运行日志。
#   - 运行索引 auto_run_index.log 永久保留（审计用）；*.lock 锁文件不受影响（非 .log）。
#   - 保留期缺省 30 天，环境变量 LOG_RETAIN_DAYS 可覆盖。
cleanup_old_logs() {
    local retain_days="${1:-${LOG_RETAIN_DAYS:-30}}"
    local log_dir="${PROJECT_DIR:-$(pwd)}/logs"
    [[ -d "${log_dir}" ]] || return 0
    find "${log_dir}" -maxdepth 1 -type f -name '*.log' \
        ! -name 'auto_run_index.log' \
        -mtime +"${retain_days}" -delete 2>/dev/null || true
}

# ---------- print_section_header <tag> <prompt_file> <log_file> <workdir> <agent> ----------
# 启动日志头(替代各脚本裸 echo "=====" ... >> LOG)
# §20260821-01：补充 Git HEAD 与 Bash 版本，便于把日志与代码版本对齐。
# §20260821-02：补充「可用 Agents」候选池，审计选择结果时可见当时可选项。
print_section_header() {
    local tag="$1"
    local prompt_file="$2"
    local log_file="$3"
    local workdir="$4"
    local agent="$5"
    local git_head avail_agents
    git_head="$(git -C "${workdir}" rev-parse --short HEAD 2>/dev/null || echo 'unknown')"
    avail_agents="$(list_available_agents 2>/dev/null | tr '\n' ' ')"
    {
        echo "============================================================"
        echo "[${tag}] 启动时间 : $(date '+%F %T')"
        echo "[${tag}] 工作目录 : ${workdir}"
        echo "[${tag}] 提示词文件: ${prompt_file}"
        echo "[${tag}] 日志文件  : ${log_file}"
        echo "[${tag}] 选中 Agent : ${agent}"
        echo "[${tag}] Agent 二进制: $(command -v "$(agent_binary_of "${agent}")" 2>/dev/null || echo 'NOT FOUND')"
        echo "[${tag}] 可用 Agents: ${avail_agents:-none}"
        echo "[${tag}] Git HEAD  : ${git_head}"
        echo "[${tag}] Bash 版本 : ${BASH_VERSION:-unknown}"
        echo "============================================================"
    } >> "${log_file}"
}

# ---------- locate_prompt_file <prompt_name> [<prompt_name> ...] ----------
# 三层定位 prompt 文件:PROJECT_DIR 绝对路径 → ./相对 → $PWD 相对。
# 命中后输出 readlink -f 的绝对路径；都不存在则输出空串 + stderr 报错。
locate_prompt_file() {
    local prompt_name="$1"
    local project_dir="${PROJECT_DIR:-$(pwd)}"
    local candidate
    for candidate in \
        "${project_dir}/${prompt_name}" \
        "./${prompt_name}" \
        "${PWD}/${prompt_name}"; do
        if [[ -f "${candidate}" ]]; then
            readlink -f "${candidate}"
            return 0
        fi
    done
    echo "[auto_run_common] [ERROR] 找不到 ${prompt_name},已检查:" >&2
    echo "  - ${project_dir}/${prompt_name}" >&2
    echo "  - ./${prompt_name}" >&2
    echo "  - ${PWD}/${prompt_name}" >&2
    return 1
}

# ---------- git_add_safe <glob_path> ----------
# 单路径 git add,失败不阻塞(目录不存在 / 被 .gitignore 整体忽略等)。
git_add_safe() {
    local path_glob="$1"
    git add -- "${path_glob}" 2>/dev/null || true
}

# ---------- git_commit_chinese <type_zh> <game_key> <commit_ts> <source_tag> [extra_msg ...] ----------
# 中文 commit 封装。
#   type_zh: "测试" / "截图" / "修复" / "处理"
#   game_key: werewolf / texasholdem / ...
#   commit_ts: 时间戳(YYYYMMDD_HHMMSS)
#   source_tag: 来源脚本名(如 AutoTestAndSaveReport_Werewolf.sh)
#   extra_msg: 可选追加 commit message 行
# 修复(§20260821-01)：旧实现用 $(basename "$0") 标注来源，但本函数实际在
# start_agent_in_background 的 bash -c 子 shell 中执行，$0 恒为 "bash"，
# 提交信息会误写成「自动提交由 bash 生成」；改为调用方显式传脚本名。
git_commit_chinese() {
    local type_zh="$1"
    local game_key="$2"
    local commit_ts="$3"
    local source_tag="${4:-auto_run}"
    shift 4 2>/dev/null || true
    local game_display
    game_display="$(game_display_name "${game_key}")"
    local -a msg_args=("-m" "${type_zh}: ${game_display} ${type_zh}报告 ${commit_ts} 已完成")
    msg_args+=("-m" "自动提交由 ${source_tag} 生成")
    if [[ $# -gt 0 ]]; then
        msg_args+=("-m" "包含: $*")
    fi
    git commit "${msg_args[@]}"
}

# ---------- start_agent_in_background <log_file> <bash_script> ----------
# nohup setsid bash -c "<script>" &; disown 封装。
#   log_file: stdout/stderr 重定向目标
#   bash_script: 传给 bash -c 的脚本内容
# 输出:启动 PID 到 stdout,失败退出码非 0。
start_agent_in_background() {
    local log_file="$1"
    local bash_script="$2"
    nohup setsid bash -c "${bash_script}" >> "${log_file}" 2>&1 </dev/null &
    local pid=$!
    disown "${pid}" 2>/dev/null || true
    echo "${pid}"
}

# ---------- any_pending_report <project_dir> ----------
# 多游戏 union 扫描,主工程 TestReport/ + 子工程 go-web-debug-tool/UseReport/。
# 输出:首个命中文件名(或空串)。配套 .sh 自行判空退出。
any_pending_report() {
    local project_dir="${1:-${PROJECT_DIR:-$(pwd)}}"
    local main_report use_report glob
    # 主工程:遍历所有游戏的 main glob(去重,_legacy 跳过)
    while IFS= read -r glob; do
        [[ -z "${glob}" ]] && continue
        main_report="$(find "${project_dir}/TestReport" -maxdepth 1 -name "${glob}" ! -name '*_无问题.md' 2>/dev/null | head -1)"
        if [[ -n "${main_report}" ]]; then
            echo "${main_report}"
            return 0
        fi
    done < <(list_game_globs_for_kind "main")
    # 子工程:固定 glob(无游戏名前缀,子工程历史上统一未带前缀)
    use_report="$(find "${project_dir}/go-web-debug-tool/UseReport" -maxdepth 1 \
        \( -name '测试工具使用报告_*.md' -o -name '*测试工具使用报告_*.md' \) \
        ! -name '*_无问题.md' 2>/dev/null | head -1)"
    if [[ -n "${use_report}" ]]; then
        echo "${use_report}"
        return 0
    fi
    echo ""
}

# ---------- game_progress_glob <game> <kind> ----------
# 单游戏单类型 glob 输出(直接给 find -name 用,包含 _legacy 兼容)。
# 旧接口：scan_game_report <game> <kind> → 替换为更直白的 game_progress_glob
game_progress_glob() {
    local game="$1"
    local kind="$2"
    enqueue_game_glob "${game}" "${kind}"
}

# ---------- scan_game_report <project_dir> <game> <kind> ----------
# 扫描某游戏某类报告,首个未归档文件名(含 _legacy 兼容 glob)。
scan_game_report() {
    local project_dir="$1"
    local game="$2"
    local kind="$3"
    local main_g legacy_g
    main_g="${GAME_GLOBS[${game}_${kind}]:-}"
    legacy_g="${GAME_GLOBS[${game}_${kind}_legacy]:-}"
    if [[ -z "${main_g}" ]]; then
        echo ""
        return 1
    fi
    # 主 glob 优先
    local hit
    hit="$(find "${project_dir}" -maxdepth 1 -name "${main_g}" ! -name '*_无问题.md' 2>/dev/null | head -1)"
    if [[ -n "${hit}" ]]; then
        echo "${hit}"
        return 0
    fi
    # legacy 兜底
    if [[ -n "${legacy_g}" ]]; then
        hit="$(find "${project_dir}" -maxdepth 1 -name "${legacy_g}" ! -name '*_无问题.md' 2>/dev/null | head -1)"
        if [[ -n "${hit}" ]]; then
            echo "${hit}"
            return 0
        fi
    fi
    echo ""
}