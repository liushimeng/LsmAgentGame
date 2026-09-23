#!/usr/bin/env python3
"""虚拟城市 — PBR 贴图生成器（轨道 2 · 无外部 API 依赖）。

产物（方案：lag_docs/虚拟城市/已实现/18-3D城市PBR材质与真实城市冲刺/03-资产方案-v1.md）：

  派生贴图（**从既有颜色贴图求高度场 → 法线/粗糙度**，自动与颜色对齐，无错位风险）：
    pbr/facades/<id>_<variant>_n.png    512×1024  楼宇立面法线（窗洞内凹 / 窗框凸起）
    pbr/facades/<id>_<variant>_r.png    512×1024  立面粗糙度（玻璃滑 / 墙体糙）
    pbr/roofs/<id>_n.png  pbr/roofs/<id>_r.png    512×512   屋顶
    pbr/streets/<name>_n.png  pbr/streets/<name>_r.png      512×512（人行道 512×256）
    pbr/ground/<name>_n.png   pbr/ground/<name>_r.png       512×512（water 只出 _n）
    pbr/districts/<id>_n.png                            512×512  城区底板（1024 下采样）

  合成贴图（与颜色贴图无关的**可平铺**通用材质）：
    pbr/synth/water_n.png         512×512  水面双层波纹法线（滚动用）
    pbr/synth/concrete_n.png      512×512  清水混凝土（裙楼 / 桥墩 / 挡墙）
    pbr/synth/concrete_r.png
    pbr/synth/brick_n.png         512×512  砖墙（老城 / 警局消防）
    pbr/synth/brick_r.png
    pbr/synth/metal_deck_n.png    512×512  压型钢板（厂房 / 集装箱 / 岸吊）
    pbr/synth/metal_deck_r.png
    pbr/synth/tile_roof_n.png     512×512  瓦屋面（别墅 / 凉亭）
    pbr/synth/tile_roof_r.png
    pbr/synth/asphalt_wear_n.png  512×512  轮胎磨耗叠加（主干道补强）
    pbr/synth/foliage_n.png       512×512  树冠起伏（TreeV3）
    pbr/synth/foliage_r.png

约定（与 procedural_city_textures.py 完全一致，两脚本可并列维护）：
  - 纯 Pillow，不依赖 python-generate-image-tool 子模块、不访问网络。
  - 确定性 seed = FNV-1a(文件名)，重跑产物**字节一致**。
  - 幂等 skip-if-exists；--force 强制覆盖；--only 指定子集。
  - 派生贴图只读取既有颜色贴图；颜色贴图缺失时自动跳过（不报错）。

用法：
  python3 3d_script/procedural_pbr_maps.py                 # 补齐缺失
  python3 3d_script/procedural_pbr_maps.py --force         # 全部重生成
  python3 3d_script/procedural_pbr_maps.py --only synth    # 只跑合成材质
"""

from __future__ import annotations

import argparse
import math
import sys
from pathlib import Path

try:
    from PIL import Image, ImageFilter
except ImportError:  # pragma: no cover
    sys.exit("需要 Pillow：pip install 'Pillow>=10'")

BASE_DIR = Path(__file__).resolve().parent
WEALTH_DIR = BASE_DIR.parent / "ClientWeb" / "src" / "assets" / "images" / "wealth"
PBR_DIR = WEALTH_DIR / "pbr"
SYNTH_DIR = PBR_DIR / "synth"

# 合成贴图统一尺寸（可平铺；法线/粗糙度对分辨率不敏感，512 足够）
SYNTH_SIZE = (512, 512)

# 派生贴图的降采样倍率：法线保半分辨率（低频起伏，线性过滤足够）、粗糙度保 1/4
# （粗糙度是最低频通道）。目的是压住仓库体积与 GPU 显存——
# 32 张 512×1024 RGBA 法线 = 64MB 显存，18MB 仓库；半分辨率后 ≈ 16MB / 4.5MB。
DERIVED_N_SCALE = 2
DERIVED_R_SCALE = 4


