#!/usr/bin/env python3
"""Play doudizhu: have all 3 bid 0 (pass) then test_03 becomes landlord."""
import json
import urllib.request
import time

DEBUG_BASE = "http://localhost:28999"

def debug_eval(page_id, expr):
    body = {"action": "eval_js", "page_id": page_id, "params": {"expression": expr}}
    r = urllib.request.Request(f"{DEBUG_BASE}/ControlChromePage", 
                                data=json.dumps(body).encode(),
                                headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(r) as resp:
        return json.loads(resp.read())

def debug_click_btn(page_id, text):
    """Click a button by exact innerText match."""
    js = f'Array.from(document.querySelectorAll("button")).filter(b=>b.innerText.trim()==={json.dumps(text)})[0]?.click();JSON.stringify({{clicked:Array.from(document.querySelectorAll("button")).filter(b=>b.innerText.trim()==={json.dumps(text)}).length}})'
    return debug_eval(page_id, js)

with open("/tmp/r14_state.json") as f:
    state = json.load(f)
pages = state["pages"]

def state_text(p, range_=(0, 1500)):
    return debug_eval(p, f"document.body.innerText.substring({range_[0]},{range_[1]})")

def whose_turn(p):
    """Return 'bid' or 'play' or 'waiting'."""
    txt = debug_eval(p, "document.body.innerText")
    if "叫地主" in txt and "不叫" in txt and ("1 分" in txt or "2 分" in txt):
        return "bid"
    if "你的回合" in txt:
        return "play"
    if "对手回合" in txt:
        return "wait_play"
    if "等待其他玩家叫分" in txt:
        return "wait_bid"
    return "unknown"

def which_player_active(p):
    """Return 'self' if it's this player's turn to act (buttons visible)."""
    btns = debug_eval(p, "Array.from(document.querySelectorAll('button')).map(b=>b.innerText.trim())")
    if "不叫" in btns and "叫地主" in btns:
        return "bid_self"
    if "出牌" in btns:
        return "play_self"
    if "要不起" in btns:
        return "play_self"
    return "wait"

print("=== Initial bidding state ===")
for acct in ["test_01", "test_02", "test_03"]:
    s = which_player_active(pages[acct])
    print(f"  {acct}: {s}")

# All 3 should pass (not call landlord), then game becomes start with random landlord
# Actually doudizhu requires at least 1 to call. Let's have test_03 call 1 分.
# First let me see the order - is it test_01 -> test_02 -> test_03?

# Check who is currently active for bidding
for round_n in range(3):
    print(f"\n--- Bidding round {round_n+1} ---")
    for acct in ["test_01", "test_02", "test_03"]:
        p = pages[acct]
        if which_player_active(p) == "bid_self":
            # test_01 pass, test_02 pass, test_03 call 1分
            if acct == "test_03" and round_n == 2:
                # test_03 should call
                r = debug_click_btn(p, "1 分")
                print(f"  {acct}: called 1分, {r}")
            else:
                r = debug_click_btn(p, "不叫")
                print(f"  {acct}: pass, {r}")
            time.sleep(1.5)
        else:
            print(f"  {acct}: waiting")
    time.sleep(2)

time.sleep(2)
print("\n=== Post-bidding state ===")
for acct in ["test_01", "test_02", "test_03"]:
    p = pages[acct]
    txt = state_text(p, (700, 1300))
    print(f"--- {acct} ---")
    print(txt[:600])
    print()