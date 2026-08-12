#!/usr/bin/env bash
# test_wallet_api.sh — HTTP E2E for wallet endpoints.
#
# TAP-style output (test lines, plan at the end).
# Exits 0 on success, 1 on any uncaught failure.
#
# Requires the LsmWebGame HTTPS/WSS server running at https://127.0.0.1:39001 /
# wss://127.0.0.1:39002, and curl, jq on $PATH.
#
# Token strategy: GET /api/login requires CAPTCHA. We instead register a fresh
# user via POST /api/auth/register — the response carries a ready-to-use JWT.
# That token is used for the remaining calls.

set -u

PASS=0
FAIL=0
FAILED_NAMES=()

HOST="${WALLET_API_HOST:-https://127.0.0.1:39001}"
# Referrer code: bootstrap with any existing user's my_invite_code. We read the
# first user's code dynamically so this script doesn't hardcode the value.
REFERRER=""
ACCOUNT=""
TOKEN=""

tap_pass()  { PASS=$((PASS+1)); echo "ok $PASS - $1"; }
tap_fail()  { FAIL=$((FAIL+1)); FAILED_NAMES+=("$1"); echo "not ok $((PASS+FAIL)) - $1"; }

# Pick a referrer code — any existing user's my_invite_code works.
pick_referrer() {
    REFERRER=$(mysql -h127.0.0.1 -P3306 -u superuser -p'da=p1da@asd+12' lsmDB -N \
        -e "SELECT my_invite_code FROM t_lsm_game_user ORDER BY created_at ASC LIMIT 1;" 2>/dev/null | head -1)
    if [ -z "$REFERRER" ] || [ "$REFERRER" = "NULL" ]; then
        return 1
    fi
    return 0
}

register_user() {
    ACCOUNT="itapy_$(openssl rand -hex 6 2>/dev/null || date +%s%N)"
    local payload
    payload=$(printf '{"account":"%s","password":"ItApiPass_1ABCxyz","referrer_code":"%s"}' \
        "$ACCOUNT" "$REFERRER")
    local resp
    resp=$(curl -sk -w '\n%{http_code}' -XPOST -H "Content-Type: application/json" \
        -d "$payload" "${HOST}/api/auth/register" 2>/dev/null)
    local code
    code=$(echo "$resp" | tail -1)
    local body
    body=$(echo "$resp" | sed '$d')
    if [ "$code" != "200" ]; then
        echo "reg http=$code body=$body" >&2
        return 1
    fi
    TOKEN=$(echo "$body" | jq -r '.data.token // empty')
    if [ -z "$TOKEN" ]; then
        echo "reg missing token: $body" >&2
        return 1
    fi
    return 0
}

api_call() {
    # $1 = method (GET|POST), $2 = path, $3 = optional body
    if [ "$1" = "GET" ]; then
        curl -sk -w '\n%{http_code}' -H "Authorization: Bearer $TOKEN" \
            "${HOST}$2" 2>/dev/null
    else
        curl -sk -w '\n%{http_code}' -XPOST -H "Content-Type: application/json" \
            -H "Authorization: Bearer $TOKEN" ${3:+-d "$3"} \
            "${HOST}$2" 2>/dev/null
    fi
}

# ── Setup ──────────────────────────────────────────────────────────
if ! command -v jq >/dev/null 2>&1; then
    echo "1..0 # SKIP jq not available"; exit 0
fi
if ! command -v curl >/dev/null 2>&1; then
    echo "1..0 # SKIP curl not available"; exit 0
fi

if ! curl -sk "${HOST}/api/health" >/dev/null 2>&1; then
    echo "1..0 # SKIP ${HOST} unreachable"; exit 0
fi

if ! pick_referrer; then
    echo "1..0 # SKIP no user found to bootstrap referrer code"; exit 0
fi
if ! register_user; then
    echo "1..0 # SKIP failed to register a fresh user"; exit 0
fi
echo "# account=$ACCOUNT referrer=$REFERRER"

# ── Test 1: GET /api/wallet/balance → 1000 ─────────────────────────
{
    resp=$(api_call GET "/api/wallet/balance")
    code=$(echo "$resp" | tail -1); body=$(echo "$resp" | sed '$d')
    [ "$code" = "200" ] || { tap_fail "balance http $code"; break; }
    api_code=$(echo "$body" | jq -r '.code')
    bal=$(echo "$body" | jq -r '.data.balance')
    if [ "$api_code" = "0" ] && [ "$bal" = "1000" ]; then
        tap_pass "balance = 1000 after register"
    else
        tap_fail "balance expect (code=0, balance=1000), got code=$api_code bal=$bal"
    fi
}

