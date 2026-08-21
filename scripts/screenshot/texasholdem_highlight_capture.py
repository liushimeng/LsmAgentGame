#!/usr/bin/env python3
"""texasholdem_highlight_capture.py — 德州扑克「1 人类 + N Bot」精彩画面双页采集器

复用 atx.py 封装(§AutoScreenshot_TexasPoker.md §2)。工作方式:

    1. local_login 取 JWT, 注入 localStorage (lsm.token + lsm.auth)
    2. Chrome 同时打开 玩家页 /texasholdem/<room> 与 观战页 /texasholdem/spectate/<room>
    3. 每 interval 秒双页各截 1 张 PNG -> ProjectPic/raw/
    4. 阶段(street)从页面 DOM 推断: .texas-community 公共牌张数
       (0=preflop / 3=flop / 4=turn / 5=river|showdown) + innerText 关键词
    5. 阶段变化时立即补抓一张(捕捉翻牌/转牌/河牌/摊牌过渡)
    6. 终止: showdown + settle_delay / 硬超时(默认 75min); Chrome 连续失败重建页面

用法:
    python3 scripts/screenshot/texasholdem_highlight_capture.py --room-id <uuid> [--interval 60]
"""
import argparse
import json
import sys
import time
from pathlib import Path

THIS_DIR = Path(__file__).resolve().parent
sys.path.insert(0, str(THIS_DIR))
from atx import (  # noqa: E402
    click_text, close_page, eval_js, local_login, navigate, new_page,
    screenshot_b64,
)

PROJECT_ROOT = THIS_DIR.parent.parent
RAW_DIR = PROJECT_ROOT / "ProjectPic" / "raw"
MIN_BYTES = 30_000

# DOM 快照: 公共牌张数(street 推断) + 关键文本(pot/阶段徽章/思考中)
# face-down 卡背带 .face-down;正面牌 = .texas-card 无 face-down
STATE_JS = (
    "(()=>{"
    "const comm=document.querySelector('.texas-community');"
    "const cards=comm?comm.querySelectorAll('.texas-card:not(.face-down)').length:0;"
    "const txt=(document.body.innerText||'').slice(0,3000);"
    "return JSON.stringify({cards,"
    "hasShowdown:/摊牌|Showdown|showdown/.test(txt),"
    "hasThinking:/思考|行动|等待/.test(txt),"
    "pot:(txt.match(/(?:底池|Pot)[:\\s]*([\\d,]+)/)||[])[1]||''});})()"
)


def save_b64(b64, path: Path) -> bool:
    if not b64:
        return False
    import base64
    data = base64.b64decode(b64)
    if len(data) < MIN_BYTES:
        print(f"[WARN] too small ({len(data)}B): {path.name}", file=sys.stderr)
        return False
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(data)
    return True


def inject_auth(page_id, token, user_id, expires_at):
    auth = {"state": {"userId": user_id, "token": token, "expiresAt": expires_at,
                      "isAuthenticated": True, "userType": 1, "myInviteCode": None},
            "version": 0}
    js = (
        f"localStorage.setItem('lsm.token', {json.dumps(token)});"
        f"localStorage.setItem('lsm.auth', {json.dumps(json.dumps(auth, ensure_ascii=False))});"
        "'ok'"
    )
    eval_js(page_id, js)


# headless Chrome 无 emoji 字体 -> 豆腐块。注入 Web Font 垫底(同狼人杀版)。
FIT_CSS = (
    "(()=>{const s=document.getElementById('emoji-webfont')||document.createElement('style');"
    "s.id='emoji-webfont';"
    "s.textContent=`@font-face{font-family:'EmojiWeb';"
    "src:url('/assets/NotoColorEmoji.ttf') format('truetype');}"
    "html,body,body *{font-family:system-ui,-apple-system,sans-serif,'EmojiWeb';}`;"
    "if(!s.parentNode)document.head.appendChild(s);"
    "return 'fit-ok';})()"
)


def inject_page_fit(page_id):
    try:
        eval_js(page_id, FIT_CSS)
    except Exception as e:  # noqa: BLE001
        print(f"[WARN] page fit inject failed: {e}", file=sys.stderr)


def page_state(pid):
    """Read street snapshot from page DOM. Returns dict, {} on failure."""
    try:
        r = eval_js(pid, STATE_JS)
        raw = r.get("data", {}).get("result")
        if isinstance(raw, str):
            return json.loads(raw)
        return raw or {}
    except Exception:  # noqa: BLE001
        return {}


def street_of(st: dict) -> str:
    if not st:
        return "unknown"
    if st.get("hasShowdown"):
        return "showdown"
    n = st.get("cards", 0)
    if n >= 5:
        return "river"
    if n == 4:
        return "turn"
    if n == 3:
        return "flop"
    return "preflop"


def open_view(url, token, user_id, expires_at, headless):
    resp = new_page(url, wait_until="networkidle", headless=headless)
    pid = resp.get("data", {}).get("page_id")
    if not pid:
        raise RuntimeError(f"new_page failed: {resp}")
    inject_auth(pid, token, user_id, expires_at)
    navigate(pid, url, wait_until="networkidle")
    time.sleep(4)
    inject_page_fit(pid)
    time.sleep(2)
    return pid


