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

  v2.14 地表环境 × 3 张（14-3D城市渲染深化 · 阶段 G）：
    ground/grass_tile.png       512×512  可平铺草地（中央公园 / 河岸草皮）
    ground/plaza_tile.png       512×512  可平铺广场石板（交通枢纽 / 金融 CBD 小广场）
    ground/water_tile.png       512×512  可平铺水面（城市运河 / 物流港港池，纵向滚动）

  v2.15 天空与城市底色 × 2 张（16-3D城市WebGL质感与城市补全 · 阶段 U/R）：
    sky/cloud_puff.png          256×128  RGBA 云朵 soft puff（CloudLayer Billboard）
    ground/urban_base.png       512×512  可平铺城市底色（Ground 基底，替代满城沥青）

约定：
  - 纯 Pillow，不依赖 python-generate-image-tool 子模块、不访问网络。
  - 确定性 seed = FNV-1a(文件名)，重跑产物字节一致。
  - 幂等 skip-if-exists（AI 轨道已生成的文件绝不覆盖）；--force 强制覆盖。
  - --only districts,facades,roofs,ground 生成子集。

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
    from PIL import Image, ImageChops, ImageDraw, ImageFilter
except ImportError:  # pragma: no cover
    sys.exit("需要 Pillow：pip install 'Pillow>=10'")

BASE_DIR = Path(__file__).resolve().parent
WEALTH_DIR = BASE_DIR.parent / "ClientWeb" / "src" / "assets" / "images" / "wealth"
DISTRICTS_DIR = WEALTH_DIR / "districts"
FACADES_DIR = WEALTH_DIR / "facades"
ROOFS_DIR = WEALTH_DIR / "roofs"
GROUND_DIR = WEALTH_DIR / "ground"
SKY_DIR = WEALTH_DIR / "sky"

DISTRICT_SIZE = (1024, 1024)
FACADE_SIZE = (512, 1024)
ROOF_SIZE = (512, 512)
GROUND_SIZE = (512, 512)

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


def _tileable_noise(img: Image.Image, rng: random.Random, strength: int = 12) -> Image.Image:
    """wrap-safe 亮度噪声：32×32 噪声块 BICUBIC 放大（像素块自带连续性）后叠加。"""
    w, h = img.size
    noise = Image.new("RGB", (32, 32))
    px = noise.load()
    for y in range(32):
        for x in range(32):
            v = 128 + rng.randint(-strength, strength)
            px[x, y] = (v, v, v)
    noise = noise.resize((w, h), Image.Resampling.BICUBIC)
    return ImageChops.add(img, noise, scale=1.0, offset=-128)


def gen_grass_tile() -> Image.Image:
    """可平铺草地：底色 + wrap-safe 斑块噪声 + 草叶 speckle。"""
    w, h = GROUND_SIZE
    rng = rng_for("ground:grass_tile")
    img = Image.new("RGB", (w, h), (63, 122, 58))  # #3f7a3a
    d = ImageDraw.Draw(img)
    # 低频斑块（wrap：用 5×5 平铺坐标采样中心，出界部分映射回图内）
    for _ in range(46):
        x, y = rng.randint(0, w), rng.randint(0, h)
        r = rng.randint(18, 64)
        shade = (47, 95, 44) if rng.random() < 0.5 else (92, 154, 79)
        for ox in (-w, 0, w):
            for oy in (-h, 0, h):
                d.ellipse([x + ox - r, y + oy - r, x + ox + r, y + oy + r], fill=shade)
    img = img.filter(ImageFilter.GaussianBlur(6))
    d = ImageDraw.Draw(img)
    # 草叶短线（wrap-safe：起止点越界时补画对侧）
    for _ in range(800):
        x, y = rng.randint(0, w), rng.randint(0, h)
        ln = rng.randint(2, 5)
        ang = rng.uniform(-0.5, 0.5)  # ±30°
        x2 = x + ln * math.cos(ang)
        y2 = y + ln * math.sin(ang)
        col = (120, 175, 95) if rng.random() < 0.5 else (52, 100, 48)
        for ox in (-w, 0, w):
            for oy in (-h, 0, h):
                d.line([(x + ox, y + oy), (x2 + ox, y2 + oy)], fill=col, width=1)
    return _tileable_noise(img, rng, strength=10)


