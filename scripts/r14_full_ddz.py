#!/usr/bin/env python3
"""R14 Complete multi-account doudizhu game in single session."""
import json
import urllib.request
import urllib.parse
import time
import base64
import sys
import ssl

API_BASE = "https://127.0.0.1:39001"
DEBUG_BASE = "http://localhost:28999"

ssl_ctx = ssl.create_default_context()
ssl_ctx.check_hostname = False
ssl_ctx.verify_mode = ssl.CERT_NONE

def req(url, method="GET", data=None, token=None):
    if data and not isinstance(data, str):
        data = json.dumps(data)
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    r = urllib.request.Request(url, data=data.encode() if data else None, headers=headers, method=method)
    with urllib.request.urlopen(r, context=ssl_ctx) as resp:
        return json.loads(resp.read())

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

def debug_look(page_id, info, params=None):
    body = {"page_id": page_id, "info": info}
    if params:
        body["params"] = params
    r = urllib.request.Request(f"{DEBUG_BASE}/LookChromePageInfo",
                                data=json.dumps(body).encode(),
                                headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(r) as resp:
        return json.loads(resp.read())

def debug_eval(page_id, expr, await_promise=False):
    p = {"expression": expr}
    if await_promise:
        p["await_promise"] = True
    r = debug_call("eval_js", page_id, p)
    return r.get("data", {}).get("result")

def debug_click(page_id, selector, nth=0):
    return debug_call("click", page_id, {"selector": selector, "nth": nth})

def debug_navigate(page_id, url):
    return debug_call("navigate", page_id, {"url": url})

def new_page(url="https://127.0.0.1:39001/"):
    body = {"url": url}
    r = urllib.request.Request(f"{DEBUG_BASE}/NewChromePage",
                                data=json.dumps(body).encode(),
                                headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(r) as resp:
        d = json.loads(resp.read())
    return d["data"]["page_id"]

def screenshot(page_id, out_path):
    d = debug_look(page_id, "screenshot", {"type": "png"})
    img_b64 = d["data"]["data"]["image_base64"]
    with open(out_path, "wb") as f:
        f.write(base64.b64decode(img_b64))
    print(f"  saved: {out_path}")

def login_in_page(page_id, account_data):
    token = account_data["token"]
    uid = account_data["user_id"]
    inv = account_data["my_invite_code"]
    exp = account_data["expires_at"]
    js = (
        f"localStorage.setItem('lsm.token','{token}');"
        f"var s={{state:{{userId:'{uid}',token:'{token}',expiresAt:{exp},isAuthenticated:true,userType:1,myInviteCode:'{inv}'}},version:0}};"
        f"localStorage.setItem('lsm.auth',JSON.stringify(s));"
        f"'ok'"
    )
    debug_eval(page_id, js)
    debug_call("reload", page_id)

def click_btn_by_text(page_id, text):
    """Click a button by exact text."""
    js = f'Array.from(document.querySelectorAll("button")).filter(b=>b.innerText.trim()==={json.dumps(text)})[0]?.click();"ok"'
    return debug_eval(page_id, js)

def click_btn_by_text_substring(page_id, text):
    js = f'Array.from(document.querySelectorAll("button")).filter(b=>b.innerText.includes({json.dumps(text)}))[0]?.click();"ok"'
    return debug_eval(page_id, js)

def get_my_cards(page_id):
    """Return list of card texts in hand."""
    cards = debug_eval(page_id, 'JSON.stringify(Array.from(document.querySelectorAll(".doudizhu-hand .card-slot")).map(s=>s.textContent.trim()))')
    return json.loads(cards) if cards else []

def whose_turn(page_id):
    """Detect game phase for this player."""
    btns = debug_eval(page_id, "Array.from(document.querySelectorAll('button')).map(b=>b.innerText.trim())")
    if not btns:
        return "unknown"
    has_bid = "不叫" in btns and "1 分" in btns
    has_play = "出牌" in btns or "要不起" in btns
    if has_bid:
        return "self_bid"
    if has_play:
        return "self_play"
    return "wait"

# === MAIN ===
print("=== STEP 1: Login all accounts ===")
with open("/usr/local/LsmWebGame/test_account.json") as f:
    cfg = json.load(f)
accounts = []
for a in cfg["accounts"][:3]:  # test_01, test_02, test_03
    d = req(f"{API_BASE}/api/auth/login", "POST", {"account": a["account"], "password": a["password"]})
    accounts.append({"account": a["account"], **d["data"]})
    print(f"  {a['account']}: OK")

print("\n=== STEP 2: Create + login 3 pages ===")
pages = {}
for a in accounts:
    pid = new_page()
    pages[a["account"]] = pid
    login_in_page(pid, a)
    time.sleep(0.5)
    print(f"  {a['account']} @ {pid}")

time.sleep(2)

print("\n=== STEP 3: All 3 navigate to doudizhu lobby ===")
for a in accounts:
    debug_navigate(pages[a["account"]], "https://127.0.0.1:39001/doudizhu")
time.sleep(2)

print("\n=== STEP 4: test_01 creates room ===")
debug_click(pages["test_01"], ".btn.btn-primary")  # +创建房间
time.sleep(2)
# In modal-dialog, click the 创建房间 confirmation (button.btn-primary inside .modal-dialog)
btn = debug_eval(pages["test_01"], 'document.querySelector(".modal-dialog .btn.btn-primary")?.innerText')
print(f"  modal confirm btn: {btn}")
if btn:
    debug_eval(pages["test_01"], f'document.querySelector(".modal-dialog .btn.btn-primary")?.click();"ok"')
else:
    # Try alternative selectors
    debug_eval(pages["test_01"], 'Array.from(document.querySelectorAll(".modal-dialog button, .modal-overlay button")).filter(b=>!b.classList.contains("btn-secondary")).forEach(b=>b.click())')
time.sleep(2)

url = debug_eval(pages["test_01"], "location.href")
print(f"  test_01 URL: {url}")

# Extract room URL
import re
m = re.search(r'/doudizhu/([0-9a-f-]+)', url)
if not m:
    print("ERROR: no room created")
    sys.exit(1)
room_uuid = m.group(1)
room_url = f"https://127.0.0.1:39001/doudizhu/{room_uuid}"
print(f"  room URL: {room_url}")

print("\n=== STEP 5: test_02 + test_03 join room ===")
for a in accounts[1:]:
    debug_navigate(pages[a["account"]], room_url)
    time.sleep(2)
    u = debug_eval(pages[a["account"]], "location.href")
    print(f"  {a['account']} URL: {u}")

# Save state
state = {
    "pages": pages,
    "accounts": [{"account": a["account"], "user_id": a["user_id"], "token": a["token"], "expires_at": a["expires_at"], "my_invite_code": a["my_invite_code"]} for a in accounts],
    "room_uuid": room_uuid,
    "room_url": room_url,
}
with open("/tmp/r14_state.json", "w") as f:
    json.dump(state, f, indent=2)

# === PHASE: BIDDING ===
print("\n=== STEP 6: Bidding phase ===")
# Strategy: test_01 pass, test_02 pass, test_03 calls 1分
bid_done = False
for i in range(10):
    active = []
    for a in accounts:
        p = pages[a["account"]]
        s = whose_turn(p)
        active.append((a["account"], s))
    print(f"  round {i}: {active}")
    
    if all(s != "self_bid" for _, s in active):
        print("  bidding done")
        break
    
    for a in accounts:
        p = pages[a["account"]]
        if whose_turn(p) == "self_bid":
            if a["account"] in ("test_01", "test_02"):
                click_btn_by_text(p, "不叫")
                print(f"  {a['account']}: pass")
            elif a["account"] == "test_03":
                click_btn_by_text(p, "3 分")
                print(f"  {a['account']}: call 3分")
            time.sleep(1.5)

time.sleep(2)

# Verify landlord
print("\n=== STEP 7: Verify landlord ===")
for a in accounts:
    p = pages[a["account"]]
    txt = debug_eval(p, "document.body.innerText")
    role = "landlord" if "地主" in txt and ("你 👑地主" in txt or "(你) 👑地主" in txt or "你 👑" in txt) else ("farmer" if "农民" in txt else "?")
    landlord = "landlord" if "👑" in txt and ("你 👑" in txt) else ("landlord" if "地主" in txt else "farmer")
    hand = len(get_my_cards(p))
    is_my_turn = "你的回合" in txt
    print(f"  {a['account']}: role={role}, hand={hand}, my_turn={is_my_turn}")

# Find landlord
landlord = None
for a in accounts:
    p = pages[a["account"]]
    txt = debug_eval(p, "document.body.innerText")
    if "你 👑" in txt or "(你) 👑" in txt or "👑地主" in txt or "地主" in txt and "你" in txt.split("地主")[0][-5:]:
        landlord = a["account"]
        break

# Better: check 座位0 vs 座位1 vs 座位2
print("\n=== Seat assignment ===")
for a in accounts:
    p = pages[a["account"]]
    txt = debug_eval(p, "document.body.innerText")
    # Look for 座位N + name
    if "你" in txt and "👑" in txt:
        landlord = a["account"]
        print(f"  LANDLORD: {a['account']}")
        break

print(f"\nLandlord: {landlord}")

# Save final state
with open("/tmp/r14_state.json", "w") as f:
    json.dump({**state, "landlord": landlord}, f, indent=2)

# === PHASE: PLAYING ===
print("\n=== STEP 8: Playing cards ===")
# Game: simple strategy - landlord plays one card at a time
# Find current player with "你的回合"
def who_is_playing():
    for a in accounts:
        p = pages[a["account"]]
        if "你的回合" in (debug_eval(p, "document.body.innerText") or ""):
            return a["account"], p
    return None, None

def play_single_card(page_id, card_index):
    """Select card at index and click 出牌."""
    debug_click(page_id, f".doudizhu-hand .card-slot:nth-child({card_index+1})")
    time.sleep(0.3)
    debug_click(page_id, "button.btn.btn-primary")
    time.sleep(0.5)

# Play up to 20 moves
for move in range(20):
    cur, p = who_is_playing()
    if not cur:
        # Maybe game ended
        for a in accounts:
            txt = debug_eval(pages[a["account"]], "document.body.innerText")
            if "胜利" in txt or "失败" in txt or "游戏结束" in txt:
                print(f"  GAME OVER detected on {a['account']}")
                break
        print(f"  no one's turn, ending play loop")
        break
    
    # Pick lowest card
    cards = get_my_cards(p)
    if not cards:
        print(f"  {cur} has no cards, passing")
        click_btn_by_text(p, "要不起")
        time.sleep(1.5)
        continue
    
    # Find single card (no pair/single logic - just play lowest)
    # Pick first card for simplicity
    print(f"  move {move+1}: {cur} plays card index 0")
    play_single_card(p, 0)
    time.sleep(1.5)

# Screenshot final
print("\n=== STEP 9: Final screenshots ===")
for a in accounts:
    p = pages[a["account"]]
    screenshot(p, f"/home/aicon/.openclaw/workspace/r14/ddz_final_{a['account']}.png")
    txt = debug_eval(p, "document.body.innerText")
    print(f"--- {a['account']} ---")
    print((txt or "")[:600])
    print()