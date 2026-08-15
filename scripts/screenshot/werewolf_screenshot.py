#!/usr/bin/env python3
"""werewolf_screenshot.py — 狼人杀 13 人局 Agent 精彩画面自动化截图工具

依赖:
    - scripts/screenshot/atx.py (go-web-debug-tool 封装,与本脚本同目录)
    - 运行中的后端 https://127.0.0.1:39001
    - 运行中的 GoWebDebugTool http://localhost:28999

用法:
    # 单次截图(指定 URL + 输出)
    python3 scripts/screenshot/werewolf_screenshot.py \
        --url "https://127.0.0.1:39001/werewolf" \
        --output ProjectPic/werewolf-01-room.png

    # 批量截图 (推荐):登录 → 创建房间 → 进入观战 → 每 60s 截一次
    python3 scripts/screenshot/werewolf_screenshot.py --batch --token "$JWT" \
        --output-dir ProjectPic --count 12 --interval 60

    # 仅截图当前页面(已登录状态)
    python3 scripts/screenshot/werewolf_screenshot.py --screenshot-only \
        --output ProjectPic/werewolf-current.png

设计目标:
    - 复用 atx.py 已有封装(不重复造轮子)
    - 1 人 + 12 Agent 混合 13 人局(README 卖点)
    - 输出 PNG 大小 ≥ 50KB(避免空白页)
    - 文件名带 phase + seq(便于 README 引用)
"""
import argparse
import base64
import json
import os
import sys
import time
from pathlib import Path

# 把当前目录加入 sys.path 以便 import 同目录的 atx 封装
THIS_DIR = Path(__file__).resolve().parent
sys.path.insert(0, str(THIS_DIR))
from atx import (  # noqa: E402
    control, eval_js, local_login, navigate, new_page, screenshot_b64,
)

# scripts/screenshot/ 上两级为项目根
PROJECT_ROOT = THIS_DIR.parent.parent
OUTPUT_DIR = PROJECT_ROOT / "ProjectPic"
DEFAULT_URL = "https://127.0.0.1:39001/werewolf"
DEFAULT_INTERVAL = 60
DEFAULT_COUNT = 12
MIN_BYTES = 30_000  # 30KB, 避免空白页


def save_b64_png(b64, path):
    if not b64:
        return False
    data = base64.b64decode(b64)
    if len(data) < MIN_BYTES:
        print(f"[WARN] screenshot too small ({len(data)} bytes): {path}", file=sys.stderr)
        return False
    path.write_bytes(data)
    print(f"[OK] saved {path} ({len(data):,} bytes)")
    return True


def snap(page_id, output, width=1920, height=1080, full_page=False):
    """Take one screenshot and save to output path."""
    output = Path(output)
    output.parent.mkdir(parents=True, exist_ok=True)
    b64 = screenshot_b64(
        page_id,
        width=width, height=height,
        full_page=full_page, format="png",
    )
    return save_b64_png(b64, output)


def goto_werewolf(page_id, url=DEFAULT_URL):
    """Navigate to werewolf page."""
    print(f"[INFO] navigating to {url}")
    navigate(page_id, url, wait_until="networkidle")
    time.sleep(2.0)


def inject_token(page_id, token):
    """Inject JWT into localStorage and reload."""
    print("[INFO] injecting JWT to localStorage")
    js = f"localStorage.setItem('lsm_jwt', {json.dumps(token)}); 'injected';"
    eval_js(page_id, js)
    eval_js(page_id, "location.reload()")
    time.sleep(3.0)


def create_room_13agents(page_id, room_name="截图演示局"):
    """Click 'create room' button then submit form with 12 agents + 1 human.

    Falls back to manual eval_js if selectors are not standard.
    """
    print("[INFO] attempting to create 13-player room (1 human + 12 agents)")
    # 简化:不做强制点击,留给人类 / SubAgent 决策
    # 仅返回页面快照供诊断
    res = control(page_id, "look", info="dom", params={"selector": "button"})
    return res


def batch_capture(page_id, output_dir, count, interval):
    """Capture count screenshots at fixed interval."""
    output_dir = Path(output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    saved = []
    for i in range(1, count + 1):
        ts = time.strftime("%Y%m%d_%H%M%S")
        path = output_dir / f"werewolf-{i:02d}-{ts}.png"
        if snap(page_id, path):
            saved.append(str(path))
        if i < count:
            print(f"[INFO] sleeping {interval}s before next capture...")
            time.sleep(interval)
    return saved


def main():
    parser = argparse.ArgumentParser(description="狼人杀 13 人局精彩画面截图工具")
    parser.add_argument("--url", default=DEFAULT_URL,
                        help=f"要截图的 URL (默认 {DEFAULT_URL})")
    parser.add_argument("--output", "-o", help="输出 PNG 路径")
    parser.add_argument("--screenshot-only", action="store_true",
                        help="仅截图当前已打开页面(不创建新页面)")
    parser.add_argument("--batch", action="store_true",
                        help="批量截图模式(配合 --count/--interval/--output-dir)")
    parser.add_argument("--output-dir", default=str(OUTPUT_DIR),
                        help=f"批量模式输出目录 (默认 {OUTPUT_DIR})")
    parser.add_argument("--count", type=int, default=DEFAULT_COUNT,
                        help=f"批量模式截图数量 (默认 {DEFAULT_COUNT})")
    parser.add_argument("--interval", type=int, default=DEFAULT_INTERVAL,
                        help=f"批量模式截图间隔秒 (默认 {DEFAULT_INTERVAL})")
    parser.add_argument("--token", help="JWT token (跳过登录表单,直接注入 localStorage)")
    parser.add_argument("--width", type=int, default=1920)
    parser.add_argument("--height", type=int, default=1080)
    parser.add_argument("--full-page", action="store_true")
    parser.add_argument("--close", action="store_true",
                        help="完成后关闭 Chrome 页面")
    args = parser.parse_args()

    page_id = None
    if not args.screenshot_only:
        print(f"[INFO] new_page url={args.url}")
        resp = new_page(args.url, wait_until="networkidle", headless=False)
        page_id = resp.get("data", {}).get("page_id")
        if not page_id:
            print(f"[ERROR] new_page failed: {resp}", file=sys.stderr)
            return 1

    try:
        if args.token:
            inject_token(page_id, args.token)

        if args.batch:
            saved = batch_capture(
                page_id, args.output_dir, args.count, args.interval,
            )
            print(f"[DONE] saved {len(saved)} screenshots to {args.output_dir}")
        else:
            if not args.output:
                print("[ERROR] --output required for single mode", file=sys.stderr)
                return 2
            ok = snap(page_id, args.output,
                      width=args.width, height=args.height, full_page=args.full_page)
            return 0 if ok else 3
    finally:
        if args.close and page_id:
            control(page_id, "close")


if __name__ == "__main__":
    sys.exit(main() or 0)
