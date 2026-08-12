#!/usr/bin/env python3
"""Continue doudizhu game - finish it quickly."""
import json
import urllib.request
import time
import base64

DEBUG_BASE = "http://localhost:28999"

def debug_eval(page_id, expr):
    body = {"action": "eval_js", "page_id": page_id, "params": {"expression": expr}}
    r = urllib.request.Request(f"{DEBUG_BASE}/ControlChromePage", 
                                data=json.dumps(body).encode(),
                                headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(r) as resp:
        resp_json = json.loads(resp.read())
    # Extract the actual result
    if "data" in resp_json and "result" in resp_json["data"]:
        return resp_json["data"]["result"]
    return resp_json

def debug_call(action, page_id=None, params=None):
    body = {"action": action}
    if page_id:
        body["page_id"] = page_id
    if params:
        body["params"] = params
    r = urllib.request.Request(f"{DEBUG_BASE}/ControlChromePage", 
                                data=json.dumps(body).encode(),
                                headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(r) as resp:
        return json.loads(resp.read())

def debug_click(page_id, selector):
    return debug_call("click", page_id, {"selector": selector})

def click_btn_text(page_id, text):
    js = f'Array.from(document.querySelectorAll("button")).filter(b=>b.innerText.trim()==={json.dumps(text)})[0]?.click();"ok"'
    return debug_eval(page_id, js)

def get_cards(page_id):
    """Get current hand cards."""
    js = 'JSON.stringify(Array.from(document.querySelectorAll(".doudizhu-hand .card-slot")).map(s=>s.textContent.trim()))'
    r = debug_eval(page_id, js)
    return json.loads(r) if r and r != "null" else []

def get_hand_size(page_id):
    return debug_eval(page_id, "document.querySelectorAll('.doudizhu-hand .card-slot').length")

def play_single_lowest(page_id):
    """Click first card, then 出牌."""
    cards = get_cards(page_id)
    if not cards:
        return False
    # Click first card (index 0)
    debug_click(page_id, ".doudizhu-hand .card-slot:nth-child(1)")
    time.sleep(0.2)
    debug_click(page_id, ".doudizhu-hand + button.btn-primary, button.btn.btn-primary")
    time.sleep(0.5)
    # Actually need to click the bottom panel 出牌 button
    debug_eval(page_id, 'Array.from(document.querySelectorAll("button")).filter(b=>b.innerText.trim()==="出牌")[0]?.click();"ok"')
    time.sleep(0.5)
    return True

def pass_card(page_id):
    click_btn_text(page_id, "要不起")
    time.sleep(0.3)

# Page mapping
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

# Quick check
print("Initial state:")
for a, p in PAGES.items():
    h = get_hand_size(p)
    mt = debug_eval(p, 'document.body.innerText.includes("你的回合")')
    print(f"  {a} @ {p}: hand={h}, my_turn={mt}")

# Play 60 moves max
for i in range(60):
    cur, p = whose_turn()
    if not cur:
        # Game might be over
        txt_t03 = debug_eval(PAGES["test_03"], "document.body.innerText") or ""
        if "胜利" in txt_t03 or "失败" in txt_t03 or "游戏结束" in txt_t03:
            print(f"  GAME OVER detected on test_03")
            break
        print(f"  no one's turn at move {i+1}, sleeping")
        time.sleep(1)
        continue
    
    hand = get_hand_size(p)
    if hand == 0:
        print(f"  {cur} has 0 cards - game should be over")
        break
    
    print(f"  move {i+1}: {cur} (hand={hand})", end="")
    if play_single_lowest(p):
        print(" played")
    else:
        print(" no cards, passing")
        pass_card(p)
    time.sleep(1.5)

print("\nFinal state:")
for a, p in PAGES.items():
    h = get_hand_size(p)
    txt = debug_eval(p, "document.body.innerText")
    is_winner = "胜利" in txt or "🏆" in txt
    is_loser = "失败" in txt
    print(f"  {a} @ {p}: hand={h}, winner={is_winner}, loser={is_loser}")
    print(f"    text snippet: {txt[600:1100]}")