def gen_plaza_tile() -> Image.Image:
    """可平铺广场石板：4×4 网格缝（128px/格整除 512）+ 单石板亮度抖动。"""
    w, h = GROUND_SIZE
    rng = rng_for("ground:plaza_tile")
    cell = 128  # 512 / 4 —— 整除保证平铺
    img = Image.new("RGB", (w, h), (154, 161, 171))  # #9aa1ab
    d = ImageDraw.Draw(img)
    for gy in range(4):
        for gx in range(4):
            jitter = rng.randint(-12, 12)
            tone = (
                max(0, min(255, 154 + jitter)),
                max(0, min(255, 161 + jitter)),
                max(0, min(255, 171 + jitter)),
            )
            d.rectangle([gx * cell + 2, gy * cell + 2, (gx + 1) * cell - 3, (gy + 1) * cell - 3], fill=tone)
    # 对角拼花（中心 2 格）
    for gx, gy in ((1, 1), (2, 2)):
        d.line(
            [(gx * cell + 8, gy * cell + 8), ((gx + 1) * cell - 8, (gy + 1) * cell - 8)],
            fill=(124, 131, 141), width=4,
        )
    # 网格缝
    seam = (124, 131, 141)  # #7c838d
    for i in range(4):
        d.line([(0, i * cell), (w, i * cell)], fill=seam, width=2)
        d.line([(i * cell, 0), (i * cell, h)], fill=seam, width=2)
    return _tileable_noise(img, rng, strength=6)


def gen_water_tile() -> Image.Image:
    """可平铺水面：低频渐变 + 正弦波纹（相位 wrap-safe）+ 高光 speckle。"""
    w, h = GROUND_SIZE
    rng = rng_for("ground:water_tile")
    img = Image.new("RGB", (w, h), (28, 74, 102))  # #1c4a66
    d = ImageDraw.Draw(img)
    # 低频色带（竖向两条偏亮水色，wrap-safe 椭圆铺 3×3）
    for _ in range(24):
        x, y = rng.randint(0, w), rng.randint(0, h)
        rx, ry = rng.randint(40, 120), rng.randint(24, 70)
        shade = (42, 106, 138) if rng.random() < 0.6 else (34, 88, 118)
        for ox in (-w, 0, w):
            for oy in (-h, 0, h):
                d.ellipse([x + ox - rx, y + oy - ry, x + ox + rx, y + oy + ry], fill=shade)
    img = img.filter(ImageFilter.GaussianBlur(8))
    d = ImageDraw.Draw(img)
    # 波纹：60 条正弦横纹，波长 64px（512/8 整周期 → 纵横双向 wrap-safe）
    for i in range(60):
        y0 = rng.randint(0, h)
        amp = rng.uniform(3, 6)
        phase = rng.uniform(0, math.tau)
        col = (255, 255, 255) if rng.random() < 0.5 else (180, 220, 235)
        alpha_hint = rng.randint(20, 46)  # 仅亮度控制近似透明度
        pts = []
        for x in range(0, w + 1, 4):
            yy = y0 + amp * math.sin(phase + x / 64 * math.tau)
            yy = ((yy % h) + h) % h
            pts.append((x, yy))
        # 亮度近似：混白低 alpha → 直接画淡色
        faint = tuple(int(c * alpha_hint / 255 + img.getpixel((pts[0][0], int(pts[0][1]) % h))[k] * (1 - alpha_hint / 255)) for k, c in enumerate(col))
        d.line(pts, fill=faint, width=rng.randint(1, 2), joint="curve")
    _noise_speckle(d, rng, w, h, (220, 240, 250), 50, r_max=1)
    return _tileable_noise(img, rng, strength=5)


def gen_urban_base() -> Image.Image:
    """可平铺城市底色（16 · 阶段 R）：中性混凝土/土色 + 修补补丁 + wrap-safe 噪点。

    语义：全城「建成区底色」，城区底板 / 道路 / 广场压在其上——替代 v1 的满城沥青。
    """
    w, h = GROUND_SIZE
    rng = rng_for("ground:urban_base")
    img = Image.new("RGB", (w, h), (58, 63, 71))  # #3a3f47
    d = ImageDraw.Draw(img)
    # 低频色斑（更亮的水泥面 / 更暗的泥土面，wrap：3×3 平铺坐标补画）
    for _ in range(30):
        x, y = rng.randint(0, w), rng.randint(0, h)
        rx, ry = rng.randint(50, 150), rng.randint(40, 110)
        shade = (66, 72, 80) if rng.random() < 0.55 else (50, 54, 61)
        for ox in (-w, 0, w):
            for oy in (-h, 0, h):
                d.ellipse([x + ox - rx, y + oy - ry, x + ox + rx, y + oy + ry], fill=shade)
    img = img.filter(ImageFilter.GaussianBlur(10))
    d = ImageDraw.Draw(img)
    # 修补路面补丁（12 处近矩形淡色块，低对比）
    for _ in range(12):
        x, y = rng.randint(0, w), rng.randint(0, h)
        bw, bh = rng.randint(30, 90), rng.randint(24, 60)
        patch = (70, 75, 83)
        for ox in (-w, 0, w):
            for oy in (-h, 0, h):
                d.rectangle([x + ox, y + oy, x + ox + bw, y + oy + bh], fill=patch)
    # 细砾 speckle
    _noise_speckle(d, rng, w, h, (78, 84, 92), 220, r_max=1)
    _noise_speckle(d, rng, w, h, (44, 48, 55), 160, r_max=1)
    return _tileable_noise(img, rng, strength=7)


