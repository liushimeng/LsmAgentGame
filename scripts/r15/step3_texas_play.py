#!/usr/bin/env python3
"""R15 Step 3: Play Texas Hold'em game (check/call/fold)."""
import json
import time
import sys
import os
sys.path.insert(0, "/usr/local/LsmAgentGame/scripts/r15")
from common import dbg_eval, dbg_click, dbg_look, dbg_screenshot

PAGES = {
    "test_01": "p_d55b494a",  # SB
    "test_02": "p_3d1b51c0",  # BB
}

ROOM = "fa041d6b-13a7-4a31-a9dd-76cec6307a5a"

def get_state(page_id):
    txt = dbg_eval(page_id, "document.body.innerText")
    return {
        "txt": txt[:500],
        "is_my_turn": "你的回合" in txt or "your turn" in txt.lower(),
        "is_opp_turn": "对手回合" in txt or "opponent turn" in txt.lower(),
        "winner": "胜利" in txt or "🏆" in txt,
        "loser": "失败" in txt,
        "game_over": "游戏结束" in txt or "🏆" in txt or "胜利" in txt or "失败" in txt,
        "btns": dbg_eval(page_id, "Array.from(document.querySelectorAll('button')).map(b=>b.innerText.trim()).filter(t=>t.length>0)"),
    }

# Round 1: test_01 (SB) acts first
print("=== Initial state ===")
for k, v in PAGES.items():
    s = get_state(v)
    print(f"  {k}: turn={'MINE' if s['is_my_turn'] else 'OPP' if s['is_opp_turn'] else '?'} btns={s['btns']}")

# Strategy: test_01 calls (跟注), then test_02 checks (or bets)
# test_01: 跟注 $100
print("\n=== test_01 calls $100 ===")
r = dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('button')).find(b=>b.innerText.includes('跟注'))?.click();'ok'")
print(f"  click: {r}")
time.sleep(3)

# test_02: check (if available) or call
print("\n=== test_02 (BB) acts ===")
s2 = get_state(PAGES["test_02"])
print(f"  state: turn={'MINE' if s2['is_my_turn'] else '?'} btns={s2['btns']}")

# Now we should be at the flop. Let's check
for k, v in PAGES.items():
    s = get_state(v)
    print(f"  {k}: txt={s['txt'][:200]}...")

# Save intermediate screenshots
os.makedirs("/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500", exist_ok=True)
dbg_screenshot(PAGES["test_01"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/texas_after_preflop.png")

# Flop betting round - test_01 (SB) acts first post-flop
print("\n=== Flop betting round ===")
def play_turn(account, page_id, action_strategy="first_legal"):
    """Play one turn for given player. action_strategy: 'call/check', 'bet', 'fold', 'check', 'first_legal'."""
    s = get_state(page_id)
    if not s["is_my_turn"]:
        print(f"  {account}: not my turn, state: turn={'MINE' if s['is_my_turn'] else '?'}")
        return False
    btns = s["btns"]
    # Print available actions
    actions = [b for b in btns if any(k in b for k in ["跟注", "下注", "弃牌", "让牌", "check", "call", "bet", "fold", "全押"])]
    print(f"  {account} actions: {actions}")
    
    # Find best action by strategy
    chosen = None
    if action_strategy == "check":
        # Prefer check (让牌) when available
        for a in ["让牌", "check"]:
            for b in btns:
                if a in b.lower() or a in b:
                    chosen = b
                    break
            if chosen: break
    if not chosen:
        for b in btns:
            if "弃牌" in b and "认输" in b:
                continue
            if "弃牌" in b:
                chosen = b  # fold
                break
    if not chosen and action_strategy == "call/check":
        for b in btns:
            if "跟注" in b or "call" in b.lower():
                chosen = b
                break
    if not chosen:
        # pick first legal action
        for b in btns:
            if any(k in b for k in ["跟注", "下注", "让牌", "全押"]):
                chosen = b
                break
    if not chosen:
        for b in btns:
            if any(k in b for k in ["弃牌", "check", "call"]):
                chosen = b
                break
    
    if chosen:
        print(f"  {account} choosing: {chosen}")
        dbg_eval(page_id, f"Array.from(document.querySelectorAll('button')).find(b=>b.innerText.trim()==={json.dumps(chosen)})?.click();'ok'")
        time.sleep(2.5)
        return True
    else:
        print(f"  {account}: no legal action found in {btns}")
        return False

# Play pre-flop through showdown, just doing first legal action each turn
# Pre-flop: test_01 SB acted first, but then test_02 BB also needs to act
# After test_01 calls $100, test_02 should have option to check or raise
for round_n in range(15):
    # Test_02 (BB) acts
    s2 = get_state(PAGES["test_02"])
    if s2["game_over"]:
        print(f"  GAME OVER detected on test_02")
        break
    if s2["is_my_turn"]:
        play_turn("test_02", PAGES["test_02"], "check")
    
    time.sleep(1)
    
    # Test_01 (SB) acts
    s1 = get_state(PAGES["test_01"])
    if s1["game_over"]:
        print(f"  GAME OVER detected on test_01")
        break
    if s1["is_my_turn"]:
        play_turn("test_01", PAGES["test_01"], "check")
    
    time.sleep(1)

# Final state
print("\n=== Final state ===")
for k, v in PAGES.items():
    s = get_state(v)
    print(f"  {k} btns={s['btns']}")
    print(f"  txt: {s['txt']}")
    print()

# Save final screenshots
dbg_screenshot(PAGES["test_01"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/texas_final_01.png")
dbg_screenshot(PAGES["test_02"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/texas_final_02.png")
print(f"\nTexas play done.")