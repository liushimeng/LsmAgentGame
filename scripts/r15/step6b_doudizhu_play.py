#!/usr/bin/env python3
"""R15 Step 6b: Play doudizhu game that's already started."""
import json
import time
import sys
import os
sys.path.insert(0, "/usr/local/LsmAgentGame/scripts/r15")
from common import dbg_eval, dbg_call, dbg_click, dbg_look, dbg_screenshot

PAGES = {
    "test_01": "p_d55b494a",  # 座位1 (农民), 17 cards
    "test_02": "p_3d1b51c0",  # c6c648 (农民), 17 cards
    "test_03": "p_9e6b845c",  # 48ffe9 👑 (地主), 19 cards
}

ROOM = "3c4ff96d-6ea6-4612-be15-db59db41150d"

def get_state(pid):
    txt = dbg_eval(pid, "document.body.innerText")
    btns = dbg_eval(pid, "Array.from(document.querySelectorAll('button')).map(b=>b.innerText.trim())")
    return {
        "my_turn": "你的回合" in txt,
        "opp_turn": "对手回合" in txt,
        "landlord": "👑" in txt,
        "game_over": "胜利" in txt or "🏆" in txt or "失败" in txt or "游戏结束" in txt,
        "winner": "胜利" in txt or "🏆" in txt,
        "hand": dbg_eval(pid, "document.querySelectorAll('.doudizhu-hand .card-slot').length"),
        "btns": btns,
    }

def play_lowest(pid):
    """Click first card then 出牌 button."""
    dbg_eval(pid, 'document.querySelectorAll(".doudizhu-hand .card-slot")[0]?.click();"ok"')
    time.sleep(0.3)
    dbg_eval(pid, 'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.trim()==="出牌")?.click();"ok"')
    time.sleep(1.5)

def pass_play(pid):
    dbg_eval(pid, 'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.trim()==="要不起")?.click();"ok"')
    time.sleep(1.5)

# Initial state
print("=== Initial state ===")
for k, v in PAGES.items():
    s = get_state(v)
    print(f"  {k}: turn={'MINE' if s['my_turn'] else 'OPP' if s['opp_turn'] else '?'} hand={s['hand']} landlord={s['landlord']} btns={[b for b in s['btns'] if any(k in b for k in ['出牌','要不起','不叫','分'])]}")

# Save starting screenshot
os.makedirs("/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500", exist_ok=True)
dbg_screenshot(PAGES["test_01"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/ddz_play_01_start.png")

# Play loop: 3 players take turns
print("\n=== Playing ===")
for move_n in range(80):
    cur_player = None
    cur_pid = None
    for k, v in PAGES.items():
        s = get_state(v)
        if s["game_over"]:
            print(f"\nGAME OVER detected on {k}: btns={s['btns']}")
            cur_player = None
            break
        if s["my_turn"]:
            cur_player = k
            cur_pid = v
            break
    
    if not cur_player:
        time.sleep(2)
        # Check again
        any_over = False
        for k, v in PAGES.items():
            s = get_state(v)
            if s["game_over"]:
                any_over = True
                print(f"  GAME OVER confirmed on {k}")
                break
        if any_over:
            break
        continue
    
    hand = get_state(cur_pid)["hand"]
    if hand == 0:
        print(f"  {cur_player}: hand=0, game should be over")
        time.sleep(2)
        continue
    
    play_lowest(cur_pid)
    print(f"  move {move_n+1}: {cur_player} (hand was {hand})")

# Final state
print("\n=== Final state ===")
for k, v in PAGES.items():
    s = get_state(v)
    txt = dbg_eval(v, "document.body.innerText.substring(0,600)")
    print(f"\n--- {k} hand={s['hand']} game_over={s['game_over']} ---")
    print(txt[:400])

# Save final screenshots
dbg_screenshot(PAGES["test_01"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/ddz_play_final_01.png")
dbg_screenshot(PAGES["test_02"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/ddz_play_final_02.png")
dbg_screenshot(PAGES["test_03"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/ddz_play_final_03.png")

# Send chat from test_01
print("\n=== test_01 sends chat ===")
dbg_eval(PAGES["test_01"], 'Array.from(document.querySelectorAll("input[placeholder*=说点什么]")).forEach(i=>{i.value=""});"ok"')
time.sleep(0.3)
dbg_call("input_text", PAGES["test_01"], {
    "selector": "input[placeholder*='说点什么']",
    "text": "AutoTest-R15 doudizhu chat",
    "clear": True,
})
time.sleep(0.5)
dbg_eval(PAGES["test_01"], 'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.trim()==="发送")?.click();"ok"')
time.sleep(2)
for k, v in PAGES.items():
    has = "AutoTest-R15 doudizhu chat" in (dbg_eval(v, "document.body.innerText") or "")
    print(f"  {k} chat contains: {has}")

# All leave
print("\n=== All leave ===")
for k, v in PAGES.items():
    dbg_eval(v, 'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.includes("离开观战"))?.click();"ok"')
    time.sleep(1.5)
    dbg_eval(v, 'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.trim()==="确认")?.click();"ok"')
    time.sleep(2)
    url = dbg_look(v, "page_meta")["data"]["data"]["url"]
    print(f"  {k} URL: {url}")

print(f"\nDoudizhu play done. Room={ROOM}")