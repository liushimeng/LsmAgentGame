#!/usr/bin/env python3
"""R15 Step 5: International Chess actual moves."""
import json
import time
import sys
import os
sys.path.insert(0, "/usr/local/LsmWebGame/scripts/r15")
from common import dbg_eval, dbg_call, dbg_click, dbg_navigate, dbg_screenshot, dbg_look

PAGES = {
    "test_01": "p_d55b494a",  # White
    "test_02": "p_3d1b51c0",  # Black
}

ROOM = "bb8a1145-3bbd-4c93-9ad4-68133e40dc93"

# Chess board position
BOARD_X = 542
BOARD_Y = 96
PADDING = 36
CELL = 64

def cell_px(col, row, my_color):
    """Convert (col, row) board coordinates to screen pixel coords.
    col: 0-7 (a-h, where 0=a)
    row: 0-7 (rank 1-8, where 0=rank 1)
    For white: rank 1 at bottom (visual row 0 = rank 1 = board row 0)
    For black: rank 1 at top (visual row 0 = rank 8 = board row 7)
    """
    # Visual row = isFlipped ? 7 - row : row
    if my_color == 'white':
        visual_row = row
    else:  # black
        visual_row = 7 - row
    visual_col = col
    x = BOARD_X + PADDING + visual_col * CELL + CELL // 2
    y = BOARD_Y + PADDING + visual_row * CELL + CELL // 2
    return x, y

def click_cell(page_id, col, row, my_color):
    """Click cell at (col, row) using CDP Input.dispatchMouseEvent."""
    x, y = cell_px(col, row, my_color)
    # Use input_text or eval_js to dispatch a click event at (x, y)
    js = f"""
    (() => {{
      const evt = new MouseEvent('click', {{
        bubbles: true, cancelable: true, view: window,
        clientX: {x}, clientY: {y}
      }});
      const boards = document.querySelectorAll('div');
      for (const b of boards) {{
        const s = b.getAttribute('style') || '';
        if (s.includes('position: relative') && s.includes('width: 584px')) {{
          b.dispatchEvent(evt);
          return 'clicked ' + {col} + ',' + {row};
        }}
      }}
      return 'no board';
    }})()
    """
    return dbg_eval(page_id, js)

def get_state(page_id):
    txt = dbg_eval(page_id, "document.body.innerText")
    return {
        "txt": txt[:500],
        "my_turn": "轮到你走子" in txt,
        "opp_turn": "对手走子中" in txt,
        "btns": dbg_eval(page_id, "Array.from(document.querySelectorAll('button')).map(b=>b.innerText.trim()).filter(t=>t.length>0)"),
        "round": dbg_eval(page_id, "document.body.innerText.match(/回合\\s*(\\d+)/)?.[1] || '0'"),
    }

# Initial state
print("=== Initial state ===")
for k, v in PAGES.items():
    s = get_state(v)
    print(f"  {k}: turn={'MINE' if s['my_turn'] else 'OPP' if s['opp_turn'] else '?'} round={s['round']}")

# Move 1: White e2->e4 (col=4, row=1 -> col=4, row=3)
# Algebraic: e2=(col=4,row=1) e4=(col=4,row=3)
print("\n=== Move 1: White e2->e4 ===")
r = click_cell(PAGES["test_01"], 4, 1, "white")
print(f"  select e2: {r}")
time.sleep(0.8)
r = click_cell(PAGES["test_01"], 4, 3, "white")
print(f"  move to e4: {r}")
time.sleep(2)
s = get_state(PAGES["test_01"])
print(f"  test_01 after e4: round={s['round']} turn={'MINE' if s['my_turn'] else 'OPP' if s['opp_turn'] else '?'}")

# Move 2: Black e7->e5 (col=4,row=6 -> col=4,row=4)
print("\n=== Move 2: Black e7->e5 ===")
r = click_cell(PAGES["test_02"], 4, 6, "black")
print(f"  select e7: {r}")
time.sleep(0.8)
r = click_cell(PAGES["test_02"], 4, 4, "black")
print(f"  move to e5: {r}")
time.sleep(2)
s = get_state(PAGES["test_02"])
print(f"  test_02 after e5: round={s['round']} turn={'MINE' if s['my_turn'] else 'OPP' if s['opp_turn'] else '?'}")

# Move 3: White d1->h5 (Queen diagonal, col=3,row=0 -> col=7,row=2)
print("\n=== Move 3: White Qd1->h5 ===")
r = click_cell(PAGES["test_01"], 3, 0, "white")
print(f"  select d1: {r}")
time.sleep(0.8)
r = click_cell(PAGES["test_01"], 7, 2, "white")
print(f"  move to h5: {r}")
time.sleep(2)
s = get_state(PAGES["test_01"])
print(f"  test_01: round={s['round']} turn={'MINE' if s['my_turn'] else 'OPP' if s['opp_turn'] else '?'}")