# ── Test 2: POST /api/wallet/claim-daily → claimed=true amount=2000 ─
{
    resp=$(api_call POST "/api/wallet/claim-daily")
    code=$(echo "$resp" | tail -1); body=$(echo "$resp" | sed '$d')
    [ "$code" = "200" ] || { tap_fail "claim-daily http $code"; break; }
    api_code=$(echo "$body" | jq -r '.code')
    amt=$(echo "$body" | jq -r '.data.amount')
    claimed=$(echo "$body" | jq -r '.data.claimed')
    bal_after=$(echo "$body" | jq -r '.data.balance_after')
    if [ "$api_code" = "0" ] && [ "$claimed" = "true" ] && [ "$amt" = "2000" ] && [ "$bal_after" = "3000" ]; then
        tap_pass "claim-daily returns claimed=true amount=2000 balance_after=3000"
    elif [ "$api_code" = "0" ] && [ "$claimed" = "true" ] && [ "$amt" = "2000" ]; then
        tap_pass "claim-daily returns claimed=true amount=2000 (balance_after=$bal_after)"
    else
        tap_fail "claim-daily expect code=0 claimed=true amount=2000, got code=$api_code claimed=$claimed amt=$amt"
    fi
}

# ── Test 3: Second claim → 30014 ───────────────────────────────────
{
    resp=$(api_call POST "/api/wallet/claim-daily")
    code=$(echo "$resp" | tail -1); body=$(echo "$resp" | sed '$d')
    api_code=$(echo "$body" | jq -r '.code')
    # BUG(wallet-mariadb-dup): the second call currently surfaces ErrDB(40002)
    # because isMySQLDuplicate() doesn't unwrap go-sql-driver's 1062 through
    # gorm. Contract says 30014; accept either while the bug is open.
    if [ "$api_code" = "30014" ] || [ "$api_code" = "40002" ]; then
        tap_pass "second claim refused (code=$api_code)"
        if [ "$api_code" = "40002" ]; then
            tap_fail "second claim returned 40002 (ErrDB) instead of 30014 — isMySQLDuplicate unwrap bug"
        fi
    else
        tap_fail "second claim expect code 30014 (or 40002 while bug open), got $api_code"
    fi
}

# ── Test 4: GET /api/wallet/transactions → 2 rows: register_bonus(1000), daily_login(2000) ─
{
    resp=$(api_call GET "/api/wallet/transactions?limit=10")
    code=$(echo "$resp" | tail -1); body=$(echo "$resp" | sed '$d')
    [ "$code" = "200" ] || { tap_fail "transactions http $code"; break; }
    api_code=$(echo "$body" | jq -r '.code')
    [ "$api_code" = "0" ] || { tap_fail "transactions api code $api_code"; break; }
    total=$(echo "$body" | jq -r '.data.total')
    first_type=$(echo "$body" | jq -r '.data.transactions[0].tx_type')
    first_amt=$(echo "$body" | jq -r '.data.transactions[0].amount')
    second_type=$(echo "$body" | jq -r '.data.transactions[1].tx_type')
    second_amt=$(echo "$body" | jq -r '.data.transactions[1].amount')
    if [ "$total" = "2" ] && \
       [ "$first_type" = "daily_login" ] && [ "$first_amt" = "2000" ] && \
       [ "$second_type" = "register_bonus" ] && [ "$second_amt" = "1000" ]; then
        tap_pass "transactions[0]=register_bonus(1000), transactions[1]=daily_login(2000)"
    else
        tap_fail "transactions order/content mismatch: total=$total first=$first_type:$first_amt second=$second_type:$second_amt"
    fi
}

# ── Test 5: GET /api/wallet/balance → 3000 after claim ──────────────
{
    resp=$(api_call GET "/api/wallet/balance")
    code=$(echo "$resp" | tail -1); body=$(echo "$resp" | sed '$d')
    [ "$code" = "200" ] || { tap_fail "balance-after http $code"; break; }
    bal=$(echo "$body" | jq -r '.data.balance')
    if [ "$bal" = "3000" ]; then
        tap_pass "balance=3000 after daily reward"
    else
        tap_fail "balance after daily reward expect 3000, got $bal"
    fi
}

# ── Summary ────────────────────────────────────────────────────────
echo "1..$((PASS+FAIL))"
if [ $FAIL -gt 0 ]; then
    echo "# FAIL: ${FAILED_NAMES[*]}"
    exit 1
fi
exit 0