def fnv1a(s: str) -> int:
    h = 2166136261
    for ch in s:
        h ^= ord(ch)
        h = (h * 16777619) & 0xFFFFFFFF
    return h


# ── 高度场 → 切线空间法线（Sobel）─────────────────────────────────────────


def _luma(img: Image.Image) -> list[list[float]]:
    """RGB(A) → 亮度矩阵（0..1）。alpha 低的像素按背景中性灰处理，避免透明区乱凸。"""
    rgba = img.convert("RGBA")
    w, h = rgba.size
    px = rgba.load()
    out = [[0.0] * w for _ in range(h)]
    for y in range(h):
        row = out[y]
        for x in range(w):
            r, g, b, a = px[x, y]
            lum = (0.299 * r + 0.587 * g + 0.114 * b) / 255.0
            k = a / 255.0
            row[x] = lum * k + 0.5 * (1.0 - k)
    return out


def _box_blur(src: list[list[float]], radius: int) -> list[list[float]]:
    """可平铺盒式模糊（半径 radius，两趟）。"""
    if radius <= 0:
        return [row[:] for row in src]
    h = len(src)
    w = len(src[0])
    tmp = [[0.0] * w for _ in range(h)]
    for y in range(h):
        row = src[y]
        out = tmp[y]
        for x in range(w):
            acc = 0.0
            for d in range(-radius, radius + 1):
                acc += row[(x + d) % w]
            out[x] = acc / (2 * radius + 1)
    out = [[0.0] * w for _ in range(h)]
    for x in range(w):
        for y in range(h):
            acc = 0.0
            for d in range(-radius, radius + 1):
                acc += tmp[(y + d) % h][x]
            out[y][x] = acc / (2 * radius + 1)
    return out


def _normalize01(mat: list[list[float]]) -> list[list[float]]:
    lo = min(min(row) for row in mat)
    hi = max(max(row) for row in mat)
    if hi - lo < 1e-9:
        return [[0.5] * len(mat[0]) for _ in mat]
    k = 1.0 / (hi - lo)
    return [[(v - lo) * k for v in row] for row in mat]


def height_from_color(img: Image.Image, mode: str) -> list[list[float]]:
    """颜色贴图 → 高度场 0..1。

    mode 决定「亮 = 高」还是「亮 = 低」以及细节锐度：
      wall    立面：墙亮=高、窗暗=低（内凹），unsharp 让窗框立起来
      ground  地面：铺装缝隙暗=低
      roof    屋顶：设备/瓦楞亮=高
      street  路面：标线亮=**低**（漆膜比沥青薄），采用反转
    """
    lum = _luma(img)
    smooth = _box_blur(lum, 2)
    detail = [[lum[y][x] - smooth[y][x] for x in range(len(lum[0]))] for y in range(len(lum))]

    if mode == "street":
        # 标线（亮）略凸一点点即可，主要靠颗粒噪点
        base = [[1.0 - v for v in row] for row in _normalize01(smooth)]
        sharp = 0.45
    elif mode == "roof":
        base = _normalize01(smooth)
        sharp = 0.75
    elif mode == "ground":
        base = _normalize01(smooth)
        sharp = 0.55
    else:  # wall
        base = _normalize01(smooth)
        sharp = 1.15

    h = [[base[y][x] + sharp * detail[y][x] * 4.0 for x in range(len(base[0]))] for y in range(len(base))]
    return _normalize01(h)


def normal_from_height(
    height: list[list[float]],
    strength: float,
) -> Image.Image:
    """高度场 → 切线空间法线 RGB（= (nx, ny, nz) × 0.5 + 0.5），A=255。"""
    h = len(height)
    w = len(height[0])
    out = Image.new("RGBA", (w, h))
    px = out.load()
    for y in range(h):
        ym = (y - 1) % h
        yp = (y + 1) % h
        for x in range(w):
            xm = (x - 1) % w
            xp = (x + 1) % w
            # Sobel（可平铺）
            dx = (
                height[ym][xp] + 2.0 * height[y][xp] + height[yp][xp]
                - height[ym][xm] - 2.0 * height[y][xm] - height[yp][xm]
            )
            dy = (
                height[yp][xm] + 2.0 * height[yp][x] + height[yp][xp]
                - height[ym][xm] - 2.0 * height[ym][x] - height[ym][xp]
            )
            nx = -dx * strength
            ny = -dy * strength
            nz = 1.0
            inv = 1.0 / math.sqrt(nx * nx + ny * ny + nz * nz)
            px[x, y] = (
                int(round((nx * inv * 0.5 + 0.5) * 255)),
                int(round((ny * inv * 0.5 + 0.5) * 255)),
                int(round((nz * inv * 0.5 + 0.5) * 255)),
                255,
            )
    return out


