#!/usr/bin/env bash
# agent_cli_common.sh
# ---------------------------------------------------------------
# 多 Agent CLI 随机选择公共库（被 AutoDebugTestReport.sh /
# AutoScreenshotWerewolf.sh / AutoTestAndSaveReport.sh source 引用）。
#
# 支持的编程 Agent CLI（2026-09-15 缩减为 3 个，命令以本机为准）：
#   claude    v2.1.238   claude --dangerously-skip-permissions -p "<prompt>"
#   opencode  v1.18.19   opencode run --auto -m <provider/model> "<prompt>"
#                        （--auto = 自动批准未显式拒绝的权限，危险模式）
#   codex     0.154.0    codex exec --dangerously-bypass-approvals-and-sandbox -
#                        （exec=非交互；`-` 从 stdin 读提示词文件，规避 argv 转义风险；
#                          --dangerously-bypass-approvals-and-sandbox = 免审批危险模式）
#
# 用法（在调用脚本中）：
#   source "${PROJECT_DIR}/agent_cli_common.sh"
#   pick_agent "<脚本名>"                     # 随机选一个可用 Agent
#   pick_agent "<脚本名>" claude              # 首选 claude(§20260821-02)：可用则必选，
#                                             # 不可用自动降级随机选（日志有 WARN）
#   pick_agent_from_list "<脚本名>" claude opencode  # 从指定列表中随机选（专业任务用专业 Agent）
#   run_agent_with_prompt "${SELECTED_AGENT}" "${PROMPT_FILE}"
#
# Agent 选择优先级（§20260821-02）：
#   AGENT_CLI 环境变量强制指定 > pick_agent 第二参数首选 Agent > 随机选择
#
# 环境变量：
#   AGENT_CLI           强制指定 Agent（claude|opencode|codex），为空走首选/随机
#   CLAUDE_BIN / OPENCODE_BIN / CODEX_BIN   各二进制路径覆盖
#   OPENCODE_MODEL      opencode 模型（provider/model 格式），缺省自动从
#                       ~/.config/opencode/config.json 推导
# ---------------------------------------------------------------

# ---------- 二进制路径（可被环境变量覆盖） ----------
CLAUDE_BIN="${CLAUDE_BIN:-claude}"
OPENCODE_BIN="${OPENCODE_BIN:-opencode}"
CODEX_BIN="${CODEX_BIN:-codex}"

# 候选 Agent 全集（顺序无关，随机选取）
AGENT_CANDIDATES=(claude opencode codex)

# SELECTED_AGENT 由 pick_agent 填充
SELECTED_AGENT=""

# agent_binary_of <agent> —— 返回对应二进制名
agent_binary_of() {
    case "$1" in
        claude)   echo "${CLAUDE_BIN}" ;;
        opencode) echo "${OPENCODE_BIN}" ;;
        codex)    echo "${CODEX_BIN}" ;;
        *)        echo "" ;;
    esac
}

# list_available_agents —— 输出 PATH 中可用的 Agent 名（每行一个）
list_available_agents() {
    local a bin
    for a in "${AGENT_CANDIDATES[@]}"; do
        bin="$(agent_binary_of "${a}")"
        if command -v "${bin}" >/dev/null 2>&1; then
            echo "${a}"
        fi
    done
}

