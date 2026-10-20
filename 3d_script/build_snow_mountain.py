#!/usr/bin/env python3
"""
build_snow_mountain — 雪山（snow_mountain.glb）Blender headless 导出脚本
（批次 60「北缘雪山真实感重建」，重写批次 26 三段圆锥套）。

批次 60 真实形态调研结论（§27.0-1，动手前检索的真实规律）：
  - 山形：不规则脊线 = 主峰 + 侧峰 + 刃脊，山坳为侵蚀沟槽，**不是圆锥**。
    参照北京周边中山（小五台/海坨，相对高差 200~500 m，与本资产前端
    scale 6~10 × 基准 25 m = 150~250 m 视觉高差同量级）。
  - 垂直带谱按**海拔 + 噪声**渐变：山脚针叶林（远看深墨绿近黑）→ 林线以上
    草甸/裸岩（冷灰）→ 雪线以上积雪（近白微蓝）；雪线是**破碎锯齿**，
    迎风脊棱雪薄见岩、沟槽雪厚，雪不盖陡崖（坡度门）。
  - 基座有裙坡/倒石锥：底部足迹比中腰宽，与自然地面平滑过渡。

几何（世界单位，1 单位 = 10 m，Blender Z-up，pivot 地面中心 (0,0,0)）：
  - 65×65 高度场网格，域 [−1.5, 1.5]²；多峰高斯叠加（主峰 2.5u）+
    径向裙坡（r 1.5u 处归零）+ 双八度确定性值噪声（整数哈希，禁随机）。
  - 逐四边面分带（噪声破碎雪线/林线 + 坡度门 + 脊棱见岩）→ 三网格节点：
    SnowMt_Forest（林/草甸双色）/ SnowMt_Rock / SnowMt_Snow，
    材质名 `<节点>_Mat`（apply_pbr 自动派生，遵守昼夜调制同名约定）。
  - flat shading 低多边形（面法线），与旧资产风格一致但山形真实。

契约不变量（NorthMountains.tsx 零改动）：
  - max(h) = 2.5u（25 m 基准高，前端 scale 6~10 → 150~250 m 视觉山脊）；
  - minY = 0 贴地、X/Z 居中、bbox ≈ 3.0×2.5×3.0（旧 2.8×2.5×2.8，裙坡略宽 7%）。

调用：
  cd 3d_script && blender --background --python build_snow_mountain.py -- <输出.glb>
"""
import bpy
import math
import os
import sys

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (  # noqa: E402
    reset_scene, set_unit_meters,
    apply_pbr, CITY_PALETTE, export_glb,
)

# ── 常量 ────────────────────────────────────────────────────────────────────
GRID_N = 65                # 每边顶点数（65×65 顶点 / 64×64 四边面）
HALF = 1.35                # 域半宽（世界单位；足迹直径 2.7u ≈ 27 m 裙坡）
H_MAX = 2.5                # 基准高峰（世界单位 = 25 m，批次 26 契约）

# 多峰：主峰 + 西南侧峰 + 东南丘。
# (x, z, 峰高, σx, σz, 旋转角°) —— 椭圆高斯（非圆），拉出**脊线**而非圆穹：
# 主峰长轴东北-西南向，侧峰/丘各取不同轴向，多峰交叠成不规则山脊。
PEAKS = [
    (-0.05, 0.02, H_MAX, 0.46, 0.36, 20.0),
    (-0.58, 0.28, 1.75, 0.34, 0.22, -35.0),
    (0.50, -0.42, 1.15, 0.30, 0.20, 55.0),
]
SKIRT_R0, SKIRT_R1 = 0.90, HALF   # 裙坡：r≤0.9 全高，边缘平滑衰减至 0

# 分带阈值（hn = h / H_MAX 归一化高度；n1/n2 为 ±1 值噪声）
SNOW_ABS = 0.80            # 绝对雪线（之上全雪）
SNOW_BASE = 0.66           # 噪声雪线基准
SNOW_NOISE = 0.14          # 雪线破碎振幅
SNOW_SLOPE_MAX = 1.7       # 坡度门：陡崖不出雪
ROCK_BASE = 0.44           # 林线（之上无植被，岩/雪）——压低保留宽岩带
ROCK_NOISE = 0.13
RIDGE_LIFT = 0.10          # 脊棱高出邻域此值 → 见岩
MEADOW_H = 0.38            # 之上森林渐变为高山草甸（双色分网格）


