#!/usr/bin/env python3
"""虚拟城市 — 程序化城市纹理生成器（轨道 2 · 无外部 API 依赖）。

产物清单（方案：lag_docs/虚拟城市/已实现/13-3D城市渲染优化/03-资产生成方案-v1.md）：

  v2.12 新 8 城区 × 4 类 = 32 张 PNG，写入 ../ClientWeb/src/assets/images/wealth/：
    districts/<id>.png          1024×1024  城区俯视底板
    facades/<id>_base.png       512×1024   楼宇立面·底层段
    facades/<id>_mid.png        512×1024   楼宇立面·中上段（循环段）
    roofs/<id>.png              512×512    屋顶俯视

  城区 id：logistics_port / hightech_park / edu_district / medical_city /
           industrial_park / central_park / transport_hub / cultural_creative

约定：
  - 纯 Pillow，不依赖 python-generate-image-tool 子模块、不访问网络。
  - 确定性 seed = FNV-1a(文件名)，重跑产物字节一致。
  - 幂等 skip-if-exists（AI 轨道已生成的文件绝不覆盖）；--force 强制覆盖。
  - --only districts,facades,roofs 生成子集。

用法：
  python3 3d_script/procedural_city_textures.py            # 补齐缺失
  python3 3d_script/procedural_city_textures.py --force    # 全部重生成
"""

from __future__ import annotations

import argparse
import math
import random
import sys
from pathlib import Path

try:
    from PIL import Image, ImageDraw, ImageFilter
except ImportError:  # pragma: no cover
    sys.exit("需要 Pillow：pip install 'Pillow>=10'")

BASE_DIR = Path(__file__).resolve().parent
WEALTH_DIR = BASE_DIR.parent / "ClientWeb" / "src" / "assets" / "images" / "wealth"
DISTRICTS_DIR = WEALTH_DIR / "districts"
FACADES_DIR = WEALTH_DIR / "facades"
ROOFS_DIR = WEALTH_DIR / "roofs"

DISTRICT_SIZE = (1024, 1024)
FACADE_SIZE = (512, 1024)
ROOF_SIZE = (512, 512)

NEW_DISTRICTS = [
    "logistics_port",
    "hightech_park",
    "edu_district",
    "medical_city",
    "industrial_park",
    "central_park",
    "transport_hub",
    "cultural_creative",
]


def fnv1a(s: str) -> int:
    h = 2166136261
    for ch in s:
        h ^= ord(ch)
        h = (h * 16777619) & 0xFFFFFFFF
    return h


def rng_for(name: str) -> random.Random:
    return random.Random(fnv1a(name))


# ── 城区配色（与 types/wealth.ts::WEALTH_DISTRICTS 主色呼应）────────────────

PALETTES = {
    "logistics_port": {
        "ground": (74, 85, 104), "block": (96, 108, 128), "accent": (148, 163, 184),
        "facade": (120, 133, 152), "window": (38, 48, 64), "roof": (100, 110, 125),
    },
    "hightech_park": {
        "ground": (52, 68, 74), "block": (70, 92, 100), "accent": (125, 211, 192),
        "facade": (168, 182, 190), "window": (30, 60, 72), "roof": (120, 135, 142),
    },
    "edu_district": {
        "ground": (72, 84, 60), "block": (150, 74, 56), "accent": (235, 228, 210),
        "facade": (158, 82, 62), "window": (245, 240, 225), "roof": (96, 60, 50),
    },
    "medical_city": {
        "ground": (78, 88, 96), "block": (228, 232, 236), "accent": (125, 180, 220),
        "facade": (232, 236, 240), "window": (120, 165, 205), "roof": (200, 208, 215),
    },
    "industrial_park": {
        "ground": (70, 66, 60), "block": (110, 104, 96), "accent": (160, 120, 80),
        "facade": (128, 120, 110), "window": (52, 50, 46), "roof": (95, 90, 84),
    },
    "central_park": {
        "ground": (46, 108, 62), "block": (62, 132, 80), "accent": (180, 205, 150),
        "facade": (150, 128, 96), "window": (60, 52, 40), "roof": (110, 90, 70),
    },
    "transport_hub": {
        "ground": (80, 78, 82), "block": (105, 102, 108), "accent": (234, 120, 50),
        "facade": (140, 138, 145), "window": (40, 48, 60), "roof": (115, 112, 118),
    },
    "cultural_creative": {
        "ground": (86, 68, 78), "block": (178, 72, 96), "accent": (240, 180, 90),
        "facade": (196, 96, 108), "window": (48, 36, 44), "roof": (140, 80, 92),
    },
}