def auto_act(pid):
    """轮到人类时自动 跟注/看牌(保守跟注,保局存活让 bot 链继续跑)。"""
    try:
        for label in ("看牌", "过牌", "Check", "跟注", "Call"):
            res = click_text(pid, label, tag="button", exact=False)
            if res.get("data", {}).get("result", {}).get("ok"):
                print(f"[ACT] clicked {label}")
                return True
        return False
    except Exception as e:  # noqa: BLE001
        print(f"[WARN] auto_act: {e}", file=sys.stderr)
        return False


def snap(pid, path):
    try:
        b64 = screenshot_b64(pid, width=1920, height=1080, format="png")
        return save_b64(b64, path)
    except Exception as e:  # noqa: BLE001
        print(f"[ERR] screenshot {path.name}: {e}", file=sys.stderr)
        return False


def main():
    ap = argparse.ArgumentParser(description="德州扑克精彩画面双页采集器")
    ap.add_argument("--room-id", required=True)
    ap.add_argument("--account", default="test_01")
    ap.add_argument("--password", default="LsmT1XSWhv3CvEUchWZ")
    ap.add_argument("--interval", type=int, default=60)
    ap.add_argument("--hard-timeout", type=int, default=75 * 60)
    ap.add_argument("--settle-delay", type=int, default=90,
                    help="showdown 检出后继续采集秒数(等筹码分配渲染)")
    ap.add_argument("--headless", action="store_true", default=True)
    ap.add_argument("--no-headless", dest="headless", action="store_false")
    ap.add_argument("--raw-dir", default=str(RAW_DIR))
    ap.add_argument("--auto-act", action="store_true", default=True,
                    help="轮到人类时自动跟注/看牌(默认开)")
    ap.add_argument("--no-auto-act", dest="auto_act", action="store_false")
    args = ap.parse_args()

    raw = Path(args.raw_dir)
    raw.mkdir(parents=True, exist_ok=True)
    login = local_login(args.account, args.password)
    token, uid, exp = login["token"], login["user_id"], login["expires_at"]
    print(f"[INFO] login ok user_id={uid}")

    base = "https://127.0.0.1:39001"
    player_url = f"{base}/texasholdem/{args.room_id}"
    spec_url = f"{base}/texasholdem/spectate/{args.room_id}"

    player_pid = open_view(player_url, token, uid, exp, args.headless)
    print(f"[INFO] player page: {player_pid}")
    spec_pid = open_view(spec_url, token, uid, exp, args.headless)
    print(f"[INFO] spectator page: {spec_pid}")

    state_log = raw / f"texas-state-{args.room_id[:8]}.jsonl"
    started = time.time()
    seq = 0
    last_street = None
    showdown_since = None
    fail_streak = 0

    while True:
        loop_t = time.time()
        elapsed = loop_t - started
        seq += 1
        ts = time.strftime("%Y%m%d_%H%M%S")

        st = page_state(player_pid)
        street = street_of(st)
        with state_log.open("a", encoding="utf-8") as f:
            f.write(json.dumps({"ts": ts, "seq": seq, "street": street, **st},
                               ensure_ascii=False) + "\n")
        print(f"[{ts}] #{seq} elapsed={int(elapsed)}s street={street} pot={st.get('pot')}")

        inject_page_fit(player_pid)
        inject_page_fit(spec_pid)
        ok1 = snap(player_pid, raw / f"texas-raw-player-{seq:03d}-{street}-{ts}.png")
        ok2 = snap(spec_pid, raw / f"texas-raw-spectator-{seq:03d}-{street}-{ts}.png")

        # 截完图再代人类行动(避免截到按钮点击动画);跟注/看牌保守保局
        if args.auto_act:
            auto_act(player_pid)

        # street 切换瞬间补抓(过渡帧 / bot 思考帧)
        if street != last_street and last_street is not None:
            time.sleep(3)
            snap(player_pid, raw / f"texas-raw-player-trans-{street}-{ts}.png")
            snap(spec_pid, raw / f"texas-raw-spectator-trans-{street}-{ts}.png")
        last_street = street

        if not ok1 and not ok2:
            fail_streak += 1
            if fail_streak >= 3:
                print("[WARN] 3 consecutive failures, rebuilding pages...", file=sys.stderr)
                try:
                    close_page(player_pid); close_page(spec_pid)
                except Exception:  # noqa: BLE001
                    pass
                player_pid = open_view(player_url, token, uid, exp, args.headless)
                spec_pid = open_view(spec_url, token, uid, exp, args.headless)
                fail_streak = 0
        else:
            fail_streak = 0

        if street == "showdown" and showdown_since is None:
            showdown_since = loop_t
            print(f"[INFO] showdown detected at {int(elapsed)}s, settling {args.settle_delay}s...")
        if showdown_since and (loop_t - showdown_since) >= args.settle_delay:
            print(f"[DONE] showdown + settled, total {int(elapsed)}s")
            break
        if elapsed >= args.hard_timeout:
            print(f"[DONE] hard timeout {int(elapsed)}s")
            break

        sleep_s = max(5, args.interval - (time.time() - loop_t))
        time.sleep(sleep_s)

    print(f"[DONE] raw screenshots in {raw}")


if __name__ == "__main__":
    sys.exit(main() or 0)
