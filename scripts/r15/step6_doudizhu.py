#!/usr/bin/env python3
"""R15 Step 6: Doudizhu actual game (3 players)."""
import json
import time
import re
import sys
import os
sys.path.insert(0, "/usr/local/LsmAgentGame/scripts/r15")
from common import dbg_eval, dbg_call, dbg_click, dbg_navigate, dbg_screenshot, dbg_look

# test_01 (p_d55b494a), test_02 (p_3d1b51c0), test_03 (p_3026edca)
PAGES = {
    "test_01": "p_d55b494a",
    "test_02": "p_3d1b51c0",
    "test_03": "p_9e6b845c",
}

# Navigate all to doudizhu lobby
print("=== Navigate to doudizhu lobby ===")
for k, v in PAGES.items():
    dbg_navigate(v, "https://127.0.0.1:39001/doudizhu")
    time.sleep(2)
time.sleep(3)

for k, v in PAGES.items():
    info = dbg_look(v, "page_meta")
    url = info.get("data", {}).get("data", {}).get("url", "?") if info else "?"
    print(f"  {k} url={url}")

# test_01 creates doudizhu room
print("\n=== test_01 creates doudizhu room ===")
dbg_eval(PAGES["test_01"], 'Array.from(document.querySelectorAll(".modal, [role=dialog]")).forEach(m=>m.remove());"ok"')
time.sleep(0.5)
dbg_eval(PAGES["test_01"], 'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.includes("创建房间"))?.click();"ok"')
time.sleep(2)
dbg_eval(PAGES["test_01"], 'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.trim()==="创建房间")?.click();"ok"')
time.sleep(3)
url = dbg_look(PAGES["test_01"], "page_meta")["data"]["data"]["url"]
print(f"  test_01 URL: {url}")
m = re.search(r"/doudizhu/([0-9a-f-]+)", url)
if not m:
    txt = dbg_eval(PAGES["test_01"], "document.body.innerText.substring(0,1000)")
    print(f"  body: {txt[:500]}")
    print("FAILED: no doudizhu room")
    sys.exit(1)
ROOM = m.group(1)
print(f"  ROOM: {ROOM}")

# test_02, test_03 join
print("\n=== test_02/test_03 join ===")
dbg_navigate(PAGES["test_02"], f"https://127.0.0.1:39001/doudizhu/{ROOM}")
time.sleep(2)
dbg_navigate(PAGES["test_03"], f"https://127.0.0.1:39001/doudizhu/{ROOM}")
time.sleep(4)

