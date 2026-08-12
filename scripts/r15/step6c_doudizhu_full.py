#!/usr/bin/env python3
"""R15 Step 6c: Auto-play doudizhu - first card + 出牌 button."""
import json
import time
import sys
import os
sys.path.insert(0, "/usr/local/LsmWebGame/scripts/r15")
from common import dbg_eval, dbg_call, dbg_click, dbg_look, dbg_screenshot

PAGES = {
    "test_01": "p_d55b494a",
    "test_02": "p_3d1b51c0",
    "test_03": "p_9e6b845c",
}

ROOM = "3c4ff96d-6ea6-4612-be15-db59db41150d"

def click_first_card(pid):
    """Click first card via element.click() - this works because of fiber state propagation."""
    dbg_eval(pid, 'document.querySelectorAll(".doudizhu-hand .card-slot")[0]?.click();"ok"')

def click_btn(pid, text):
    """Click button by exact text."""
    dbg_eval(pid, f'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.trim()==={json.dumps(text)})?.click();"ok"')

def get_state(pid):
    txt = dbg_eval(pid, "document.body.innerText")
    btns = dbg_eval(pid, "Array.from(document.querySelectorAll('button')).map(b=>b.innerText.trim())")
    return {
        "my_turn": "你的回合" in txt,
        "game_over": "胜利" in txt or "🏆" in txt or "失败" in txt,
        "hand": dbg_eval(pid, "document.querySelectorAll('.doudizhu-hand .card-slot').length"),
        "selected": dbg_eval(pid, "document.querySelectorAll('.doudizhu-hand .card-slot.selected-slot').length"),
        "can_pass": "要不起" in btns,
        "btns": btns,
    }

# Save initial screenshot
os.makedirs("/usr/local/LsmWebGame/TestReport/screenshots/20260704_170500", exist_ok=True)
dbg_screenshot(PAGES["test_01"], "/usr/local/LsmWebGame/TestReport/screenshots/20260704_170500/ddz_autoplay_01.png")

print("=== Auto-playing doudizhu ===")
for move_n in range(60):
    cur = None
    cur_pid = None
    cur_state = None
    
    for k, v in PAGES.items():
        s = get_state(v)
        if s["game_over"]:
            print(f"\nGAME OVER on {k}!")
            cur = None
            break
        if s["my_turn"]:
            cur = k
            cur_pid = v
            cur_state = s
            break
    
    if not cur:
        time.sleep(2)
        # check game over
        any_over = False
        winner = None
        for k, v in PAGES.items():
            t = dbg_eval(v, "document.body.innerText")
            if "胜利" in t or "🏆" in t:
                any_over = True
                winner = k
                break
            if "失败" in t:
                any_over = True
                break
        if any_over:
            print(f"  GAME OVER confirmed, winner: {winner}")
            break
        continue
    
    hand = cur_state["hand"]
    if hand == 0:
        time.sleep(2)
        continue
    
    # Click first card
    click_first_card(cur_pid)
    time.sleep(0.3)
    # Click 出牌 via element.click()
    js = 'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.trim()==="出牌")?.click();"ok"'
    dbg_eval(cur_pid, js)
    time.sleep(1.5)
    print(f"  move {move_n+1}: {cur} (hand was {hand})")

# Final state
print("\n=== Final state ===")
for k, v in PAGES.items():
    s = get_state(v)
    txt = dbg_eval(v, "document.body.innerText.substring(0,500)")
    print(f"\n--- {k} hand={s['hand']} game_over={s['game_over']} ---")
    print(txt[:400])

# Save final screenshots
dbg_screenshot(PAGES["test_01"], "/usr/local/LsmWebGame/TestReport/screenshots/20260704_170500/ddz_autoplay_final_01.png")
dbg_screenshot(PAGES["test_02"], "/usr/local/LsmWebGame/TestReport/screenshots/20260704_170500/ddz_autoplay_final_02.png")
dbg_screenshot(PAGES["test_03"], "/usr/local/LsmWebGame/TestReport/screenshots/20260704_170500/ddz_autoplay_final_03.png")

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

# Test style switching (都市打工仔)
print("\n=== test_01 switches style to 都市打工仔 ===")
dbg_eval(PAGES["test_01"], 'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.includes("都市打工仔"))?.click();"ok"')
time.sleep(2)

# All leave
print("\n=== All leave ===")
for k, v in PAGES.items():
    dbg_eval(v, 'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.includes("离开观战"))?.click();"ok"')
    time.sleep(1.5)
    dbg_eval(v, 'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.trim()==="确认")?.click();"ok"')
    time.sleep(2)
    url = dbg_look(v, "page_meta")["data"]["data"]["url"]
    print(f"  {k} URL: {url}")

print(f"\nDoudizhu auto-play done. Room={ROOM}")