#!/usr/bin/env python3
"""R15 Step 2: Texas Hold'em full game (2 players).
URL: /texasholdem for lobby, /texasholdem/{uuid} for room.
"""
import json
import time
import re
import sys
import os
sys.path.insert(0, "/usr/local/LsmAgentGame/scripts/r15")
from common import dbg_call, dbg_eval, dbg_click, dbg_navigate, dbg_screenshot, dbg_look, dbg_reload

PAGES = {
    "test_01": "p_d55b494a",
    "test_02": "p_3d1b51c0",
}

# Save state
print("=== Pages ===")
for k, v in PAGES.items():
    meta = dbg_look(v, "page_meta")
    print(f"  {k} @ {v}: {meta['data']['data']['url']}")

# Navigate all to texasholdem lobby
print("\n=== Navigate to texasholdem lobby ===")
for k, v in PAGES.items():
    dbg_navigate(v, "https://127.0.0.1:39001/texasholdem")
time.sleep(4)

# Verify lobby
for k, v in PAGES.items():
    url = dbg_look(v, "page_meta")["data"]["data"]["url"]
    print(f"  {k} url={url}")

# test_01 creates room by clicking button with text '+ 创建房间'
print("\n=== test_01 clicks +创建房间 ===")
# Close any open modal first
dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('.modal, [role=dialog]')).forEach(m=>m.remove());'ok'")
time.sleep(0.5)
# Click by text
r = dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('button')).find(b=>b.innerText.includes('创建房间'))?.click();'ok'")
print(f"  click +创建房间: {r}")
time.sleep(2)

# Modal opened
btns = dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('button')).map(b=>b.innerText.trim()).filter(t=>t.length>0)")
print(f"  btns after primary click: {btns}")

# Look for modal content
modal_txt = dbg_eval(PAGES["test_01"], "document.querySelector('.modal')?.innerText || document.querySelector('[role=dialog]')?.innerText || document.querySelector('.dialog')?.innerText || 'NO MODAL'")
print(f"  modal text: {modal_txt[:300]}")

# Click 创建房间 in modal
r = dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('button')).find(b=>b.innerText.trim()==='创建房间')?.click();'ok'")
print(f"  modal 创建房间 click: {r}")
time.sleep(3)

url = dbg_look(PAGES["test_01"], "page_meta")["data"]["data"]["url"]
print(f"  test_01 URL: {url}")

m = re.search(r"/texasholdem/([0-9a-f-]+)", url)
if not m:
    # Maybe still in lobby. Try alternative: click 创建房间 inside any modal
    modal_btns = dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('.modal button, [role=dialog] button')).map(b=>b.innerText.trim())")
    print(f"  modal buttons: {modal_btns}")
    if modal_btns:
        dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('.modal button, [role=dialog] button')).find(b=>b.innerText.includes('创建'))?.click();'ok'")
        time.sleep(3)
        url = dbg_look(PAGES["test_01"], "page_meta")["data"]["data"]["url"]
        print(f"  retry URL: {url}")
        m = re.search(r"/texasholdem/([0-9a-f-]+)", url)

if not m:
    print("FAILED: no room UUID")
    txt = dbg_eval(PAGES["test_01"], "document.body.innerText.substring(0,2000)")
    print(f"  body text: {txt[:500]}")
    sys.exit(1)

ROOM = m.group(1)
print(f"  ROOM UUID: {ROOM}")

# test_02 joins
print("\n=== test_02 joins room ===")
dbg_navigate(PAGES["test_02"], f"https://127.0.0.1:39001/texasholdem/{ROOM}")
time.sleep(4)

# Save screenshots
os.makedirs("/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500", exist_ok=True)
dbg_screenshot(PAGES["test_01"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/texas_01_creator.png")
dbg_screenshot(PAGES["test_02"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/texas_02_joiner.png")

# Print states
for k, v in PAGES.items():
    txt = dbg_eval(v, "document.body.innerText.substring(0,1500)")
    btns = dbg_eval(v, "Array.from(document.querySelectorAll('button')).map(b=>b.innerText.trim()).filter(t=>t.length>0)")
    print(f"\n--- {k} @ {v} ---\n  btns: {btns}\n  txt: {txt[:500]}")

# Save state for next step
with open("/tmp/r15_texas_room.json", "w") as f:
    json.dump({"room": ROOM, "pages": PAGES}, f)
print(f"\nTexas room setup done. Room={ROOM}")