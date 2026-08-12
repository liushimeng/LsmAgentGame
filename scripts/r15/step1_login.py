#!/usr/bin/env python3
"""R15 Step 1: login 4 accounts + create 4 pages."""
import json
import time
import sys
sys.path.insert(0, "/usr/local/LsmWebGame/scripts/r15")
from common import login, balance, new_page, login_in_page, page_url, dbg_eval, dbg_look

print("=== STEP 1: Login all accounts ===")
with open("/usr/local/LsmWebGame/test_account.json") as f:
    cfg = json.load(f)
accounts = []
for a in cfg["accounts"]:
    try:
        d = login(a["account"], a["password"])
        b = balance(d["token"])["data"]["balance"]
        accounts.append({"account": a["account"], "password": a["password"], **d, "balance": b})
        print(f"  [{a['account']}] login OK, balance={b}")
    except Exception as e:
        print(f"  [{a['account']}] login FAILED: {e}")

# Save accounts
with open("/tmp/r15_accounts.json", "w") as f:
    json.dump(accounts, f)

print("\n=== STEP 2: Create 4 pages, login each ===")
pages = {}
for a in accounts:
    pid = new_page()
    pages[a["account"]] = pid
    login_in_page(pid, a)
    time.sleep(0.5)
    print(f"  {a['account']} @ {pid} URL={page_url(pid)[:80]}")

with open("/tmp/r15_pages.json", "w") as f:
    json.dump(pages, f)
print("\nSaved accounts and pages.")