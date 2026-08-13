#!/usr/bin/env python3
"""werewolf_highlight_capture.py — 狼人杀 13 人局「1 人类 + 12 Agent」精彩画面双页采集器

方案: tmpPlan/游戏精彩画面截图添加到-README-2026-08-13-01.md (阶段 2)

工作方式:
    1. local_login 取 JWT, 注入 localStorage (lsm.token + lsm.auth)
    2. Chrome 同时打开 玩家页 /werewolf/<room> 与 观战页 /werewolf/spectate/<room>
    3. 每 interval 秒双页各截 1 张 PNG -> ProjectPic/raw/
       每 60s 轮询 REST 房间状态 -> ProjectPic/raw/state.jsonl
    4. phase 变化时立即补抓一张(捕捉 天黑/天亮 过渡特效)
    5. 终止: status/phase == over 且过了 settle_delay 秒(等总结/结算渲染)
       或硬超时 hard_timeout(默认 75min); Chrome 连续失败 auto-rebuild 页面

用法:
    python3 scripts/werewolf_highlight_capture.py --room-id <uuid> [--interval 60]
"""
import argparse
import json
import sys
import time
from pathlib import Path

THIS_DIR = Path(__file__).resolve().parent
sys.path.insert(0, str(THIS_DIR))
from auto3.atx import (  # noqa: E402
    api_get, close_page, eval_js, list_pages, local_login, navigate, new_page,
    screenshot_b64,
)

PROJECT_ROOT = THIS_DIR.parent
RAW_DIR = PROJECT_ROOT / "ProjectPic" / "raw"
MIN_BYTES = 30_000


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


# headless Chrome (root 进程)无 emoji 字体 -> 豆腐块。通过 Web Font 注入
# Noto Color Emoji(由后端 /assets/NotoColorEmoji.ttf 提供,构建产物 gitignored)。
# EmojiWeb 垫底:数字/中文仍走系统字体,仅 emoji 字形补位。
# 同时 zoom 0.78 让 13 座位一屏全收(默认 9 座需滚动)。
FIT_CSS = (
    "(()=>{const s=document.getElementById('emoji-webfont')||document.createElement('style');"
    "s.id='emoji-webfont';"
    "s.textContent=`@font-face{font-family:'EmojiWeb';"
    "src:url('/assets/NotoColorEmoji.ttf') format('truetype');}"
    "html,body,body *{font-family:system-ui,-apple-system,sans-serif,'EmojiWeb';}`;"
    "if(!s.parentNode)document.head.appendChild(s);"
    "const t=document.querySelector('.werewolf-table');if(t){t.style.zoom='0.78';}"
    "return 'fit-ok:'+!!t;})()"
)


def inject_page_fit(page_id):
    try:
        eval_js(page_id, FIT_CSS)
    except Exception as e:  # noqa: BLE001
        print(f"[WARN] page fit inject failed: {e}", file=sys.stderr)


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


def snap(pid, path):
    try:
        b64 = screenshot_b64(pid, width=1920, height=1080, format="png")
        return save_b64(b64, path)
    except Exception as e:  # noqa: BLE001
        print(f"[ERR] screenshot {path.name}: {e}", file=sys.stderr)
        return False


def poll_state(token, room_id):
    try:
        d = api_get(f"/api/rooms/{room_id}", token=token)
        data = d.get("data") or {}
        return {
            "ts": time.strftime("%Y%m%d_%H%M%S"),
            "status": data.get("status"),
            "phase": data.get("phase"),
            "round": data.get("round_number"),
            "current_count": data.get("current_count"),
        }
    except Exception as e:  # noqa: BLE001
        return {"ts": time.strftime("%Y%m%d_%H%M%S"), "error": str(e)}


