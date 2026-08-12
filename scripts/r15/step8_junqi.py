#!/usr/bin/env python3
"""R15 Step 8: Chinese Junqi (军棋) actual game."""
import json
import time
import re
import sys
import os
sys.path.insert(0, "/usr/local/LsmWebGame/scripts/r15")
from common import dbg_eval, dbg_call, dbg_click, dbg_navigate, dbg_screenshot, dbg_look

# test_01 = Red, test_02 = Black
PAGES = {
    "test_01": "p_d55b494a",
    "test_02": "p_e0760f72",
}

print("=== Navigate to junqi lobby ===")
for k, v in PAGES.items():
    dbg_navigate(v, "https://127.0.0.1:39001/junqi")
time.sleep(3)

for k, v in PAGES.items():
    url = dbg_look(v, "page_meta")["data"]["data"]["url"]
    btns = dbg_eval(v, "Array.from(document.querySelectorAll('button')).map(b=>b.innerText.trim()).filter(t=>t.length>0)")
    print(f"  {k} url={url} btns={btns}")

# test_01 creates junqi room
print("\n=== test_01 creates junqi room ===")
dbg_eval(PAGES["test_01"], 'Array.from(document.querySelectorAll(".modal, [role=dialog]")).forEach(m=>m.remove());"ok"')
time.sleep(0.5)
dbg_eval(PAGES["test_01"], 'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.includes("创建房间"))?.click();"ok"')
time.sleep(2)
dbg_eval(PAGES["test_01"], 'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.trim()==="创建房间")?.click();"ok"')
time.sleep(3)
url = dbg_look(PAGES["test_01"], "page_meta")["data"]["data"]["url"]
print(f"  test_01 URL: {url}")
m = re.search(r"/junqi/([0-9a-f-]+)", url)
if not m:
    txt = dbg_eval(PAGES["test_01"], "document.body.innerText.substring(0,1000)")
    print(f"  body: {txt[:500]}")
    print("FAILED: no junqi room")
    sys.exit(1)
ROOM = m.group(1)
print(f"  ROOM: {ROOM}")

# test_02 joins
dbg_navigate(PAGES["test_02"], f"https://127.0.0.1:39001/junqi/{ROOM}")
time.sleep(4)

# State check
for k, v in PAGES.items():
    txt = dbg_eval(v, "document.body.innerText.substring(0,1500)")
    btns = dbg_eval(v, "Array.from(document.querySelectorAll('button')).map(b=>b.innerText.trim()).filter(t=>t.length>0)")
    print(f"\n--- {k} ---")
    print(f"  btns: {btns}")
    print(f"  txt: {txt[:400]}")

os.makedirs("/usr/local/LsmWebGame/TestReport/screenshots/20260704_170500", exist_ok=True)
dbg_screenshot(PAGES["test_01"], "/usr/local/LsmWebGame/TestReport/screenshots/20260704_170500/junqi_01.png")
dbg_screenshot(PAGES["test_02"], "/usr/local/LsmWebGame/TestReport/screenshots/20260704_170500/junqi_02.png")

# Check layout mode (need to place pieces)
# Look for board cells
js = '''
(() => {
  const boards = document.querySelectorAll('div');
  for (const b of boards) {
    const s = b.getAttribute('style') || '';
    if (s.includes('position: relative') && (s.includes('width: 552px') || s.includes('width: 584px'))) {
      const r = b.getBoundingClientRect();
      return JSON.stringify({left: r.left, top: r.top, w: r.width, h: r.height});
    }
  }
  return 'no board';
})()
'''
for k, v in PAGES.items():
    print(f'  {k} board:', dbg_eval(v, js))

# Auto-place pieces via WS - simpler approach
# Look at junqi.store for layout requirements
import subprocess

# Save room info
with open("/tmp/r15_junqi_room.json", "w") as f:
    json.dump({"room": ROOM, "pages": PAGES}, f)

print(f"\nJunqi room setup done. Room={ROOM}")
print("Note: Layout placement requires complex UI interaction - manual setup recommended")