# pick_agent <caller_tag> [preferred_agent] —— 选择一个可用 Agent，结果写入 SELECTED_AGENT。
# 选择优先级（§20260821-02）：
#   1. AGENT_CLI 环境变量强制指定（用于定向测试某个 Agent），不可用则报错退出；
#   2. preferred_agent 首选（如 AutoDebugTestReport 首选 claude/Claude Code CLI）：
#      可用则必选；不可用打 WARN 并自动降级随机选择（不阻塞自动化流水线）；
#   3. 无首选时从全部可用 Agent 中随机选择。
# 无可用 Agent 时直接退出（exit 3）。
pick_agent() {
    local caller_tag="${1:-AutoAgent}"
    local preferred="${2:-}"
    local available=()
    local a
    while IFS= read -r a; do
        [[ -n "${a}" ]] && available+=("${a}")
    done < <(list_available_agents)

    if [[ ${#available[@]} -eq 0 ]]; then
        echo "[${caller_tag}] [ERROR] 未发现任何可用 Agent CLI（候选: ${AGENT_CANDIDATES[*]}），拒绝启动。" >&2
        exit 3
    fi

    if [[ -n "${AGENT_CLI:-}" ]]; then
        local found=""
        for a in "${available[@]}"; do
            if [[ "${a}" == "${AGENT_CLI}" ]]; then found="${a}"; break; fi
        done
        if [[ -z "${found}" ]]; then
            echo "[${caller_tag}] [ERROR] AGENT_CLI=${AGENT_CLI} 不可用；当前可用: ${available[*]}" >&2
            exit 3
        fi
        SELECTED_AGENT="${found}"
        echo "[${caller_tag}] AGENT_CLI 强制指定 Agent : ${SELECTED_AGENT}"
    elif [[ -n "${preferred}" ]]; then
        local found=""
        for a in "${available[@]}"; do
            if [[ "${a}" == "${preferred}" ]]; then found="${a}"; break; fi
        done
        if [[ -n "${found}" ]]; then
            SELECTED_AGENT="${found}"
            echo "[${caller_tag}] 首选命中 Agent : ${SELECTED_AGENT}（可用候选: ${available[*]}）"
        else
            echo "[${caller_tag}] [WARN] 首选 Agent '${preferred}' 不可用，降级随机选择（可用候选: ${available[*]}）" >&2
            local idx=$(( RANDOM % ${#available[@]} ))
            SELECTED_AGENT="${available[$idx]}"
            echo "[${caller_tag}] 随机选中 Agent : ${SELECTED_AGENT}"
        fi
    else
        local idx=$(( RANDOM % ${#available[@]} ))
        SELECTED_AGENT="${available[$idx]}"
        echo "[${caller_tag}] 随机选中 Agent : ${SELECTED_AGENT}（可用候选: ${available[*]}）"
    fi
}

# pick_agent_from_list <caller_tag> <agent1> [agent2] ... —— 从指定列表中随机选择可用 Agent。
# 用于「专业任务用专业 Agent」场景：AutoDebugTestReport 只需 claude/opencode，
# 不把 codex 纳入随机池。
# 选择优先级：AGENT_CLI 环境变量强制指定 > 从指定列表中随机选择 >
#   指定列表全部不可用时降级为全部可用 Agent 随机选择（日志有 WARN）。
pick_agent_from_list() {
    local caller_tag="${1:-AutoAgent}"
    shift
    local allowed=("$@")
    local available=()
    local a
    while IFS= read -r a; do
        [[ -n "${a}" ]] && available+=("${a}")
    done < <(list_available_agents)

    if [[ ${#available[@]} -eq 0 ]]; then
        echo "[${caller_tag}] [ERROR] 未发现任何可用 Agent CLI（候选: ${AGENT_CANDIDATES[*]}），拒绝启动。" >&2
        exit 3
    fi

    if [[ -n "${AGENT_CLI:-}" ]]; then
        local found=""
        for a in "${available[@]}"; do
            if [[ "${a}" == "${AGENT_CLI}" ]]; then found="${a}"; break; fi
        done
        if [[ -z "${found}" ]]; then
            echo "[${caller_tag}] [ERROR] AGENT_CLI=${AGENT_CLI} 不可用；当前可用: ${available[*]}" >&2
            exit 3
        fi
        SELECTED_AGENT="${found}"
        echo "[${caller_tag}] AGENT_CLI 强制指定 Agent : ${SELECTED_AGENT}"
        return
    fi

    # 从 allowed 列表中筛选出当前可用的
    local allowed_available=()
    local b
    for a in "${allowed[@]}"; do
        for b in "${available[@]}"; do
            if [[ "${a}" == "${b}" ]]; then
                allowed_available+=("${a}")
                break
            fi
        done
    done

    if [[ ${#allowed_available[@]} -eq 0 ]]; then
        echo "[${caller_tag}] [WARN] 指定 Agent 列表 ${allowed[*]} 均不可用，降级为全部可用 Agent 随机选择（可用候选: ${available[*]}）" >&2
        local idx=$(( RANDOM % ${#available[@]} ))
        SELECTED_AGENT="${available[$idx]}"
        echo "[${caller_tag}] 随机选中 Agent : ${SELECTED_AGENT}（可用候选: ${available[*]}）"
        return
    fi

    local idx=$(( RANDOM % ${#allowed_available[@]} ))
    SELECTED_AGENT="${allowed_available[$idx]}"
    echo "[${caller_tag}] 从指定列表随机选中 Agent : ${SELECTED_AGENT}（指定: ${allowed[*]}，可用: ${allowed_available[*]}）"
}

# opencode_model_arg —— 推导 opencode 的 provider/model 参数。
# 实测（2026-08-14）：opencode run 仅凭全局 config 的 "model" 字段
# （无 provider 前缀）会报 ProviderModelNotFoundError，必须显式 -m。
opencode_model_arg() {
    if [[ -n "${OPENCODE_MODEL:-}" ]]; then
        echo "${OPENCODE_MODEL}"
        return 0
    fi
    local cfg="${HOME}/.config/opencode/config.json"
    local m=""
    if [[ -f "${cfg}" ]] && command -v node >/dev/null 2>&1; then
        m="$(node -e 'try{const c=require(process.argv[1]);process.stdout.write(c.model||"")}catch(e){}' "${cfg}" 2>/dev/null || true)"
    fi
    if [[ -z "${m}" ]]; then
        m="liusm191-server-model/liusm191-server-model"
    elif [[ "${m}" != */* ]]; then
        # 仅有模型名时，本项目 provider 与模型同名，补全为 provider/model
        m="${m}/${m}"
    fi
    echo "${m}"
}

# run_agent_with_prompt <agent> <prompt_file> [project_dir]
# 以「放开权限、全程自动化」方式运行指定 Agent，提示词取自 prompt 文件。
# 返回 Agent 进程的退出码。
run_agent_with_prompt() {
    local agent="$1"
    local prompt_file="$2"
    local project_dir="${3:-${PROJECT_DIR:-$(pwd)}}"
    local prompt_text

    if [[ ! -f "${prompt_file}" ]]; then
        echo "[agent_cli_common] [ERROR] 提示词文件不存在: ${prompt_file}" >&2
        return 64
    fi
    prompt_text="$(cat "${prompt_file}")"

    case "${agent}" in
        claude)
            # §20260821-02：与 opencode/codex 对齐，显式 cd 到项目目录，
            # 不再隐式依赖调用方 cwd（Claude Code 以 cwd 为工作区）
            (cd "${project_dir}" && "${CLAUDE_BIN}" --dangerously-skip-permissions -p "${prompt_text}")
            ;;
        opencode)
            local model_arg
            model_arg="$(opencode_model_arg)"
            echo "[agent_cli_common] opencode 模型参数: ${model_arg}"
            (cd "${project_dir}" && "${OPENCODE_BIN}" run --auto -m "${model_arg}" "${prompt_text}")
            ;;
        codex)
            # stdin 直读 UTF-8 提示词文件，安全传递超长中文文本（规避 argv 转义风险）；
            # --dangerously-bypass-approvals-and-sandbox = 免审批危险模式
            (cd "${project_dir}" && "${CODEX_BIN}" exec \
                --dangerously-bypass-approvals-and-sandbox \
                - < "${prompt_file}")
            ;;
        *)
            echo "[agent_cli_common] [ERROR] 未知 Agent: ${agent}" >&2
            return 64
            ;;
    esac
}
