#!/usr/bin/env python3
"""R14 multi-account doudizhu game continuation."""
import json
import urllib.request
import urllib.parse
import time
import base64

API_BASE = "https://127.0.0.1:39001"
DEBUG_BASE = "http://localhost:28999"

def req(url, method="GET", data=None, token=None):
    if data and not isinstance(data, str):
        data = json.dumps(data)
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    ctx_args = {}
    import ssl
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    ctx_args["context"] = ctx
    r = urllib.request.Request(url, data=data.encode() if data else None, headers=headers, method=method)
    with urllib.request.urlopen(r, **ctx_args) as resp:
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

def debug_eval(page_id, expr):
    r = debug_call("eval_js", page_id, {"expression": expr})
    return r.get("data", {}).get("result")

def debug_click(page_id, selector, nth=0):
    return debug_call("click", page_id, {"selector": selector, "nth": nth})

def debug_navigate(page_id, url):
    return debug_call("navigate", page_id, {"url": url})

def screenshot(page_id, out_path):
    body = {"page_id": page_id, "info": "screenshot", "params": {"type": "png"}}
    r = urllib.request.Request(f"{DEBUG_BASE}/LookChromePageInfo",
                                data=json.dumps(body).encode(),
                                headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(r) as resp:
        d = json.loads(resp.read())
    img_b64 = d["data"]["data"]["image_base64"]
    with open(out_path, "wb") as f:
        f.write(base64.b64decode(img_b64))
    print(f"  saved screenshot: {out_path}")

# Load state
with open("/tmp/r14_state.json") as f:
    state = json.load(f)
pages = state["pages"]
accts = {a["account"]: a for a in state["accounts"]}
URL = "https://127.0.0.1:39001/doudizhu/345935ae-599a-4d0c-9642-4fb3c776cfa3"

print("=== Have test_02 + test_03 join doudizhu room ===")
for acct in ["test_02", "test_03"]:
    debug_navigate(pages[acct], URL)
    time.sleep(2)
    u = debug_eval(pages[acct], "location.href")
    print(f"  {acct} URL: {u}")

time.sleep(2)

# Take screenshot of all
print("\n=== Screenshots ===")
screenshot(pages["test_01"], "/home/aicon/.openclaw/workspace/r14/ddz_t01_seat0.png")
screenshot(pages["test_02"], "/home/aicon/.openclaw/workspace/r14/ddz_t02_seat1.png")
screenshot(pages["test_03"], "/home/aicon/.openclaw/workspace/r14/ddz_t03_seat2.png")

# Check game state on each page
print("\n=== Game state check ===")
for acct in ["test_01", "test_02", "test_03"]:
    txt = debug_eval(pages[acct], "document.body.innerText.substring(0,1500)")
    print(f"--- {acct} ---")
    print(txt[:600])
    print()

# Identify landlord / who's turn
print("\n=== Identify roles ===")
for acct in ["test_01", "test_02", "test_03"]:
    role = debug_eval(pages[acct], "document.body.innerText.match(/座位\\d \\S+ 地主|座位\\d \\S+ 农民|你 \\S+地主|你 \\S+农民|对手回合|你的回合/g)")
    my_hand_size = debug_eval(pages[acct], "document.querySelectorAll('.doudizhu-hand .card-slot').length")
    print(f"  {acct}: roles={role}, hand={my_hand_size}")