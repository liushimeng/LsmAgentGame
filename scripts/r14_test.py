#!/usr/bin/env python3
"""R14 Multi-account Doudizhu + Xiangqi full game automation."""
import json
import urllib.request
import urllib.parse
import subprocess
import time
import sys
import os
import base64

API_BASE = "https://127.0.0.1:39001"
DEBUG_BASE = "http://localhost:28999"

def req(url, method="GET", data=None, token=None, insecure=True):
    url_full = url
    if data and not isinstance(data, str):
        data = json.dumps(data)
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    ctx_args = {}
    if insecure:
        import ssl
        ctx = ssl.create_default_context()
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_NONE
        ctx_args["context"] = ctx
    r = urllib.request.Request(url_full, data=data.encode() if data else None, headers=headers, method=method)
    with urllib.request.urlopen(r, **ctx_args) as resp:
        return json.loads(resp.read())

def login(account, password):
    r = req(f"{API_BASE}/api/auth/login", "POST", {"account": account, "password": password})
    return r["data"]

def balance(token):
    return req(f"{API_BASE}/api/wallet/balance", token=token)

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

def debug_eval(page_id, expr):
    r = debug_call("eval_js", page_id, {"expression": expr})
    return r.get("data", {}).get("result")

def debug_click(page_id, selector, nth=0):
    r = debug_call("click", page_id, {"selector": selector, "nth": nth})
    return r

def debug_navigate(page_id, url):
    r = debug_call("navigate", page_id, {"url": url})
    return r

def new_page(url="https://127.0.0.1:39001/"):
    body = {"url": url}
    r = urllib.request.Request(f"{DEBUG_BASE}/NewChromePage",
                                data=json.dumps(body).encode(),
                                headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(r) as resp:
        d = json.loads(resp.read())
    return d["data"]["page_id"]

def login_in_page(page_id, account_data):
    """Set localStorage and reload."""
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
    time.sleep(2)

def screenshot(page_id, out_path):
    """Save PNG screenshot."""
    d = debug_look(page_id, "screenshot", {"type": "png"})
    img_b64 = d["data"]["data"]["image_base64"]
    with open(out_path, "wb") as f:
        f.write(base64.b64decode(img_b64))
    print(f"  saved screenshot: {out_path}")

def main():
    # Step 1: Login all 4 test accounts
    accounts = []
    with open("/usr/local/LsmWebGame/test_account.json") as f:
        cfg = json.load(f)
    
    for a in cfg["accounts"]:
        d = login(a["account"], a["password"])
        accounts.append({"account": a["account"], "password": a["password"], **d})
        b = balance(d["token"])
        print(f"[{a['account']}] login OK, balance={b['data']['balance']}")

    # Step 2: Create 3 pages (avoid idle timeout by working fast)
    print("\n=== Creating pages ===")
    pages = {}
    for a in accounts:
        pid = new_page()
        pages[a["account"]] = pid
        print(f"  {a['account']}: {pid}")
    
    # Step 3: Login all 3 pages we'll use (skip test_04 for now to save pages)
    print("\n=== Logging in pages ===")
    for acct in ["test_01", "test_02", "test_03"]:
        a = next(x for x in accounts if x["account"] == acct)
        login_in_page(pages[acct], a)
        # Verify
        b = debug_eval(pages[acct], "JSON.parse(localStorage.getItem('lsm.auth')||'{}').state.userId")
        print(f"  {acct} @ {pages[acct]}: userId={b}")

    # Step 4: Navigate all to doudizhu lobby
    print("\n=== Navigate to doudizhu lobby ===")
    for acct in ["test_01", "test_02", "test_03"]:
        debug_navigate(pages[acct], "https://127.0.0.1:39001/doudizhu")
        time.sleep(0.5)
    time.sleep(2)

    # Step 5: test_01 creates a doudizhu room
    print("\n=== test_01 creates doudizhu room ===")
    debug_click(pages["test_01"], ".btn.btn-primary")  # 创建房间 button
    time.sleep(2)
    
    # Check for room dialog
    btns = debug_eval(pages["test_01"], "Array.from(document.querySelectorAll('button')).map(b=>b.innerText.substring(0,30))")
    print(f"  test_01 buttons: {btns}")
    
    # If there's a dialog, click 创建房间 button to confirm
    if "创建房间" in str(btns):
        # Find the 创建房间 button inside dialog
        debug_eval(pages["test_01"], "Array.from(document.querySelectorAll('button')).filter(b=>b.innerText.trim()==='创建房间').forEach(b=>b.click())")
        time.sleep(2)

    # Check URL change
    url = debug_eval(pages["test_01"], "location.href")
    print(f"  test_01 URL: {url}")
    
    # If still in lobby, maybe dialog needs different handling
    if "/doudizhu/" not in str(url):
        # Try clicking the dialog 创建房间 button via class
        btns = debug_eval(pages["test_01"], "Array.from(document.querySelectorAll('.modal button, .ant-modal button, [role=dialog] button, .dialog button')).map(b=>b.innerText)")
        print(f"  dialog buttons: {btns}")
        # Just try with text contains
        debug_eval(pages["test_01"], "Array.from(document.querySelectorAll('button')).filter(b=>b.innerText.includes('创建')||b.innerText.includes('确认')).forEach(b=>b.click())")
        time.sleep(2)
        url = debug_eval(pages["test_01"], "location.href")
        print(f"  test_01 URL after retry: {url}")

    # Save state
    state = {
        "pages": pages,
        "accounts": [{"account": a["account"], "user_id": a["user_id"], "token": a["token"]} for a in accounts],
    }
    with open("/tmp/r14_state.json", "w") as f:
        json.dump(state, f, indent=2)
    print(f"\nState saved. Pages: {pages}")

if __name__ == "__main__":
    main()