def _noise_speckle(draw: ImageDraw.ImageDraw, rng: random.Random, w: int, h: int,
                   color: tuple[int, int, int], count: int, r_max: int = 3) -> None:
    for _ in range(count):
        x, y = rng.randint(0, w), rng.randint(0, h)
        r = rng.randint(1, r_max)
        draw.ellipse([x - r, y - r, x + r, y + r], fill=color)


def gen_district_baseboard(did: str) -> Image.Image:
    """城区俯视底板：底色 + 街区块 + 路网暗线；central_park 为绿地+园路+湖面。"""
    w, h = DISTRICT_SIZE
    p = PALETTES[did]
    rng = rng_for(f"district:{did}")
    img = Image.new("RGB", (w, h), p["ground"])
    d = ImageDraw.Draw(img)

    if did == "central_park":
        # 绿地斑块
        for _ in range(40):
            x, y = rng.randint(0, w), rng.randint(0, h)
            r = rng.randint(30, 120)
            shade = tuple(max(0, min(255, c + rng.randint(-18, 18))) for c in p["block"])
            d.ellipse([x - r, y - r, x + r, y + r], fill=shade)
        # 蜿蜒园路（浅色折线）
        for _ in range(5):
            pts = []
            x, y = rng.randint(0, w), rng.randint(0, h)
            for _ in range(8):
                pts.append((x, y))
                x = max(0, min(w, x + rng.randint(-160, 160)))
                y = max(0, min(h, y + rng.randint(-160, 160)))
            d.line(pts, fill=p["accent"], width=14, joint="curve")
        # 湖面
        lx, ly = rng.randint(w // 4, w * 3 // 4), rng.randint(h // 4, h * 3 // 4)
        d.ellipse([lx - 130, ly - 90, lx + 130, ly + 90], fill=(58, 110, 150))
        d.ellipse([lx - 110, ly - 72, lx + 110, ly + 72], fill=(70, 128, 170))
    else:
        # 街区块（网格 + 抖动）
        cell = 170
        for gy in range(0, h, cell):
            for gx in range(0, w, cell):
                if rng.random() < 0.28:
                    continue
                pad = rng.randint(14, 30)
                shade = tuple(max(0, min(255, c + rng.randint(-14, 14))) for c in p["block"])
                d.rectangle([gx + pad, gy + pad, gx + cell - pad, gy + cell - pad], fill=shade)
        # 路网暗线
        road = tuple(max(0, c - 26) for c in p["ground"])
        for i in range(1, 6):
            pos = i * w // 6
            d.line([(pos, 0), (pos, h)], fill=road, width=10)
            d.line([(0, pos), (w, pos)], fill=road, width=10)
        _noise_speckle(d, rng, w, h, p["accent"], 240)

    return img.filter(ImageFilter.GaussianBlur(0.6))


def gen_facade(did: str, variant: str) -> Image.Image:
    """楼宇立面：底色 + 窗格矩阵；base 段底部加裙楼色带，mid 段纯窗格循环。"""
    w, h = FACADE_SIZE
    p = PALETTES[did]
    rng = rng_for(f"facade:{did}:{variant}")
    img = Image.new("RGB", (w, h), p["facade"])
    d = ImageDraw.Draw(img)

    # 竖向材质分带（铝板/砖缝感）
    band = tuple(max(0, min(255, c - 10)) for c in p["facade"])
    for y in range(0, h, 64):
        d.line([(0, y), (w, y)], fill=band, width=2)

    # 窗格矩阵
    cols = 8
    rows = 20
    cw, ch = w // cols, h // rows
    win_w, win_h = int(cw * 0.56), int(ch * 0.52)
    lit_ratio = 0.22 if did in ("medical_city", "hightech_park") else 0.12
    for r in range(rows):
        for c in range(cols):
            x0 = c * cw + (cw - win_w) // 2
            y0 = r * ch + (ch - win_h) // 2
            wc = p["window"]
            if rng.random() < lit_ratio:
                wc = tuple(min(255, v + 90) for v in wc)  # 亮灯窗
            d.rectangle([x0, y0, x0 + win_w, y0 + win_h], fill=wc)
            # 窗上沿高光
            hi = tuple(min(255, v + 40) for v in wc)
            d.line([(x0, y0), (x0 + win_w, y0)], fill=hi, width=2)

    if variant == "base":
        # 底部裙楼色带（占 18% 高）
        band_h = int(h * 0.18)
        podium = tuple(max(0, min(255, c - 22)) for c in p["facade"])
        d.rectangle([0, h - band_h, w, h], fill=podium)
        # 入口雨棚
        d.rectangle([w // 2 - 70, h - band_h - 12, w // 2 + 70, h - band_h], fill=p["accent"])
        # 大门
        d.rectangle([w // 2 - 40, h - 90, w // 2 + 40, h], fill=p["window"])

    return img.filter(ImageFilter.GaussianBlur(0.4))


def gen_roof(did: str) -> Image.Image:
    """屋顶俯视：平顶灰 + 设备方块；坡屋顶城区给瓦条纹。"""
    w, h = ROOF_SIZE
    p = PALETTES[did]
    rng = rng_for(f"roof:{did}")
    img = Image.new("RGB", (w, h), p["roof"])
    d = ImageDraw.Draw(img)

    if did in ("edu_district", "cultural_creative"):
        # 坡屋顶瓦条纹
        stripe = tuple(max(0, c - 18) for c in p["roof"])
        for x in range(0, w, 18):
            d.line([(x, 0), (x, h)], fill=stripe, width=4)
        # 屋脊
        d.line([(0, h // 2), (w, h // 2)], fill=tuple(min(255, c + 30) for c in p["roof"]), width=10)
    else:
        # 女儿墙描边
        edge = tuple(max(0, c - 24) for c in p["roof"])
        d.rectangle([8, 8, w - 8, h - 8], outline=edge, width=10)
        # 屋顶设备（空调机组/水箱方块 + 投影）
        for _ in range(rng.randint(3, 6)):
            bw, bh = rng.randint(40, 110), rng.randint(40, 110)
            x, y = rng.randint(30, w - bw - 30), rng.randint(30, h - bh - 30)
            d.rectangle([x + 6, y + 6, x + bw + 6, y + bh + 6], fill=(0, 0, 0, 60))
            box = tuple(max(0, min(255, c + rng.randint(-12, 20))) for c in p["roof"])
            d.rectangle([x, y, x + bw, y + bh], fill=box, outline=edge, width=3)
        _noise_speckle(d, rng, w, h, edge, 160, r_max=2)

    return img.filter(ImageFilter.GaussianBlur(0.4))


def _save(img: Image.Image, path: Path, force: bool) -> str:
    if path.exists() and not force:
        return f"skip  {path.name}"
    path.parent.mkdir(parents=True, exist_ok=True)
    img.save(path, "PNG")
    return f"write {path.name} {img.size[0]}x{img.size[1]}"


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--force", action="store_true", help="覆盖已存在文件")
    ap.add_argument("--only", default="", help="逗号分隔子集：districts,facades,roofs")
    args = ap.parse_args()
    only = {s.strip() for s in args.only.split(",") if s.strip()}

    total = written = 0
    for did in NEW_DISTRICTS:
        jobs = []
        if not only or "districts" in only:
            jobs.append((gen_district_baseboard(did), DISTRICTS_DIR / f"{did}.png"))
        if not only or "facades" in only:
            for var in ("base", "mid"):
                jobs.append((gen_facade(did, var), FACADES_DIR / f"{did}_{var}.png"))
        if not only or "roofs" in only:
            jobs.append((gen_roof(did), ROOFS_DIR / f"{did}.png"))
        for img, path in jobs:
            total += 1
            msg = _save(img, path, args.force)
            if msg.startswith("write"):
                written += 1
            print(f"[{did}] {msg}")
    print(f"\n完成：{written}/{total} 张写入（其余 skip-if-exists）")


if __name__ == "__main__":
    main()
