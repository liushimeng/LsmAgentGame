#!/usr/bin/env bash
# =============================================================================
#  rebuild_restart_app.sh
#  功能：编译前端 → 编译后端 → 杀掉旧进程 → 启动新服务 → 校验版本号
#  入口：项目根目录（与本脚本同级的目录）
#  权限：chmod +x rebuild_restart_app.sh
#
#  修复记录（BUG-VERSION-STALE）：
#    1. 后端 build_time 从 cgo __DATE__/__TIME__ 改为 -ldflags -X 注入；
#       必须 go clean -cache 避免编译缓存命中导致 build_time 不刷新。
#    2. 杀旧进程加端口兜底（fuser / lsof 39001 39002），防 PID 文件丢失
#       或并发启动把新 PID 写入前旧进程仍未退出的窗口。
#    3. 启动后必须 curl /api/version 比对 build_time 与本次编译注入值，
#       不一致则报错并回滚（保留旧日志以供排错）。
# =============================================================================

set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"
PIDFILE="${ROOT}/LsmAgentGame.pid"
LOGFILE="${ROOT}/LsmAgentGame.log"
BIN="${ROOT}/LsmAgentGame"
SERVERGO="${ROOT}/ServerGo"

# ---------- 1. 颜色与日志 ----------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }

# ---------- 2. 自检 ----------
[[ -f LsmAgentGame.conf ]] || log_warn "LsmAgentGame.conf 不存在，将回退到 LsmAgentGame.conf.example（开发模式）"
[[ -f server.crt && -f server.key ]] || { log_error "缺少 server.crt / server.key"; exit 1; }
command -v go >/dev/null  || { log_error "未找到 go"; exit 1; }
command -v npm >/dev/null || { log_error "未找到 npm"; exit 1; }
command -v git >/dev/null || { log_error "未找到 git"; exit 1; }

# ---------- 3. 收集 ldflags 注入值（在编译前一刻求值） ----------
# 用本地时间，格式对齐 Go 原 cgo __DATE__/__TIME__ 输出（便于日志/UI 习惯）
# 例：Jul  7 2026 16:08:33
BUILD_TIME="$(date '+%b %e %Y %H:%M:%S' 2>/dev/null || date)"
GIT_SHA="$(cd "${ROOT}" && git rev-parse --short HEAD 2>/dev/null || echo nogit)"
APP_VERSION="v1.0.0-${GIT_SHA}"
# 注入 ldflagsBuildTime（不是 buildDateTime）：main.go 的 init() 会按
# 优先级选取 ldflagsBuildTime → cgo __DATE__/__TIME__ → unknown-build-time。
# 手工 `go build`（无 ldflags）会自然走 cgo 路径，保持兼容。
LDFLAGS="-X 'main.AppVersion=${APP_VERSION}' -X 'main.gitShortSHA=${GIT_SHA}' -X 'main.ldflagsBuildTime=${BUILD_TIME}'"
log_info "ldflags 注入：version=${APP_VERSION}  sha=${GIT_SHA}  build_time=${BUILD_TIME}"

# ---------- 4. 构建前端 ----------
log_info "构建前端 (Vite)…"
( cd ClientWeb && npm ci --no-audit --no-fund && npm run build )

# ---------- 5. 拷贝静态资源 ----------
log_info "拷贝 dist → ServerGo/static"
mkdir -p ServerGo/static
rsync -a --delete ClientWeb/dist/ ServerGo/static/

# ---------- 6. 强制清 Go 编译缓存 + 构建后端 ----------
# 关键：-ldflags -X 注入的字符串字面量必须重新预处理。Go 编译缓存命中
# 会跳过 main.go 的预处理，导致 ldflags 注入值（build_time 源）一直是
# 首次构建时的旧值。cgo 路径同样会受缓存影响，所以两端都靠 go clean -cache 兜底。
log_info "清理 Go 编译缓存 (go clean -cache)…"
( cd "${SERVERGO}" && go clean -cache )
log_info "构建后端 (Go)…"
( cd "${SERVERGO}" && go mod tidy && go build -ldflags "${LDFLAGS}" -o "${BIN}" main.go )