# Move 4: Black f7->f6 (try to block check threat, but no check yet. Just f7-f6 normal pawn)
print("\n=== Move 4: Black f7->f6 ===")
r = click_cell(PAGES["test_02"], 5, 6, "black")
print(f"  select f7: {r}")
time.sleep(0.8)
r = click_cell(PAGES["test_02"], 5, 5, "black")
print(f"  move to f6: {r}")
time.sleep(2)

# Move 5: White h5->e5 (Queen takes e5 pawn - capture!)
# But wait, after f7-f6 there's nothing on e5 (it was moved away). Let's do Bc1-f4
print("\n=== Move 5: White Bc1-f4 (col=2,row=0 -> col=5,row=2) ===")
r = click_cell(PAGES["test_01"], 2, 0, "white")
print(f"  select c1: {r}")
time.sleep(0.8)
r = click_cell(PAGES["test_01"], 5, 2, "white")
print(f"  move to f4: {r}")
time.sleep(2)

# Save screenshots
os.makedirs("/usr/local/LsmWebGame/TestReport/screenshots/20260704_170500", exist_ok=True)
dbg_screenshot(PAGES["test_01"], "/usr/local/LsmWebGame/TestReport/screenshots/20260704_170500/chess_after_moves.png")
dbg_screenshot(PAGES["test_02"], "/usr/local/LsmWebGame/TestReport/screenshots/20260704_170500/chess_after_moves_black.png")

print("\n=== State after 5 moves ===")
for k, v in PAGES.items():
    s = get_state(v)
    print(f"  {k} round={s['round']} turn={'MINE' if s['my_turn'] else 'OPP' if s['opp_turn'] else '?'}")
    print(f"  txt: {s['txt'][:200]}...")

# Send a chat message from test_01
print("\n=== test_01 sends chat ===")
dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('input[placeholder*=说点什么]')).forEach(i=>{i.value=''});'ok'")
time.sleep(0.3)
dbg_call("input_text", PAGES["test_01"], {
    "selector": "input[placeholder*='说点什么']",
    "text": "AutoTest-R15 chess chat",
    "clear": True,
})
time.sleep(0.5)
dbg_eval(PAGES["test_01"], "Array.from(document.querySelectorAll('button')).find(b=>b.innerText.trim()==='发送')?.click();'ok'")
time.sleep(2)
for k, v in PAGES.items():
    has = "AutoTest-R15 chess chat" in (dbg_eval(v, "document.body.innerText") or "")
    print(f"  {k} chat contains: {has}")

# Resign
print("\n=== test_02 resigns ===")
r = dbg_eval(PAGES["test_02"], "Array.from(document.querySelectorAll('button')).find(b=>b.innerText.includes('认输'))?.click();'ok'")
print(f"  click 认输: {r}")
time.sleep(2)
# Confirm modal
btns = dbg_eval(PAGES["test_02"], "Array.from(document.querySelectorAll('button')).map(b=>b.innerText.trim()).filter(t=>t.length>0)")
print(f"  btns after resign: {btns}")
# Look for confirm button
dbg_eval(PAGES["test_02"], "Array.from(document.querySelectorAll('.modal button, [role=dialog] button')).find(b=>b.innerText.includes('确认'))?.click();'ok'")
time.sleep(3)

# Final state
for k, v in PAGES.items():
    txt = dbg_eval(v, "document.body.innerText")
    print(f"\n--- {k} final ---")
    print(txt[:500])

# Save final screenshots
dbg_screenshot(PAGES["test_01"], "/usr/local/LsmWebGame/TestReport/screenshots/20260704_170500/chess_final_01.png")
dbg_screenshot(PAGES["test_02"], "/usr/local/LsmWebGame/TestReport/screenshots/20260704_170500/chess_final_02.png")

# Both leave
print("\n=== Both leave ===")
for k, v in PAGES.items():
    dbg_eval(v, "Array.from(document.querySelectorAll('button')).find(b=>b.innerText.includes('离开观战'))?.click();'ok'")
    time.sleep(1.5)
    dbg_eval(v, "Array.from(document.querySelectorAll('button')).find(b=>b.innerText.includes('确认'))?.click();'ok'")
    time.sleep(2)
    url = dbg_look(v, "page_meta")["data"]["data"]["url"]
    print(f"  {k} URL: {url}")

print("\n=== Chess full test done ===")