def gen_cloud_puff() -> Image.Image:
    """云朵 soft puff（16 · 阶段 U）：RGBA 透明底 + 多团白椭圆 + 高斯模糊 + 底部渐隐。

    消费端 CloudLayer 以 Billboard 叠 3-4 层；缺图时组件走白色扁球兜底。
    """
    w, h = (256, 128)
    rng = rng_for("sky:cloud_puff")
    img = Image.new("RGBA", (w, h), (255, 255, 255, 0))
    d = ImageDraw.Draw(img)
    # 主团：中心密集 6-9 团椭圆，边缘 2-3 团稀疏
    blobs = []
    for i in range(rng.randint(6, 9)):
        cx = w * 0.5 + rng.uniform(-0.22, 0.22) * w
        cy = h * 0.48 + rng.uniform(-0.18, 0.12) * h
        rx = rng.uniform(0.10, 0.16) * w
        ry = rx * rng.uniform(0.55, 0.75)
        alpha = rng.randint(110, 165)
        blobs.append((cx, cy, rx, ry, alpha))
    for i in range(3):
        cx = w * (0.14 + 0.72 * rng.random())
        cy = h * (0.38 + 0.2 * rng.random())
        rx = rng.uniform(0.06, 0.10) * w
        ry = rx * 0.6
        blobs.append((cx, cy, rx, ry, rng.randint(70, 110)))
    for cx, cy, rx, ry, alpha in blobs:
        d.ellipse([cx - rx, cy - ry, cx + rx, cy + ry], fill=(252, 253, 255, alpha))
    img = img.filter(ImageFilter.GaussianBlur(7))
    # 底部 15% alpha 渐隐（云底平感）+ 顶部 8% 轻微渐隐
    px = img.load()
    for y in range(h):
        if y > h * 0.85:
            k = 1.0 - (y - h * 0.85) / (h * 0.15)
        elif y < h * 0.08:
            k = y / (h * 0.08) * 0.85 + 0.15
        else:
            continue
        for x in range(w):
            r, g, b, a = px[x, y]
            px[x, y] = (r, g, b, int(a * k))
    return img


def _save(img: Image.Image, path: Path, force: bool) -> str:
    if path.exists() and not force:
        return f"skip  {path.name}"
    path.parent.mkdir(parents=True, exist_ok=True)
    img.save(path, "PNG")
    return f"write {path.name} {img.size[0]}x{img.size[1]}"


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--force", action="store_true", help="覆盖已存在文件")
    ap.add_argument("--only", default="", help="逗号分隔子集：districts,facades,roofs,ground,sky")
    args = ap.parse_args()
    only = {s.strip() for s in args.only.split(",") if s.strip()}

    total = written = 0
    # 天空贴图（16-3D城市WebGL质感与城市补全 · 阶段 U）
    if not only or "sky" in only:
        total += 1
        msg = _save(gen_cloud_puff(), SKY_DIR / "cloud_puff.png", args.force)
        if msg.startswith("write"):
            written += 1
        print(f"[sky] {msg}")
    # 地表环境贴图（14-3D城市渲染深化 · 阶段 G；16 阶段 R 追加 urban_base）
    if not only or "ground" in only:
        ground_jobs = [
            (gen_grass_tile(), GROUND_DIR / "grass_tile.png"),
            (gen_plaza_tile(), GROUND_DIR / "plaza_tile.png"),
            (gen_water_tile(), GROUND_DIR / "water_tile.png"),
            (gen_urban_base(), GROUND_DIR / "urban_base.png"),
        ]
        for img, path in ground_jobs:
            total += 1
            msg = _save(img, path, args.force)
            if msg.startswith("write"):
                written += 1
            print(f"[ground] {msg}")
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