def roughness_from_color(img: Image.Image, mode: str) -> Image.Image:
    """颜色贴图 → 粗糙度灰度图（R=G=B=rough，three 只采 G 通道）。"""
    lum = _normalize01(_box_blur(_luma(img), 1))
    w, h = len(lum[0]), len(lum)
    out = Image.new("L", (w, h))  # 灰度 PNG：three 读 G 通道，体积比 RGB 小 3×
    px = out.load()

    def clamp01(v: float) -> float:
        return 0.0 if v < 0.0 else (1.0 if v > 1.0 else v)

    for y in range(h):
        for x in range(w):
            v = lum[y][x]
            if mode == "wall":
                # 玻璃（暗、偏冷）滑；墙体（亮）糙
                rough = 0.28 + 0.58 * v
            elif mode == "street":
                # 沥青糙；标线漆面略滑
                rough = 0.92 - 0.42 * v
            elif mode == "roof":
                rough = 0.55 + 0.30 * v
            else:  # ground
                rough = 0.72 + 0.22 * v
            px[x, y] = int(round(clamp01(rough) * 255))
    return out


# ── 派生贴图（读既有颜色贴图）──────────────────────────────────────────

# (颜色贴图 glob 相对 wealth/ 路径, pbr 子目录, 高度模式, 法线强度, 法线降采样倍率)
# districts 是 1024² 俯视底板（远景、低频），法线取 1/4（256²）——单张 ~200KB，
# 否则 16 张 512² 法线就吃掉 6.8MB，性价比最低。
DERIVED_JOBS = [
    ("facades/*.png", "facades", "wall", 2.4, DERIVED_N_SCALE),
    ("roofs/*.png", "roofs", "roof", 2.6, DERIVED_N_SCALE),
    ("streets/*.png", "streets", "street", 2.0, DERIVED_N_SCALE),
    ("ground/*.png", "ground", "ground", 1.8, DERIVED_N_SCALE),
    ("districts/*.png", "districts", "ground", 1.6, DERIVED_N_SCALE * 2),
]


