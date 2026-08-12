#!/usr/bin/env python3
"""Finish doudizhu game using element.click() properly."""
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
        resp_json = json.loads(resp.read())
    if "data" in resp_json and "result" in resp_json["data"]:
        return resp_json["data"]["result"]
    return resp_json

PAGES = {
    "test_03": "p_d284a3b9",  # landlord
    "test_01": "p_63ad26f4",  # peasant
    "test_02": "p_c1133976",  # peasant
}

def whose_turn():
    for a, p in PAGES.items():
        r = debug_eval(p, 'document.body.innerText.includes("你的回合")')
        if r is True:
            return a, p
    return None, None

def get_hand_size(p):
    return debug_eval(p, "document.querySelectorAll('.doudizhu-hand .card-slot').length") or 0

def get_cards(p):
    r = debug_eval(p, 'JSON.stringify(Array.from(document.querySelectorAll(".doudizhu-hand .card-slot")).map(s=>s.textContent.trim()))')
    return json.loads(r) if r else []

def play_lowest_single(p):
    """Click first card, then 出牌."""
    debug_eval(p, 'document.querySelectorAll(".doudizhu-hand .card-slot")[0]?.click();"ok"')
    time.sleep(0.2)
    debug_eval(p, 'Array.from(document.querySelectorAll("button")).filter(b=>b.innerText.trim()==="出牌")[0]?.click();"ok"')
    time.sleep(0.3)

def play_pair(p, n=2):
    """Click first n cards, then 出牌."""
    for i in range(n):
        debug_eval(p, f'document.querySelectorAll(".doudizhu-hand .card-slot")[{i}]?.click();"ok"')
        time.sleep(0.1)
    debug_eval(p, 'Array.from(document.querySelectorAll("button")).filter(b=>b.innerText.trim()==="出牌")[0]?.click();"ok"')
    time.sleep(0.3)

def pass_play(p):
    debug_eval(p, 'Array.from(document.querySelectorAll("button")).filter(b=>b.innerText.trim()==="要不起")[0]?.click();"ok"')
    time.sleep(0.3)

def check_game_over():
    for a, p in PAGES.items():
        txt = debug_eval(p, "document.body.innerText") or ""
        if "胜利" in txt or "🏆" in txt or "失败" in txt or "游戏结束" in txt:
            return a, txt
    return None, None

# Play 80 moves max
print("=== Playing doudizhu ===")
for i in range(80):
    cur, p = whose_turn()
    if not cur:
        winner, txt = check_game_over()
        if winner:
            print(f"  GAME OVER on {winner}")
            break
        time.sleep(1)
        continue
    
    hand = get_hand_size(p)
    if hand == 0:
        print(f"  {cur} has 0 cards - checking game over")
        time.sleep(2)
        winner, txt = check_game_over()
        if winner:
            print(f"  GAME OVER on {winner}")
            break
        continue
    
    cards = get_cards(p)
    print(f"  move {i+1}: {cur} hand={hand} cards={cards[:5]}...", end="")
    
    if cur == "test_03":
        # Landlord: play lowest single
        play_lowest_single(p)
        print(" played")
    else:
        # Peasant: try to beat
        play_lowest_single(p)
        print(" played/pass")
    time.sleep(1.2)

print("\n=== Final state ===")
for a, p in PAGES.items():
    h = get_hand_size(p)
    txt = debug_eval(p, "document.body.innerText") or ""
    print(f"--- {a} @ {p} hand={h} ---")
    print(txt[:1500])
    print()