def main():
    ap = argparse.ArgumentParser(description="狼人杀精彩画面双页采集器")
    ap.add_argument("--room-id", required=True)
    ap.add_argument("--account", default="test_01")
    ap.add_argument("--password", default="LsmT1XSWhv3CvEUchWZ")
    ap.add_argument("--interval", type=int, default=60)
    ap.add_argument("--min-runtime", type=int, default=20 * 60,
                    help="最短运行秒数(默认 20 分钟)")
    ap.add_argument("--hard-timeout", type=int, default=75 * 60,
                    help="硬超时秒数(默认 75 分钟)")
    ap.add_argument("--settle-delay", type=int, default=180,
                    help="游戏结束后继续采集秒数(等总结渲染)")
    ap.add_argument("--headless", action="store_true", default=True)
    ap.add_argument("--no-headless", dest="headless", action="store_false")
    ap.add_argument("--raw-dir", default=str(RAW_DIR))
    args = ap.parse_args()

    raw = Path(args.raw_dir)
    raw.mkdir(parents=True, exist_ok=True)
    login = local_login(args.account, args.password)
    token, uid, exp = login["token"], login["user_id"], login["expires_at"]
    print(f"[INFO] login ok user_id={uid}")

    base = "https://127.0.0.1:39001"
    player_url = f"{base}/werewolf/{args.room_id}"
    spec_url = f"{base}/werewolf/spectate/{args.room_id}"

    player_pid = open_view(player_url, token, uid, exp, args.headless)
    print(f"[INFO] player page: {player_pid}")
    spec_pid = open_view(spec_url, token, uid, exp, args.headless)
    print(f"[INFO] spectator page: {spec_pid}")

    state_log = raw / "state.jsonl"
    started = time.time()
    seq = 0
    last_phase = None
    over_since = None
    fail_streak = 0

    while True:
        loop_t = time.time()
        elapsed = loop_t - started
        seq += 1
        ts = time.strftime("%Y%m%d_%H%M%S")

        st = poll_state(token, args.room_id)
        with state_log.open("a", encoding="utf-8") as f:
            f.write(json.dumps(st, ensure_ascii=False) + "\n")
        phase = st.get("phase")
        status = st.get("status")
        print(f"[{ts}] #{seq} elapsed={int(elapsed)}s status={status} phase={phase} round={st.get('round')}")

        # 每轮重注入(防 WS 重渲染 / SPA 导航擦掉注入样式)
        inject_page_fit(player_pid)
        inject_page_fit(spec_pid)
        ok1 = snap(player_pid, raw / f"werewolf-raw-player-{seq:03d}-{ts}.png")
        ok2 = snap(spec_pid, raw / f"werewolf-raw-spectator-{seq:03d}-{ts}.png")

        # phase 切换瞬间补抓(过渡特效/首帧)
        if phase and phase != last_phase and last_phase is not None:
            time.sleep(3)
            snap(player_pid, raw / f"werewolf-raw-player-trans-{phase}-{ts}.png")
            snap(spec_pid, raw / f"werewolf-raw-spectator-trans-{phase}-{ts}.png")
        last_phase = phase or last_phase

        # 连续失败 -> 重建页面
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

        # 终止判定
        is_over = (status == "over") or (phase == "over")
        if is_over and over_since is None:
            over_since = loop_t
            print(f"[INFO] game over detected at {int(elapsed)}s, settling {args.settle_delay}s...")
        if over_since and (loop_t - over_since) >= args.settle_delay \
                and elapsed >= args.min_runtime:
            print(f"[DONE] game over + settled, total {int(elapsed)}s")
            break
        if elapsed >= args.hard_timeout:
            print(f"[DONE] hard timeout {int(elapsed)}s")
            break

        sleep_s = max(5, args.interval - (time.time() - loop_t))
        time.sleep(sleep_s)

    print(f"[DONE] pages kept open: player={player_pid} spectator={spec_pid}")
    print(f"[DONE] raw screenshots in {raw}")


if __name__ == "__main__":
    sys.exit(main() or 0)