def _shrink(img: Image.Image, factor: int) -> Image.Image:
    if factor <= 1:
        return img
    w, h = img.size
    return img.resize((max(1, w // factor), max(1, h // factor)), Image.Resampling.LANCZOS)


def _derive_one(
    src: Path,
    dst_dir: Path,
    mode: str,
    strength: float,
    n_scale: int,
    force: bool,
    log: list[str],
) -> None:
    stem = src.stem
    n_path = dst_dir / f"{stem}_n.png"
    r_path = dst_dir / f"{stem}_r.png"
    img = Image.open(src)

    if force or not n_path.exists():
        # 法线降采样：先出高度场再缩放（比先缩放再求梯度更稳，避免锯齿边缘假坡）
        height = height_from_color(img, mode)
        if n_scale > 1:
            h_img = Image.new("F", (len(height[0]), len(height)))
            h_img.putdata([height[y][x] for y in range(len(height)) for x in range(len(height[0]))])
            h_img = _shrink(h_img, n_scale)
            hw, hh = h_img.size
            flat = list(h_img.getdata())
            height = [[flat[y * hw + x] for x in range(hw)] for y in range(hh)]
        n_path.parent.mkdir(parents=True, exist_ok=True)
        normal_from_height(height, strength * n_scale).save(n_path, "PNG")
        log.append(f"write {n_path.relative_to(WEALTH_DIR)}")
    else:
        log.append(f"skip  {n_path.relative_to(WEALTH_DIR)}")

    # roughness 是最低频通道，1/4 分辨率足够；灰度 PNG 省 3× 体积
    if force or not r_path.exists():
        rough = roughness_from_color(_shrink(img, DERIVED_R_SCALE), mode)
        r_path.parent.mkdir(parents=True, exist_ok=True)
        rough.save(r_path, "PNG")
        log.append(f"write {r_path.relative_to(WEALTH_DIR)}")
    else:
        log.append(f"skip  {r_path.relative_to(WEALTH_DIR)}")


# ── 合成贴图（可平铺通用材质）──────────────────────────────────────────


def _tile_noise(w: int, h: int, cells: int, seed: int) -> list[list[float]]:
    """可平铺值噪声：cells×cells 网格随机控制点 + 周期性双线性插值。"""
    grid = [[0.0] * (cells + 1) for _ in range(cells + 1)]
    state = seed & 0xFFFFFFFF
    for j in range(cells):
        for i in range(cells):
            state = (1103515245 * state + 12345) & 0x7FFFFFFF
            grid[j][i] = (state % 10000) / 10000.0
    # 周期闭合
    for i in range(cells + 1):
        grid[cells][i] = grid[0][i]
    for j in range(cells + 1):
        grid[j][cells] = grid[j][0]

    out = [[0.0] * w for _ in range(h)]
    for y in range(h):
        fy = y / h * cells
        j0 = int(fy) % cells
        ty = fy - int(fy)
        ty = ty * ty * (3 - 2 * ty)  # smoothstep
        for x in range(w):
            fx = x / w * cells
            i0 = int(fx) % cells
            tx = fx - int(fx)
            tx = tx * tx * (3 - 2 * tx)
            v00 = grid[j0][i0]
            v10 = grid[j0][i0 + 1]
            v01 = grid[j0 + 1][i0]
            v11 = grid[j0 + 1][i0 + 1]
            out[y][x] = (v00 * (1 - tx) + v10 * tx) * (1 - ty) + (v01 * (1 - tx) + v11 * tx) * ty
    return out


def _fbm(w: int, h: int, seed: int, octaves: int = 4) -> list[list[float]]:
    acc = [[0.0] * w for _ in range(h)]
    amp = 1.0
    total = 0.0
    for o in range(octaves):
        layer = _tile_noise(w, h, 4 << o, seed + o * 7919)
        for y in range(h):
            for x in range(w):
                acc[y][x] += amp * layer[y][x]
        total += amp
        amp *= 0.5
    return _normalize01([[v / total for v in row] for row in acc])


def _save_gray(mat: list[list[float]], path: Path, force: bool, log: list[str]) -> None:
    if path.exists() and not force:
        log.append(f"skip  {path.relative_to(WEALTH_DIR)}")
        return
    h = len(mat)
    w = len(mat[0])
    img = Image.new("L", (w, h))
    px = img.load()
    for y in range(h):
        for x in range(w):
            px[x, y] = int(round(max(0.0, min(1.0, mat[y][x])) * 255))
    path.parent.mkdir(parents=True, exist_ok=True)
    img.save(path, "PNG")
    log.append(f"write {path.relative_to(WEALTH_DIR)}")


def gen_synth_water_n(force: bool, log: list[str]) -> None:
    """水面双层波纹法线：两个方向不同波长的正弦叠加 + fbm 扰动。"""
    w, h = SYNTH_SIZE
    seed = fnv1a("synth:water_n")
    turb = _fbm(w, h, seed, octaves=3)
    height = [[0.0] * w for _ in range(h)]
    for y in range(h):
        v = y / h * 2 * math.pi
        for x in range(w):
            u = x / w * 2 * math.pi
            wave = (
                0.55 * math.sin(3 * u + 1.0 * v)
                + 0.35 * math.sin(5 * v - 2.0 * u)
                + 0.22 * math.sin(8 * u + 7 * v)
            )
            height[y][x] = 0.5 + 0.18 * wave + 0.22 * (turb[y][x] - 0.5)
    height = _normalize01(height)
    n_path = SYNTH_DIR / "water_n.png"
    if n_path.exists() and not force:
        log.append(f"skip  {n_path.relative_to(WEALTH_DIR)}")
        return
    n_path.parent.mkdir(parents=True, exist_ok=True)
    normal_from_height(height, 2.0).save(n_path, "PNG")
    log.append(f"write {n_path.relative_to(WEALTH_DIR)}")


def gen_synth_concrete(force: bool, log: list[str]) -> None:
    """清水混凝土：细噪点 + 模板缝（每 128px 十字缝）。"""
    w, h = SYNTH_SIZE
    base = _fbm(w, h, fnv1a("synth:concrete"), octaves=5)
    height = [[0.5 + 0.35 * (base[y][x] - 0.5) for x in range(w)] for y in range(h)]
    for y in range(h):
        if y % 128 < 2:
            for x in range(w):
                height[y][x] *= 0.72
    for x in range(w):
        if x % 128 < 2:
            for y in range(h):
                height[y][x] *= 0.72
    _save_pair(height, "concrete", rough_lo=0.62, rough_hi=0.88, force=force, log=log)


def gen_synth_brick(force: bool, log: list[str]) -> None:
    """砖墙：错缝砖列 + 灰缝凹陷。砖 2:1，行高 32px、砖宽 64px。"""
    w, h = SYNTH_SIZE
    height = [[1.0] * w for _ in range(h)]
    row_h, brick_w, mortar = 32, 64, 3
    for y in range(h):
        row = y // row_h
        offset = (brick_w // 2) if (row % 2) else 0
        in_mortar_h = (y % row_h) < mortar
        for x in range(w):
            xs = (x + offset) % brick_w
            in_mortar_v = xs < mortar
            if in_mortar_h or in_mortar_v:
                height[y][x] = 0.55
            else:
                # 单砖微凸面（中心略高）
                bx = (xs - brick_w / 2) / (brick_w / 2)
                by = ((y % row_h) - row_h / 2) / (row_h / 2)
                height[y][x] = 0.85 + 0.12 * (1 - min(1.0, bx * bx + by * by))
    _save_pair(height, "brick", rough_lo=0.70, rough_hi=0.90, force=force, log=log)


def gen_synth_metal_deck(force: bool, log: list[str]) -> None:
    """压型钢板：纵向瓦楞（每 32px 一棱）+ 横向搭接缝。"""
    w, h = SYNTH_SIZE
    height = [[0.0] * w for _ in range(h)]
    for y in range(h):
        seam = 0.7 if (y % 128) < 3 else 1.0
        for x in range(w):
            rib = 0.5 + 0.45 * math.sin(x / w * 2 * math.pi * (w / 32))
            height[y][x] = (0.25 + 0.75 * rib) * seam
    height = _normalize01(height)
    _save_pair(height, "metal_deck", rough_lo=0.22, rough_hi=0.48, force=force, log=log)


def gen_synth_tile_roof(force: bool, log: list[str]) -> None:
    """瓦屋面：筒瓦行列（每 40px 半圆棱）+ 每行叠瓦缝。"""
    w, h = SYNTH_SIZE
    height = [[0.0] * w for _ in range(h)]
    for y in range(h):
        row = (y % 40) / 40.0
        overlap = 0.75 if row < 0.18 else 1.0  # 叠瓦缝
        for x in range(w):
            t = (x % 40) / 40.0
            ridge = math.sin(t * math.pi)  # 半圆筒瓦
            height[y][x] = (0.2 + 0.8 * ridge) * overlap * (0.9 + 0.1 * row)
    height = _normalize01(height)
    _save_pair(height, "tile_roof", rough_lo=0.55, rough_hi=0.85, force=force, log=log)


def gen_synth_asphalt_wear(force: bool, log: list[str]) -> None:
    """轮胎磨耗：两条纵向光带 + 细碎修补块（叠加在 asphalt 主贴图上）。"""
    w, h = SYNTH_SIZE
    base = _fbm(w, h, fnv1a("synth:asphalt_wear"), octaves=4)
    height = [[0.5 + 0.25 * (base[y][x] - 0.5) for x in range(w)] for y in range(h)]
    for y in range(h):
        for x in range(w):
            # 双轮迹带（相对路宽 0.28 / 0.72 处）
            for c in (0.28, 0.72):
                d = abs(x / w - c)
                if d < 0.10:
                    height[y][x] *= 1.0 - 0.18 * (1.0 - d / 0.10)
    height = _normalize01(height)
    _save_pair(height, "asphalt_wear", rough_lo=0.55, rough_hi=0.88, force=force, log=log)


def gen_synth_foliage(force: bool, log: list[str]) -> None:
    """树冠起伏：多尺度球状凸起（TreeV3 树冠用），粗糙度较高。"""
    w, h = SYNTH_SIZE
    base = _fbm(w, h, fnv1a("synth:foliage"), octaves=5)
    height = [[0.35 + 0.65 * (base[y][x] ** 1.4) for x in range(w)] for y in range(h)]
    _save_pair(height, "foliage", rough_lo=0.72, rough_hi=0.95, force=force, log=log)


def _save_pair(
    height: list[list[float]],
    name: str,
    rough_lo: float,
    rough_hi: float,
    force: bool,
    log: list[str],
) -> None:
    """高度 → 法线 + 由高度派生的粗糙度（凸=略滑、凹=略糙）。"""
    n_path = SYNTH_DIR / f"{name}_n.png"
    r_path = SYNTH_DIR / f"{name}_r.png"
    if force or not n_path.exists():
        n_path.parent.mkdir(parents=True, exist_ok=True)
        normal_from_height(_normalize01(height), 2.2).save(n_path, "PNG")
        log.append(f"write {n_path.relative_to(WEALTH_DIR)}")
    else:
        log.append(f"skip  {n_path.relative_to(WEALTH_DIR)}")
    if force or not r_path.exists():
        # 凹处积灰更糙，凸处磨亮更滑
        rough = [[rough_hi - (rough_hi - rough_lo) * height[y][x] for x in range(len(height[0]))] for y in range(len(height))]
        _save_gray(rough, r_path, force=True, log=log)
    else:
        log.append(f"skip  {r_path.relative_to(WEALTH_DIR)}")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--force", action="store_true", help="覆盖已存在文件")
    ap.add_argument(
        "--only",
        default="",
        help="逗号分隔子集：derive,synth,facades,roofs,streets,ground,districts",
    )
    args = ap.parse_args()
    only = {s.strip() for s in args.only.split(",") if s.strip()}
    derive_subs = {sub for _, sub, _, _, _ in DERIVED_JOBS}
    # --only districts 即可只跑城区底板派生；--only derive 跑全部派生
    want_synth = not only or "synth" in only
    want_derive = not only or "derive" in only or bool(only & derive_subs)

    log: list[str] = []

    # ── 合成材质（不依赖既有贴图，永远可跑）──
    if want_synth:
        gen_synth_water_n(args.force, log)
        gen_synth_concrete(args.force, log)
        gen_synth_brick(args.force, log)
        gen_synth_metal_deck(args.force, log)
        gen_synth_tile_roof(args.force, log)
        gen_synth_asphalt_wear(args.force, log)
        gen_synth_foliage(args.force, log)

    # ── 派生材质（读既有颜色贴图；缺源自动跳过）──
    if want_derive:
        for glob, sub, mode, strength, n_scale in DERIVED_JOBS:
            if only and "derive" not in only and sub not in only:
                continue
            srcs = sorted(WEALTH_DIR.glob(glob))
            if not srcs:
                print(f"[derive:{sub}] 无源贴图，跳过")
                continue
            dst = PBR_DIR / sub
            for src in srcs:
                _derive_one(src, dst, mode, strength, n_scale, args.force, log)

    written = sum(1 for m in log if m.startswith("write"))
    for m in log:
        print(m)
    print(f"\n完成：{written}/{len(log)} 张写入（其余 skip-if-exists）")


if __name__ == "__main__":
    main()