os.makedirs("/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500", exist_ok=True)
dbg_screenshot(PAGES["test_01"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/ddz_start_01.png")
dbg_screenshot(PAGES["test_02"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/ddz_start_02.png")
dbg_screenshot(PAGES["test_03"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/ddz_start_03.png")

# State check
for k, v in PAGES.items():
    txt = dbg_eval(v, "document.body.innerText.substring(0,1500)")
    btns = dbg_eval(v, "Array.from(document.querySelectorAll('button')).map(b=>b.innerText.trim()).filter(t=>t.length>0)")
    hand_size = dbg_eval(v, "document.querySelectorAll('.doudizhu-hand .card-slot').length")
    print(f"\n--- {k} hand={hand_size} btns={btns}")
    print(f"  txt: {txt[:300]}")

# Bidding phase: typically all 3 pass, then game starts
# Actually doudizhu requires landlord. Let me check bidding
print("\n=== Bidding ===")
def get_bid_state(pid):
    txt = dbg_eval(pid, "document.body.innerText")
    btns = dbg_eval(pid, "Array.from(document.querySelectorAll('button')).map(b=>b.innerText.trim())")
    return {
        "bidding": "叫地主" in txt and ("不叫" in btns or "1 分" in btns),
        "my_bid": "不叫" in btns and any("分" in b for b in btns),
        "playing": "你的回合" in txt or "出牌" in btns,
        "waiting": "等待" in txt or "对手回合" in txt,
        "btns": btns,
        "hand": dbg_eval(pid, "document.querySelectorAll('.doudizhu-hand .card-slot').length"),
        "landlord": "👑" in txt or "地主" in txt,
    }

# Wait for bidding
for round_n in range(5):
    print(f"\n--- Bidding round {round_n+1} ---")
    bid_done = True
    for k, v in PAGES.items():
        s = get_bid_state(v)
        if s["bidding"]:
            bid_done = False
            print(f"  {k}: bidding active, btns={s['btns']}")
            # All pass to let system pick random landlord
            # Or first one call 1分
            if round_n == 0:
                # have test_01 call 3分 (max)
                # Actually let me have test_03 call 3分 at first opportunity
                if k == "test_03":
                    dbg_eval(v, 'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.includes("3 分"))?.click();"ok"')
                    print(f"    {k}: called 3分")
                else:
                    dbg_eval(v, 'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.trim()==="不叫")?.click();"ok"')
                    print(f"    {k}: pass")
                time.sleep(1.5)
            else:
                # if bidding still active, all pass
                dbg_eval(v, 'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.trim()==="不叫")?.click();"ok"')
                print(f"    {k}: pass")
                time.sleep(1.5)
    time.sleep(2)
    if bid_done:
        print("  Bidding appears done")
        break

# Check who's landlord
for k, v in PAGES.items():
    s = get_bid_state(v)
    print(f"  {k} after bid: hand={s['hand']} landlord={s['landlord']} btns={s['btns']}")

# Play phase: have all 3 play lowest cards
def get_play_state(pid):
    txt = dbg_eval(pid, "document.body.innerText")
    btns = dbg_eval(pid, "Array.from(document.querySelectorAll('button')).map(b=>b.innerText.trim())")
    return {
        "my_turn": "你的回合" in txt or "出牌" in btns,
        "btns": btns,
        "hand": dbg_eval(pid, "document.querySelectorAll('.doudizhu-hand .card-slot').length"),
        "game_over": "胜利" in txt or "🏆" in txt or "失败" in txt or "游戏结束" in txt,
    }

print("\n=== Playing doudizhu ===")
for move_n in range(60):
    # Find whose turn
    cur_player = None
    for k, v in PAGES.items():
        s = get_play_state(v)
        if s["game_over"]:
            print(f"\nGAME OVER detected on {k}")
            break
        if s["my_turn"]:
            cur_player = k
            cur_pid = v
            cur_btns = s["btns"]
            break
    
    if not cur_player:
        # Check game over
        any_over = False
        for k, v in PAGES.items():
            s = get_play_state(v)
            if s["game_over"]:
                any_over = True
                print(f"\nGAME OVER on {k}")
                break
        if any_over:
            break
        time.sleep(1.5)
        continue
    
    # Play lowest single
    hand_count = dbg_eval(cur_pid, "document.querySelectorAll('.doudizhu-hand .card-slot').length")
    if hand_count == 0:
        print(f"  {cur_player}: hand=0, game should be over")
        time.sleep(2)
        continue
    
    # Click first card
    dbg_eval(cur_pid, 'document.querySelectorAll(".doudizhu-hand .card-slot")[0]?.click();"ok"')
    time.sleep(0.3)
    # Click 出牌 button
    dbg_eval(cur_pid, 'Array.from(document.querySelectorAll("button")).find(b=>b.innerText.trim()==="出牌")?.click();"ok"')
    time.sleep(1.5)
    print(f"  move {move_n+1}: {cur_player} played")

# Final state
print("\n=== Final state ===")
for k, v in PAGES.items():
    s = get_play_state(v)
    txt = dbg_eval(v, "document.body.innerText.substring(0,800)")
    print(f"\n--- {k} hand={s['hand']} game_over={s['game_over']} ---")
    print(txt[:500])

# Save final screenshots
dbg_screenshot(PAGES["test_01"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/ddz_final_01.png")
dbg_screenshot(PAGES["test_02"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/ddz_final_02.png")
dbg_screenshot(PAGES["test_03"], "/usr/local/LsmAgentGame/TestReport/screenshots/20260704_170500/ddz_final_03.png")

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

with open("/tmp/r15_doudizhu_room.json", "w") as f:
    json.dump({"room": ROOM, "pages": PAGES}, f)
print(f"\nDoudizhu full test done. Room={ROOM}")