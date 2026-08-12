#!/usr/bin/env python3
"""R15 Common helpers for multi-account game automation."""
import json
import urllib.request
import time
import base64
import ssl

API_BASE = "https://127.0.0.1:39001"
DEBUG_BASE = "http://localhost:28999"

SSL_CTX = ssl.create_default_context()
SSL_CTX.check_hostname = False
SSL_CTX.verify_mode = ssl.CERT_NONE


def req(url, method="GET", data=None, token=None):
    if data and not isinstance(data, str):
        data = json.dumps(data)
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    r = urllib.request.Request(url, data=data.encode() if data else None, headers=headers, method=method)
    with urllib.request.urlopen(r, context=SSL_CTX) as resp:
        return json.loads(resp.read())


def login(account, password):
    r = req(f"{API_BASE}/api/auth/login", "POST", {"account": account, "password": password})
    return r["data"]


def balance(token):
    return req(f"{API_BASE}/api/wallet/balance", token=token)


def dbg_call(action, page_id=None, params=None):
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


def dbg_look(page_id, info, params=None):
    body = {"page_id": page_id, "info": info}
    if params:
        body["params"] = params
    r = urllib.request.Request(f"{DEBUG_BASE}/LookChromePageInfo",
                               data=json.dumps(body).encode(),
                               headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(r) as resp:
        return json.loads(resp.read())


def dbg_eval(page_id, expr):
    r = dbg_call("eval_js", page_id, {"expression": expr})
    if r.get("code") != 0:
        return None
    return r.get("data", {}).get("result")


def dbg_click(page_id, selector, nth=0):
    return dbg_call("click", page_id, {"selector": selector, "nth": nth})


def dbg_navigate(page_id, url):
    return dbg_call("navigate", page_id, {"url": url})


def dbg_reload(page_id):
    return dbg_call("reload", page_id, {})


def dbg_screenshot(page_id, out_path):
    d = dbg_look(page_id, "screenshot", {"type": "png"})
    img_b64 = d["data"]["data"]["image_base64"]
    with open(out_path, "wb") as f:
        f.write(base64.b64decode(img_b64))


def new_page(url="https://127.0.0.1:39001/"):
    body = {"url": url}
    r = urllib.request.Request(f"{DEBUG_BASE}/NewChromePage",
                               data=json.dumps(body).encode(),
                               headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(r) as resp:
        d = json.loads(resp.read())
    return d["data"]["page_id"]


def login_in_page(page_id, account_data):
    """Set token + auth in localStorage, then reload."""
    token = account_data["token"]
    uid = account_data["user_id"]
    inv = account_data.get("my_invite_code", "")
    exp = account_data["expires_at"]
    js = (
        f"localStorage.setItem('lsm.token','{token}');"
        f"var s={{state:{{userId:'{uid}',token:'{token}',expiresAt:{exp},isAuthenticated:true,userType:1,myInviteCode:'{inv}'}},version:0}};"
        f"localStorage.setItem('lsm.auth',JSON.stringify(s));"
        f"'ok'"
    )
    dbg_eval(page_id, js)
    dbg_reload(page_id)
    time.sleep(2)


def click_btn_text(page_id, text):
    """Click button with exact text via element.click()."""
    js = f'Array.from(document.querySelectorAll("button")).filter(b=>b.innerText.trim()==={json.dumps(text)})[0]?.click();"ok"'
    return dbg_eval(page_id, js)


def click_btn_substr(page_id, text):
    js = f'Array.from(document.querySelectorAll("button")).filter(b=>b.innerText.includes({json.dumps(text)}))[0]?.click();"ok"'
    return dbg_eval(page_id, js)


def list_pages():
    r = urllib.request.Request(f"{DEBUG_BASE}/ListChromePages",
                               data=b'{}',
                               headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(r) as resp:
        d = json.loads(resp.read())
    return d.get("data", {}).get("pages", [])


def page_url(page_id):
    d = dbg_look(page_id, "url")
    return d.get("data", {}).get("data", {}).get("url", "")


def login_all_accounts():
    """Load test_account.json, login first 3, return list of dicts."""
    with open("/usr/local/LsmWebGame/test_account.json") as f:
        cfg = json.load(f)
    out = []
    for a in cfg["accounts"][:3]:
        d = login(a["account"], a["password"])
        b = balance(d["token"])["data"]["balance"]
        out.append({"account": a["account"], "password": a["password"], **d, "balance": b})
        print(f"  [{a['account']}] login OK, balance={b}")
    return out


def setup_pages(accounts):
    """Create a page for each account, login in. Returns {account: page_id}."""
    pages = {}
    for a in accounts:
        pid = new_page()
        pages[a["account"]] = pid
        login_in_page(pid, a)
        print(f"  {a['account']} @ {pid}")
    return pages