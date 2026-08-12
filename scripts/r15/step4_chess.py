#!/usr/bin/env python3
"""R15 Step 4: International Chess actual game (2 players).
URL: /chess for lobby.
"""
import json
import time
import re
import sys
import os
sys.path.insert(0, "/usr/local/LsmAgentGame/scripts/r15")
from common import dbg_eval, dbg_call, dbg_click, dbg_navigate, dbg_screenshot, dbg_look

PAGES = {
    "test_01": "p_d55b494a",
    "test_02": "p_3d1b51c0",
}

# Navigate all to chess lobby
print("=== Navigate to chess lobby ===")
for k, v in PAGES.items():
    dbg_navigate(v, "https://127.0.0.1:39001/chess")
time.sleep(3)

for k, v in PAGES.items():
    url = dbg_look(v, "page_meta")["data"]["data"]["url"]
    btns = dbg_eval(v, "Array.from(document.querySelectorAll('button')).map(b=>b.innerText.trim()).filter(t=>t.length>0)")
    print(f"  {k} url={url} btns={btns}")

# test_01 creates chess room
print("\n=== test_01 clicks +创建房间 ===")
dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('.modal, [role=dialog]')).forEach(m=>m.remove());'ok'")
time.sleep(0.5)
dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('button')).find(b=>b.innerText.includes('创建房间'))?.click();'ok'")
time.sleep(2)
dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('button')).find(b=>b.innerText.trim()==='创建房间' && b.closest('.modal,[role=dialog]'))?.click();'ok'")
time.sleep(3)

url = dbg_look(PAGES["test_01"], "page_meta")["data"]["data"]["url"]
print(f"  test_01 URL: {url}")
m = re.search(r"/chess/([0-9a-f-]+)", url)
if not m:
    # Try without filter
    dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('button')).find(b=>b.innerText.trim()==='创建房间')?.click();'ok'")
    time.sleep(3)
    url = dbg_look(PAGES["test_01"], "page_meta")["data"]["data"]["url"]
    print(f"  test_01 URL retry: {url}")
    m = re.search(r"/chess/([0-9a-f-]+)", url)

if not m:
    txt = dbg_eval(PAGES["test_01"], "document.body.innerText.substring(0,1500)")
    print(f"  body: {txt[:500]}")
    print("FAILED: no chess room")
    sys.exit(1)
ROOM = m.group(1)
print(f"  ROOM: {ROOM}")

# test_02 joins
dbg_navigate(PAGES["test_02"], f"https://127.0.0.1:39001/chess/{ROOM}")
time.sleep(4)

os.makedirs("/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500", exist_ok=True)
dbg_screenshot(PAGES["test_01"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/chess_01_start.png")
dbg_screenshot(PAGES["test_02"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/chess_02_join.png")

for k, v in PAGES.items():
    txt = dbg_eval(v, "document.body.innerText.substring(0,800)")
    print(f"\n--- {k} ---\n{txt[:500]}")

with open("/tmp/r15_chess_room.json", "w") as f:
    json.dump({"room": ROOM, "pages": PAGES}, f)
print(f"\nChess room setup done. Room={ROOM}")