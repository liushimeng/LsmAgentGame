#!/usr/bin/env python3
"""AutoTest helper for LsmAgentGame GoWebDebugTool.

Thin wrapper over the REST API exposed by /usr/local/LsmAgentGame/go-web-debug-tool.
"""
import json
import os
import ssl
import sys
import time
import urllib.request
import urllib.error

HOST = os.environ.get("HOST", "http://localhost:28999")
API_BASE = os.environ.get("API_BASE", "https://127.0.0.1:39001")
_SSL_CTX = ssl.create_default_context()
_SSL_CTX.check_hostname = False
_SSL_CTX.verify_mode = ssl.CERT_NONE


def _req(method, path, body=None):
    data = None
    if body is not None:
        data = json.dumps(body).encode("utf-8")
    req = urllib.request.Request(
        f"{HOST}{path}", data=data, method=method,
        headers={"Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            return json.loads(r.read())
    except urllib.error.HTTPError as e:
        return {"code": e.code, "message": e.reason, "raw": e.read().decode("utf-8", errors="replace")}
    except urllib.error.URLError as e:
        return {"code": -1, "message": str(e.reason)}


def new_page(url, wait_until="networkidle", headless=True):
    return _req("POST", "/NewChromePage", {
        "url": url, "headless": headless, "wait_until": wait_until
    })


def close_page(page_id):
    return _req("POST", "/CloseChromePage", {"page_id": page_id})


def list_pages():
    return _req("POST", "/ListChromePages", {})


def control(page_id, action, **params):
    return _req("POST", "/ControlChromePage", {
        "page_id": page_id, "action": action, "params": params
    })


def look(page_id, info, **params):
    return _req("POST", "/LookChromePageInfo", {
        "page_id": page_id, "info": info, "params": params
    })


def eval_js(page_id, expression):
    return control(page_id, "eval_js", expression=expression)


def navigate(page_id, url, wait_until="networkidle"):
    return control(page_id, "navigate", url=url, wait_until=wait_until)


def click(page_id, selector, nth=0):
    """Click the nth element matching selector. Returns the JS result dict."""
    js = (
        "(els => {"
        " const all = document.querySelectorAll(" + json.dumps(selector) + ");"
        " const e = all[" + str(nth) + "];"
        " if(!e){return {ok:false,reason:'no match',count:all.length};"
        "}"
        " e.scrollIntoView({block:'center'});"
        " e.click();"
        " return {ok:true,tag:e.tagName,text:(e.innerText||'').slice(0,80)};"
        "})()"
    )
    return eval_js(page_id, js)


def fill_input(page_id, selector, value, nth=0):
    js = (
        "(v => {"
        " const all = document.querySelectorAll(" + json.dumps(selector) + ");"
        " const e = all[" + str(nth) + "];"
        " if(!e){return {ok:false,reason:'no match',count:all.length};"
        "}"
        " const setter = Object.getOwnPropertyDescriptor("
        "   e.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLInputElement.prototype,'value').set;"
        " setter.call(e, v);"
        " e.dispatchEvent(new Event('input',{bubbles:true}));"
        " e.dispatchEvent(new Event('change',{bubbles:true}));"
        " return {ok:true,value:e.value};"
        "})(" + json.dumps(value) + ")"
    )
    return eval_js(page_id, js)


def click_text(page_id, text, tag="*", exact=True):
    js = (
        "(()=>{"
        " const t = " + json.dumps(tag) + ";"
        " const tx = " + json.dumps(text) + ";"
        " const exact = " + ("true" if exact else "false") + ";"
        " const nodes = Array.from(document.querySelectorAll(t));"
        " for (const n of nodes) {"
        "   const a = (n.innerText||'').trim();"
        "   const b = (n.textContent||'').trim();"
        "   if ((exact && (a===tx||b===tx)) || (!exact && (a.includes(tx)||b.includes(tx)))) {"
        "     n.scrollIntoView({block:'center'});"
        "     n.click();"
        "     return {ok:true, tag:n.tagName, text:a.slice(0,80)};"
        "   }"
        " }"
        " return {ok:false, reason:'text not found', searched:tx};"
        "})()"
    )
    return eval_js(page_id, js)


def list_inputs(page_id):
    """List all form-relevant input-like elements."""
    return eval_js(page_id, """
    (()=>{
      const list = [];
      document.querySelectorAll('input,textarea,button,a[href],[role=button]').forEach((e,i)=>{
        const r=e.getBoundingClientRect();
        list.push({
          i, tag:e.tagName.toLowerCase(),
          id:e.id||'', name:e.name||'',
          type:e.getAttribute('type')||'',
          text:(e.innerText||e.textContent||'').trim().slice(0,80),
          placeholder:e.placeholder||'',
          href:e.getAttribute('href')||'',
          visible: r.width>0 && r.height>0,
          rect:{x:Math.round(r.x),y:Math.round(r.y),w:Math.round(r.width),h:Math.round(r.height)},
        });
      });
      return list;
    })()
    """)


def url(page_id):
    return eval_js(page_id, "location.href").get("data", {}).get("result")


def screenshot_b64(page_id, **kw):
    """Take a screenshot and return base64 string. kw: width,height,quality,full_page"""
    return control(page_id, "screenshot", **kw).get("data", {}).get("result", {}).get("image_base64")


def local_login(account, password):
    """Login via API directly (captcha bypassed for test_01). Returns token + user_id."""
    req = urllib.request.Request(
        f"{API_BASE}/api/auth/login",
        data=json.dumps({"account": account, "password": password, "captcha_bypass": True}).encode(),
        method="POST",
        headers={"Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=15, context=_SSL_CTX) as r:
            body = json.loads(r.read())
    except urllib.error.HTTPError as e:
        body = json.loads(e.read())
    if body.get("code") != 0:
        return body
    d = body["data"]
    return {"token": d["token"], "user_id": d["user_id"], "expires_at": d["expires_at"]}


def set_local_storage(page_id, items):
    """Set multiple localStorage entries then reload the page."""
    js = "(()=>{"
    for k, v in items.items():
        js += f"localStorage.setItem({json.dumps(k)}, {json.dumps(v)});"
    js += "return Object.fromEntries(Object.entries(localStorage));})()"
    return eval_js(page_id, js)


def api_get(path, token=None):
    h = {"Content-Type": "application/json"}
    if token:
        h["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(f"{API_BASE}{path}", headers=h, method="GET")
    with urllib.request.urlopen(req, timeout=15, context=_SSL_CTX) as r:
        return json.loads(r.read())


def api_post(path, body, token=None):
    h = {"Content-Type": "application/json"}
    if token:
        h["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(
        f"{API_BASE}{path}", data=json.dumps(body).encode(), method="POST", headers=h
    )
    with urllib.request.urlopen(req, timeout=30, context=_SSL_CTX) as r:
        return json.loads(r.read())


def main():
    cmd = sys.argv[1] if len(sys.argv) > 1 else "help"
    if cmd == "eval":
        print(json.dumps(eval_js(sys.argv[2], sys.argv[3]), indent=2, ensure_ascii=False))
    elif cmd == "nav":
        print(json.dumps(navigate(sys.argv[2], sys.argv[3]), indent=2, ensure_ascii=False))
    elif cmd == "url":
        print(url(sys.argv[2]))
    elif cmd == "list_inputs":
        print(json.dumps(list_inputs(sys.argv[2]), indent=2, ensure_ascii=False))
    elif cmd == "click":
        print(json.dumps(click(sys.argv[2], sys.argv[3], int(sys.argv[4]) if len(sys.argv) > 4 else 0), indent=2, ensure_ascii=False))
    elif cmd == "fill":
        print(json.dumps(fill_input(sys.argv[2], sys.argv[3], sys.argv[4], int(sys.argv[5]) if len(sys.argv) > 5 else 0), indent=2, ensure_ascii=False))
    elif cmd == "login":
        print(json.dumps(local_login(sys.argv[2], sys.argv[3]), indent=2, ensure_ascii=False))
    else:
        print(__doc__)


if __name__ == "__main__":
    main()