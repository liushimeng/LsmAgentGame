#!/usr/bin/env bash
# test_wallet_ws.sh — WS push test for wallet.balance.
#
#   1. Register a fresh user (HTTP) — response carries a JWT.
#   2. Connect wss://127.0.0.1:39002/ws?token=<jwt> via scripts/smoke/wsclient.go.
#   3. POST /api/wallet/claim-daily.
#   4. Assert a `wallet.balance` frame with delta=+2000 (or document bug).
#
# TAP output. Exits 0 on green, 1 on any failure.
set -u

PASS=0
FAIL=0
FAILED_NAMES=()

HOST="${WALLET_API_HOST:-https://127.0.0.1:39001}"
WSS="${WALLET_WSS_HOST:-wss://127.0.0.1:39002/ws}"
SCRIPTDIR="$(cd "$(dirname "$0")" && pwd)"
# scripts/smoke/ 上两级为项目根(2026-08-15 自 scripts/ 迁入,层级加深一级)
ROOTDIR="$(dirname "$(dirname "$SCRIPTDIR")")"
CLIENT_OUT="${SCRIPTDIR}/.wsclient"
ACCOUNT=""
TOKEN=""
WS_LOG="${SCRIPTDIR}/.ws.log"

tap_pass()  { PASS=$((PASS+1)); echo "ok $PASS - $1"; }
tap_fail()  { FAIL=$((FAIL+1)); FAILED_NAMES+=("$1"); echo "not ok $((PASS+FAIL)) - $1"; }

if ! command -v jq >/dev/null 2>&1; then echo "1..0 # SKIP jq not available"; exit 0; fi
if ! command -v curl >/dev/null 2>&1; then echo "1..0 # SKIP curl not available"; exit 0; fi
if ! curl -sk "${HOST}/api/health" >/dev/null 2>&1; then echo "1..0 # SKIP ${HOST} unreachable"; exit 0; fi

# DB 凭据从环境变量读取,不接受默认值,以免泄漏真实密码到 git。
if [ -z "${WALLET_DB_PASS:-}" ]; then echo "1..0 # SKIP WALLET_DB_PASS 未设置" >&2; exit 0; fi
export MYSQL_PWD="$WALLET_DB_PASS"
REFERRER=$(mysql -h"${WALLET_DB_HOST:-127.0.0.1}" -P"${WALLET_DB_PORT:-3306}" \
    -u"${WALLET_DB_USER:-superuser}" "${WALLET_DB_NAME:-lsmDB}" -N \
    -e "SELECT my_invite_code FROM t_lsm_game_user ORDER BY created_at ASC LIMIT 1;" 2>/dev/null | head -1 | tr -d '[:space:]')
if [ -z "$REFERRER" ] || [ "$REFERRER" = "NULL" ]; then echo "1..0 # SKIP no referrer"; exit 0; fi

ACCOUNT="itws_$(openssl rand -hex 6 2>/dev/null || date +%s%N)"
payload=$(printf '{"account":"%s","password":"ItWsPass_1ABCxyz","referrer_code":"%s"}' \
    "$ACCOUNT" "$REFERRER")
reg_resp=$(curl -sk -XPOST -H "Content-Type: application/json" -d "$payload" \
    "${HOST}/api/auth/register" 2>/dev/null)
TOKEN=$(echo "$reg_resp" | jq -r '.data.token // empty')
if [ -z "$TOKEN" ]; then echo "1..0 # SKIP register failed"; exit 0; fi
echo "# account=$ACCOUNT"

# Build wsclient once.
GOWS_PATH="${ROOTDIR}/ServerGo"
if [ ! -d "${GOWS_PATH}" ]; then echo "1..0 # SKIP ServerGo dir missing"; exit 0; fi
if ! (cd "${GOWS_PATH}" && go build -o "${CLIENT_OUT}" "${SCRIPTDIR}/wsclient.go" 2>/tmp/wsbuild.log); then
    echo "1..0 # SKIP wsclient build failed: $(tail -3 /tmp/wsbuild.log)"; exit 0
fi

# Run client in background.
rm -f "$WS_LOG"
"${CLIENT_OUT}" -url "${WSS}?token=${TOKEN}" > "$WS_LOG" 2>&1 &
CLIENT_PID=$!
sleep 2

# Trigger claim-daily from a different HTTP session.
curl -sk -XPOST -H "Authorization: Bearer $TOKEN" "${HOST}/api/wallet/claim-daily" >/dev/null 2>&1
sleep 2

kill "$CLIENT_PID" 2>/dev/null
wait "$CLIENT_PID" 2>/dev/null || true

# Assertions.
frame_count=$(grep -c '^WS ' "$WS_LOG" 2>/dev/null || true)
frame_count="${frame_count:-0}"
if [ "$frame_count" -ge 1 ]; then
    tap_pass "WS connection received $frame_count frame(s)"
else
    tap_fail "WS connection received no frames"
fi

push_line=$(grep '"type":"wallet.balance"' "$WS_LOG" 2>/dev/null | tail -1 || true)
if [ -n "$push_line" ]; then
    delta=$(echo "$push_line" | jq -r '.payload.delta // .delta // empty' 2>/dev/null)
    reason=$(echo "$push_line" | jq -r '.payload.reason // .reason // empty' 2>/dev/null)
    if [ "$delta" = "2000" ]; then
        tap_pass "wallet.balance push delta=+2000 reason=$reason"
    else
        tap_fail "wallet.balance push delta=$delta (expected 2000)"
    fi
else
    # BUG(wallet-ws-push): api/wallet_auth.go does not call hub.PushBalanceChange
    tap_fail "no wallet.balance push observed after claim-daily (bug wallet-ws-push)"
fi

echo "1..$((PASS+FAIL))"
if [ $FAIL -gt 0 ]; then echo "# FAIL: ${FAILED_NAMES[*]}"; exit 1; fi
exit 0