# ── 确定性值噪声（整数哈希 + smoothstep 插值；禁 Math.random 同源约定）──
def _hash(ix: int, iz: int, seed: int) -> float:
    """整数格点 → [0,1) 伪随机（FNV 风格混合，纯确定性）。"""
    h = (ix * 374761393 + iz * 668265263 + seed * 1442695041) & 0xFFFFFFFF
    h = (h ^ (h >> 13)) * 1274126177 & 0xFFFFFFFF
    h = h ^ (h >> 16)
    return (h & 0xFFFFFF) / 16777216.0


def _smooth(t: float) -> float:
    return t * t * (3.0 - 2.0 * t)


def noise2(x: float, z: float, seed: int) -> float:
    """双线性 smoothstep 插值值噪声，返回 [−1, 1]。"""
    ix, iz = math.floor(x), math.floor(z)
    fx, fz = x - ix, z - iz
    a = _hash(ix, iz, seed)
    b = _hash(ix + 1, iz, seed)
    c = _hash(ix, iz + 1, seed)
    d = _hash(ix + 1, iz + 1, seed)
    ux, uz = _smooth(fx), _smooth(fz)
    return ((a * (1 - ux) + b * ux) * (1 - uz) + (c * (1 - ux) + d * ux) * uz) * 2.0 - 1.0


def fbm2(x: float, z: float, seed: int) -> float:
    """双八度：大形 + 细节，返回 ≈[−1, 1]。"""
    return 0.72 * noise2(x, z, seed) + 0.28 * noise2(x * 2.3 + 7.7, z * 2.3 + 3.1, seed + 101)


# ── 高度场 ──────────────────────────────────────────────────────────────────
def height(x: float, z: float) -> float:
    h = 0.0
    for px, pz, amp, sx, sz, rot_deg in PEAKS:
        # 逆旋转到峰局部系，椭圆高斯：拉出脊线而非圆穹
        a = math.radians(rot_deg)
        ca, sa = math.cos(a), math.sin(a)
        dx, dz = x - px, z - pz
        lx = ca * dx + sa * dz
        lz = -sa * dx + ca * dz
        h += amp * math.exp(-(lx * lx / (2.0 * sx * sx) + lz * lz / (2.0 * sz * sz)))
    # 裙坡/倒石锥：r > SKIRT_R0 后高斯裙衰减（凹形过渡，如真实山麓；
    # 旧 smoothstep 线性截断是凸形"火山裙"——批次 60 v2 验收结论）
    r = math.hypot(x, z)
    if r > SKIRT_R0:
        h *= math.exp(-((r - SKIRT_R0) / 0.28) ** 2)
    # 双八度噪声：随高度放大幅度（上段脊线破碎感更强，山脚少扰动保裙坡干净）
    if h > 1e-6:
        n = fbm2(x * 2.6, z * 2.6, 7)
        h *= 1.0 + 0.16 * n * (0.55 + 0.45 * min(1.0, h / H_MAX))
    return h


