#!/usr/bin/env bash
# test_wallet_db_consistency.sh — DB invariant checks for the wallet subsystem.
#
# Invariants asserted:
#   1. wallet.balance == SUM(wallet_tx.amount) for every user ("the ledger must
#      always reconcile to the wallet balance" — the SUM generalises on the
#      seed amount, which the service allows to be non-1000 in edge cases).
#   2. Daily reward uniqueness: (user_id, reward_date) must be unique.
#
# Reads $DB_USER / $DB_PASS / $DB_NAME from environment. **No defaults** for
# DB_PASS — fail-fast if unset, to avoid leaking real production passwords
# into the repo. Other fields default to the dev DB endpoint.
# TAP output. Exits 0 if both checks pass, 1 on any non-zero drift/dup.

set -u

PASS=0
FAIL=0
FAILED_NAMES=()

DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="${DB_PORT:-3306}"
DB_USER="${DB_USER:-superuser}"
DB_NAME="${DB_NAME:-lsmDB}"
if [ -z "${DB_PASS:-}" ]; then
    echo "1..0 # SKIP DB_PASS 未设置 — 必须显式提供 DB 密码环境变量(避免硬编码泄漏)" >&2
    exit 0
fi
# 用 MYSQL_PWD 注入密码,避免出现在 ps 列表与脚本源码中。
export MYSQL_PWD="$DB_PASS"
MYSQL="mysql -h${DB_HOST} -P${DB_PORT} -u${DB_USER} ${DB_NAME}"

tap_pass() { PASS=$((PASS+1)); echo "ok $PASS - $1"; }
tap_fail() { FAIL=$((FAIL+1)); FAILED_NAMES+=("$1"); echo "not ok $((PASS+FAIL)) - $1"; }

if ! command -v mysql >/dev/null 2>&1; then
    echo "1..0 # SKIP mysql client not installed"; exit 0
fi
if ! ${MYSQL} -e "SELECT 1" >/dev/null 2>&1; then
    echo "1..0 # SKIP DB not reachable"; exit 0
fi

# Check 1: ledger balance must equal wallet.balance for every user.
drift_sql="
  SELECT w.user_id, w.balance, IFNULL(SUM(t.amount), 0) AS ledger_sum,
         w.balance - IFNULL(SUM(t.amount), 0) AS drift
  FROM t_lsm_game_wallet w
  LEFT JOIN t_lsm_game_wallet_tx t USING(user_id)
  GROUP BY w.user_id, w.balance
  HAVING ABS(w.balance - IFNULL(SUM(t.amount), 0)) > 0;
"
drift_raw=$(${MYSQL} -e "${drift_sql}" 2>/dev/null)
drift_count=$(echo "${drift_raw}" | awk 'END{print NR-1}')   # subtract header
drift_count="${drift_count:-0}"
if [ "${drift_count}" -eq 0 ]; then
    tap_pass "wallet.balance == SUM(ledger.amount) for every user (drift=0)"
else
    # Repeat with column headers for the failure log.
    drift_detail=$(${MYSQL} -N -e "${drift_sql}" 2>/dev/null)
    tap_fail "t_lsm_game_wallet drift detected: ${drift_count} user(s) out of balance
$(echo "${drift_raw}")"
fi

# Check 2: daily reward uniqueness.
dup_sql="
  SELECT user_id, reward_date, COUNT(*) AS c
  FROM t_lsm_game_daily_reward
  GROUP BY user_id, reward_date
  HAVING c > 1;
"
dup_raw=$(${MYSQL} -e "${dup_sql}" 2>/dev/null)
dup_count=$(echo "${dup_raw}" | awk 'END{print NR-1}')
dup_count="${dup_count:-0}"
if [ "${dup_count}" -eq 0 ]; then
    tap_pass "t_lsm_game_daily_reward unique per (user_id, reward_date)"
else
    tap_fail "t_lsm_game_daily_reward duplicate rows:
$(echo "${dup_raw}")"
fi

# ── Summary ────────────────────────────────────────────────────────
echo "1..$((PASS+FAIL))"
if [ $FAIL -gt 0 ]; then
    echo "# FAIL: ${FAILED_NAMES[*]}"
    exit 1
fi
exit 0