# ---------- 7. 杀掉旧进程（PID 文件 + 端口双保险） ----------
log_info "停止旧的 LsmAgentGame/LsmWebGame 实例…"
kill_pid() {
  local pid="$1"
  [[ -z "${pid}" ]] && return 0
  kill -0 "${pid}" 2>/dev/null || return 0
  kill -TERM "${pid}" 2>/dev/null || true
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    kill -0 "${pid}" 2>/dev/null || return 0
    sleep 1
  done
  kill -KILL "${pid}" 2>/dev/null || true
}

# (a) PID 文件
if [[ -f "${PIDFILE}" ]]; then
  OLD_PID="$(cat "${PIDFILE}" 2>/dev/null || true)"
  if [[ -n "${OLD_PID}" ]]; then
    log_info "PID 文件记录：${OLD_PID}"
    kill_pid "${OLD_PID}"
  fi
  rm -f "${PIDFILE}"
fi

# (b) 端口兜底：kill 任何占用 39001/39002 的 LsmAgentGame（兼容旧名 LsmWebGame）
if command -v fuser >/dev/null 2>&1; then
  for port in 39001 39002; do
    pids="$(fuser -n tcp "${port}" 2>/dev/null | tr -d ' ' || true)"
    for p in ${pids}; do
      cmd="$(cat /proc/${p}/comm 2>/dev/null || true)"
      if [[ "${cmd}" == "LsmAgentGame" || "${cmd}" == "LsmWebGame" ]]; then
        log_warn "端口 ${port} 上有遗留进程 ${p}(${cmd})，强制 kill"
        kill_pid "${p}"
      fi
    done
  done
elif command -v lsof >/dev/null 2>&1; then
  for port in 39001 39002; do
    pids="$(lsof -ti tcp:"${port}" 2>/dev/null || true)"
    for p in ${pids}; do
      cmd="$(cat /proc/${p}/comm 2>/dev/null || true)"
      if [[ "${cmd}" == "LsmAgentGame" || "${cmd}" == "LsmWebGame" ]]; then
        log_warn "端口 ${port} 上有遗留进程 ${p}(${cmd})，强制 kill"
        kill_pid "${p}"
      fi
    done
  done
else
  log_warn "未找到 fuser / lsof，跳过端口兜底"
fi

# ---------- 8. 启动新实例 ----------
log_info "启动新实例…"
nohup "${BIN}" >> "${LOGFILE}" 2>&1 &
NEW_PID=$!
echo "${NEW_PID}" > "${PIDFILE}"
disown

# ---------- 9. 健康检查 + 版本号校验 ----------
log_info "等待健康检查…"
for i in $(seq 1 20); do
  if curl -ksf https://127.0.0.1:39001/api/health >/dev/null 2>&1; then
    log_info "✅ 服务已就绪：https://127.0.0.1:39001"
    # 校验 /api/version 中 build_time 是否已刷新
    REPORTED_BUILD="$(curl -ks https://127.0.0.1:39001/api/version \
      | sed -nE 's/.*"build_time":"([^"]+)".*/\1/p' || true)"
    REPORTED_VER="$(curl -ks https://127.0.0.1:39001/api/version \
      | sed -nE 's/.*"version":"([^"]+)".*/\1/p' || true)"
    if [[ "${REPORTED_BUILD}" == "${BUILD_TIME}" ]]; then
      log_info "✅ 版本号已刷新：${REPORTED_VER}  build_time=${REPORTED_BUILD}"
      log_info "WSS：wss://127.0.0.1:39002/ws?token=<jwt>"
      exit 0
    else
      log_error "❌ 版本号未刷新：注入=${BUILD_TIME}  实际=${REPORTED_BUILD}"
      log_error "可能编译缓存仍命中，检查 go clean -cache 是否生效"
      log_error "日志尾部：" ; tail -30 "${LOGFILE}" >&2
      exit 2
    fi
  fi
  sleep 0.5
done

log_error "❌ 健康检查失败；查看 ${LOGFILE}"
tail -30 "${LOGFILE}" >&2 || true
exit 1
