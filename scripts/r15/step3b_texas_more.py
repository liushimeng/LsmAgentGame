#!/usr/bin/env python3
"""R15 Step 3b: Texas Hold'em - test style switch, chat, then exit."""
import json
import time
import sys
import os
sys.path.insert(0, "/usr/local/LsmAgentGame/scripts/r15")
from common import dbg_call, dbg_eval, dbg_click, dbg_look, dbg_screenshot

PAGES = {
    "test_01": "p_d55b494a",
    "test_02": "p_3d1b51c0",
}

ROOM = "fa041d6b-13a7-4a31-a9dd-76cec6307a5a"

def get_state(page_id):
    txt = dbg_eval(page_id, "document.body.innerText")
    return {
        "txt": txt[:500],
        "is_my_turn": "你的回合" in txt or "your turn" in txt.lower(),
        "is_opp_turn": "对手回合" in txt or "opponent turn" in txt.lower(),
        "game_over": "游戏结束" in txt or "🏆" in txt or "胜利" in txt or "失败" in txt or "胜者" in txt,
        "btns": dbg_eval(page_id, "Array.from(document.querySelectorAll('button')).map(b=>b.innerText.trim()).filter(t=>t.length>0)"),
    }

# Step A: Test style switching (test_01 clicks 荒野逃生)
print("=== test_01 switches style to 荒野逃生 ===")
r = dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('button')).find(b=>b.innerText.includes('荒野逃生'))?.click();'ok'")
print(f"  click: {r}")
time.sleep(2)
dbg_screenshot(PAGES["test_01"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/texas_style_switch.png")

# Step B: Send chat from test_01
print("\n=== test_01 sends chat message ===")
dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('input[placeholder*=说点什么]')).forEach(i=>{i.value=''; i.dispatchEvent(new Event('input', {bubbles:true}))});'ok'")
time.sleep(0.3)
r = dbg_call("input_text", PAGES["test_01"], {
    "selector": "input[placeholder*='说点什么']",
    "text": "AutoTest-R15 texas chat test",
    "clear": True,
})
print(f"  input: {r}")
time.sleep(0.5)
r = dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('button')).find(b=>b.innerText.trim()==='发送')?.click();'ok'")
print(f"  send: {r}")
time.sleep(2)

# Verify chat
for k, v in PAGES.items():
    txt = dbg_eval(v, "document.body.innerText")
    has = "AutoTest-R15 texas chat test" in txt
    print(f"  {k} chat contains message: {has}")

dbg_screenshot(PAGES["test_01"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/texas_chat.png")

# Step C: Test 规则说明 modal
print("\n=== test_01 clicks 规则说明 ===")
r = dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('button')).find(b=>b.innerText.trim()==='规则说明')?.click();'ok'")
print(f"  click: {r}")
time.sleep(2)
modal_txt = dbg_eval(PAGES["test_01"], "document.querySelector('.modal')?.innerText || document.body.innerText.substring(0,500)")
print(f"  modal content: {modal_txt[:300]}")
dbg_screenshot(PAGES["test_01"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/texas_rules.png")

# Close modal (Esc)
r = dbg_call("key_press", PAGES["test_01"], {"key": "Escape"})
print(f"  Escape: {r}")
time.sleep(1)

# Step D: Both exit room
print("\n=== test_01 leaves room ===")
r = dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('button')).find(b=>b.innerText.trim()==='离开观战')?.click();'ok'")
print(f"  click: {r}")
time.sleep(2)
# Confirm modal
r = dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('button')).find(b=>b.innerText.includes('确认'))?.click();'ok'")
print(f"  confirm: {r}")
time.sleep(3)

url_01 = dbg_look(PAGES["test_01"], "page_meta")["data"]["data"]["url"]
print(f"  test_01 URL after leave: {url_01}")

# test_02 also leaves
r = dbg_eval(PAGES["test_02"], "Array.from(document.querySelectorAll('button')).find(b=>b.innerText.trim()==='离开观战')?.click();'ok'")
print(f"  test_02 click leave: {r}")
time.sleep(2)
r = dbg_eval(PAGES["test_02"], "Array.from(document.querySelectorAll('button')).find(b=>b.innerText.includes('确认'))?.click();'ok'")
print(f"  test_02 confirm: {r}")
time.sleep(3)

url_02 = dbg_look(PAGES["test_02"], "page_meta")["data"]["data"]["url"]
print(f"  test_02 URL after leave: {url_02}")

# Save final state
dbg_screenshot(PAGES["test_01"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/texas_after_leave_01.png")
dbg_screenshot(PAGES["test_02"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/texas_after_leave_02.png")

print("\n=== Texas full test done ===")