def build_mountain():
    reset_scene()
    set_unit_meters()

    n = GRID_N
    step = (2.0 * HALF) / (n - 1)
    # 顶点高度表
    hs = [[0.0] * n for _ in range(n)]
    for j in range(n):
        z = -HALF + j * step
        for i in range(n):
            x = -HALF + i * step
            hs[j][i] = height(x, z)

    # 归一化：峰顶 = H_MAX（前端 scale 契约）
    peak = max(max(row) for row in hs)
    scale = H_MAX / peak
    for j in range(n):
        for i in range(n):
            hs[j][i] *= scale

    def slope_at(i: int, j: int) -> float:
        """归一化坡度（Δh/Δx，网格间距 step）。"""
        i0, i1 = max(0, i - 1), min(n - 1, i + 1)
        j0, j1 = max(0, j - 1), min(n - 1, j + 1)
        dhx = (hs[j][i1] - hs[j][i0]) / ((i1 - i0) * step)
        dhz = (hs[j1][i] - hs[j0][i]) / ((j1 - j0) * step)
        return math.hypot(dhx, dhz) / H_MAX * 2.5  # 折算到归一化坡度量级

    # ── 逐四边面分带 ──
    zone_faces = {'forest': [], 'meadow': [], 'rock': [], 'snow': []}
    for j in range(n - 1):
        for i in range(n - 1):
            x0, z0 = -HALF + i * step, -HALF + j * step
            x1, z1 = x0 + step, z0 + step
            xc, zc = (x0 + x1) / 2.0, (z0 + z1) / 2.0
            h00, h10 = hs[j][i], hs[j][i + 1]
            h01, h11 = hs[j + 1][i], hs[j + 1][i + 1]
            hc = (h00 + h10 + h01 + h11) / 4.0
            if hc < 0.02:
                continue  # 裙坡边缘以下不建面（与地表衔接靠裙坡自身）
            hn = hc / H_MAX
            # 山麓带（r > 1.08）：植被退场，裸岩倒石锥接地 —— 消除"绿色圆毯"感
            rc = math.hypot(xc, zc)
            n1 = fbm2(xc * 2.6, zc * 2.6, 7)
            n2 = noise2(xc * 4.1 + 3.3, zc * 4.1 + 9.2, 23)
            slope = slope_at(i, j)
            # 脊棱因子：面心为 3×3 邻域局部最高且显著高出 → 见岩
            local = [hs[jj][ii] for jj in range(max(0, j - 1), min(n, j + 2))
                     for ii in range(max(0, i - 1), min(n, i + 2))]
            is_ridge = hc >= max(local) - 1e-9 and (hc - min(local)) > RIDGE_LIFT
            snow_line = SNOW_BASE + SNOW_NOISE * n2
            is_snow = (hn > SNOW_ABS
                       or (hn > snow_line and slope <= SNOW_SLOPE_MAX and not is_ridge))
            foothill = rc > 0.98
            if is_snow:
                zone = 'snow'
            elif hn > ROCK_BASE + ROCK_NOISE * n2 or is_ridge or foothill:
                zone = 'rock'
            elif hn > MEADOW_H:
                zone = 'meadow'   # 林线附近草坡（亮黄绿），之下密林（深墨绿）
            else:
                zone = 'forest'
            zone_faces[zone].append((i, j))

    # ── 分区建网格（flat shading：逐面法线）──
    import bmesh
    mats = {
        'forest': (CITY_PALETTE['forest_alpine'], 0.92),
        'meadow': (CITY_PALETTE['meadow_alpine'], 0.95),
        'rock': (CITY_PALETTE['rock_alpine'], 0.97),
        'snow': (CITY_PALETTE['snow_cap'], 0.55),
    }
    node_names = {'forest': 'SnowMt_Forest', 'meadow': 'SnowMt_Meadow',
                  'rock': 'SnowMt_Rock', 'snow': 'SnowMt_Snow'}

    for zone, faces in zone_faces.items():
        if not faces:
            continue
        bm = bmesh.new()
        verts = {}
        for (i, j) in faces:
            for (vi, vj) in ((i, j), (i + 1, j), (i + 1, j + 1), (i, j + 1)):
                if (vi, vj) not in verts:
                    co = (-HALF + vi * step, -HALF + vj * step, hs[vj][vi])
                    verts[(vi, vj)] = bm.verts.new(co)
        for (i, j) in faces:
            quad = [verts[(i, j)], verts[(i + 1, j)],
                    verts[(i + 1, j + 1)], verts[(i, j + 1)]]
            bm.faces.new(quad)
        mesh = bpy.data.meshes.new(node_names[zone])
        bm.to_mesh(mesh)
        bm.free()
        mesh.update()
        obj = bpy.data.objects.new(node_names[zone], mesh)
        bpy.context.scene.collection.objects.link(obj)
        color, rough = mats[zone]
        apply_pbr(obj, color, rough=rough, metal=0.0)

    print('[build] faces forest=%d meadow=%d rock=%d snow=%d' % (
        len(zone_faces['forest']), len(zone_faces['meadow']),
        len(zone_faces['rock']), len(zone_faces['snow'])), flush=True)


if __name__ == '__main__':
    build_mountain()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/snow_mountain.glb'
    export_glb(out_path)
    print(f'[size] {os.path.getsize(out_path)} bytes', flush=True)
