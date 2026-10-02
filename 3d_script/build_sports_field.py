#!/usr/bin/env python3
"""
build_sports_field — 批次 47「体育场真实感」200 m 半圆式田径场（civic/sports_field.glb）。

── 重制理由：批次 18-AA 的程序化体育场是玩具，且放错了城区 ────────────────
  1) **形状不成立**：现状 = `TorusGeometry(u(28), u(4))` 压扁成椭圆 + `CircleGeometry(u(24))`
     **圆**内场。田径场是「两段直道 + 两个半圆」的**体育场形**，不是椭圆；而 r=28 m 的
     **圆**内场套不进任何标准足球场（足球场是矩形）。分道线 `RingGeometry(r±0.05)`
     还漏乘了 `u()`（半径按世界单位而非米写，批次 46 登记 L3）。
  2) **布点错位**：现状在 (-16.5, 22)（教育园区/医疗城之间），而 **(10, 40) 有个叫
     「体育新城」的区**（`virtualCity.ts:1551`），官方区简介明写「围绕体育场馆建设的新城
     板块」——一个以体育场命名、简介写着体育场、实际空无一馆的区。本批把体育场搬过去。

── §27.0-1 真实几何（结论先行，逐条依据见设计 47 §2.1）──────────────────
  闭式校核（厂家规范表 `S = π(r+x·d)² − πr² + l·2·d·x` 反解）：
      r = 20.00 m（最内道内沿）, 6 道 × 1.22 m ⇒ 外半径 27.32 m
      弯道总长 2πr = 125.66 m  ⇒  单侧直道 l = (200 − 125.66)/2 = 37.17 m
      跑道外接矩形 = (2×27.32 + 37.17) × (2×27.32) = 91.8 × 54.6 m = 5015 ㎡
      实际规划用地（含外侧安全区 + 起跑缓冲）≈ 100 × 60 m = 6000 ㎡（9 亩）
      ✓ 2×37.17 + 2π×20.00 = 200.00 m 精确闭合
  ⚠ **两个易错点**（本件都踩过）：
    (1) 别拿 400 m 场的 68.39 m 直道套 200 m 场（那是 r=36.5 时的值）——200 m 场单侧
        直道只有 37.17 m。上一批因此把它估成 83×55 m、误判「超出布点预算」。
    (2) `_stadium_point` 的参数是**直道半长 L**，而厂家表给的是**直道全长 l=37.17**。
        闭式 `l + 2(r+xd)` 里的 l 是全长 ⇒ L = 18.585。首版把 37.17 当半长用，
        跑道横向长了 37 m（bbox 138.6 m 而非 91.8 m），靠 verify 的 bbox 才抓到。

  | 部件 | 真实规格                        | 依据                       | 本件取值 |
  |------|---------------------------------|----------------------------|----------|
  | 跑道 | r=20 m，6 道 ×1.22 m，外半径 27.32 | 厂家规范表 / 面积公式      | 同       |
  | 内场 | 两直道间 74.34 × 2r=40 m         | 由跑道几何导出             | 七人制人造草 60×32 m     |
  | 足球 | 200 m 场放不下 11 人制(105×68)   | 74.34 < 105               | FIFA 7-a-side 50~70×32~40 |
  | 看台 | 排距 ≥0.75 m；C 值 ≥0.06/排     | JGJ 31-2003                | 3 排 ×0.85 m，级高 0.45   |
  | 栏杆 | ≥0.90 m，室外看台**后部 ≥1.10** | JGJ 31-2003                | 后排 1.10 m              |
  | 灯杆 | **≥15 m**（11 人制 ≥15 m）      | 湖南省社会足球场地技术标准 | 15.0 m × 4，每杆 6 头     |
  | 围网 | 训练/业余 ≥4 m                  | DB37/T 3912-2020           | 4.0 m                    |

── 关键实现：stadium band 自定义网格 ─────────────────────────────────────
  田径场是「两直道 + 两半圆」，**Blender 无对应图元**（`make_cylinder` 压扁只得椭圆，
  正是批次 18-AA 的错误根源）。这里按 4 段参数化边界并让**内外两条边界共用同一 t**：
      t ∈ [0, .25)  右弧  a = −90°→+90°   ( L + r·cos a,  r·sin a )
      t ∈ [.25,.5)  上直  ( L − u·2L,       r )          u = (t−.25)/.25
      t ∈ [.5,.75)  左弧  a =  90°→270°    (−L + r·cos a,  r·sin a )
      t ∈ [.75,1)   下直  (−L + u·2L,      −r)
  内外一一对应 ⇒ 直接扫 quads，**无需带洞多边形三角化**；L=0 时直段自动退化成点
  ⇒ 同一函数可画中圈圆环（分道线 / 罚球弧同理）。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 Z-up、米制；导出自动 Yup ⇒ three.js 直立。
  · 跑道长轴沿 Blender X；**主看台在 Blender +Y 侧**。
  · 节点 identity；minY = 0（跑道面贴地）。
  · 内容盒轴对称 ⇒ `center_content_xz()` 幂等（仍调用以符合批次规约）。

── 材质槽（7，按材质分桶 ⇒ 7 个 primitive/DC）──────────────────────────
  Stadium_Track / _TrackLine / _Turf / _Concrete / _Steel / _Fence / _Floodlight

用法：
  blender --background --python build_sports_field.py -- \
    ClientWeb/src/assets/models/civic/sports_field.glb
"""
import math
import os
import sys

import bpy

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (  # noqa: E402
    reset_scene, set_unit_meters,
    make_box, make_cylinder, make_taper,
    make_material, assign_material, join_objects,
    export_glb, center_content_xz, CITY_PALETTE,
)

# ⚠⚠ **Blender 的 make_box(size) 是 (X 宽, Y 水平进深, Z 高度)**，
#    与 three.js `BoxGeometry(w, h, d)` = (宽, **高**, 进深) 的**中间槽位相反** ——
#    Blender 是 Z-up，垂直轴是 Z 不是 Y。写 (w, h, d) 会把 66 m 的场地基面立成
#    66 m 高的墙、把 0.12 m 的标线做成 32 m 高的板（首版即栽在此，靠 verify 的
#    bbox / 直立判据抓出来）。**本文件所有 make_box 一律按 (X 宽, Y 进深, Z 高) 书写。**

# ── 跑道几何（米）────────────────────────────────────────────────────────
R_IN = 20.00                    # 最内道内沿半径
LANES = 6
LANE_W = 1.22
R_OUT = R_IN + LANES * LANE_W   # 27.32
STRAIGHT_L = 37.17              # 单侧直道**全长** m
HALF_STRAIGHT = STRAIGHT_L / 2  # 半长（stadium 形的参数用的是半长：4×18.585 + 2π×20 = 200.00 m）
SAFETY_OUT = 3.00               # 跑道外安全区（含起跑缓冲）

# ── 七人制足球场 ─────────────────────────────────────────────────────────
PITCH_L, PITCH_W = 60.0, 32.0
PITCH_CIRCLE_R = 9.15
GOAL_W, GOAL_H = 7.32, 2.44
BOX_L, BOX_W = 40.32, 16.50     # 罚球区
GOAL_AREA_L, GOAL_AREA_W = 18.32, 5.50
LINE_W = 0.12                   # 标线宽（FIFA）

# ── 看台 ─────────────────────────────────────────────────────────────────
STAND_L = 56.0                  # 主看台长（沿 X）
STAND_ROWS = 5
STAND_ROW_D = 0.85              # 排距（≥0.75）
STAND_RISE = 0.45               # 级高（≥0.06 C 值，取结构模数）
STAND_RAIL_H = 1.10             # 后排栏杆（室外看台后部 ≥1.10）
END_STAND_L = 24.0              # 端部看台长（沿 Z）
END_ROWS = 2

# ── 灯杆 / 围网 / 场地 ───────────────────────────────────────────────────
LAMP_H, LAMP_N_HEADS = 15.0, 6
LAMP_X, LAMP_Y = 42.0, 33.0
FENCE_H, FENCE_SPACING = 4.0, 6.0
ROAD_W = 4.0                    # 消防车道
# 场地（混凝土硬化地）半尺寸。X 向须容下「跑道外沿 48.905 + 端部看台 2 排 1.70 +
# 栏杆 0.1 + 车道边距」；Y 向须容下「跑道外沿 30.32 + 主看台 5 排 4.25 + 车道边距」。
SITE_HX = 55.0                  # 场地半长（X）= 110 m
SITE_HZ = 37.0                  # 场地半宽（Y）= 74 m
LANE_CX = 52.0                  # 消防车道中心线（X）
LANE_CY = 34.5                  # 消防车道中心线（Y）
FLAGPOLE_H, FLAG_N = 8.0, 3
EQUIP_L, EQUIP_D, EQUIP_H = 6.0, 4.0, 3.0

# 场地表面标高（分层，避免 z-fighting）。
# ⚠ **层序必须与几何厚度一致**：`SiteSlab` 是 0.05 m 厚的混凝土基面（顶面 0.05），
#   其上依次叠 跑道 / 安全区 / 草皮 / 路缘 / 标线。首版基面厚 0.12 m 而跑道面只有
#   0.06 —— **基面把整条跑道和草皮都盖住了**，出图只剩两条红色边线。
Z_BASE = 0.0
Z_SLAB = 0.05                   # 混凝土基面顶
Z_SURF = 0.06                   # 跑道外安全区
Z_RUNWAY = 0.07                 # 跑道面
Z_TURF = 0.08                   # 内场草皮
Z_KERB = 0.10                   # 内场路缘
Z_LINE = 0.115                  # 标线（跑道分道线 / 足球场标线）
PIN_TOLERANCE_M = 0.05         # 贴地钉扎容差：超过即判定为「轴位写错」而非贴地误差


# ── stadium band 网格工具（见文件头「关键实现」）──────────────────────────
def _stadium_point(t: float, half_straight: float, r: float) -> tuple:
    """参数化体育场形边界；t∈[0,1) 走 右弧→上直→左弧→下直。
    内外半径共用同一 t ⇒ 可直接扫 quads；half_straight=0 时退化为圆。"""
    t = t % 1.0
    # ⚠ 每段占 t 的 1/4，**且要扫满 180°**（半圆）。首版写成 `* 4.0 * 90.0` 只扫了 90°，
    #   弧末点落在 (L+r, 0) 而直段首点在 (L, r) —— 边界不连续，扇形三角化在内场中央
    #   留下一条空洞（出图 = 草皮中间一条白带）。
    if t < 0.25:
        a = math.radians(-90.0 + t * 4.0 * 180.0)
        return (half_straight + r * math.cos(a), r * math.sin(a))
    if t < 0.5:
        u = (t - 0.25) * 4.0
        return (half_straight - u * 2.0 * half_straight, r)
    if t < 0.75:
        a = math.radians(90.0 + (t - 0.5) * 4.0 * 180.0)
        return (-half_straight + r * math.cos(a), r * math.sin(a))
    u = (t - 0.75) * 4.0
    return (-half_straight + u * 2.0 * half_straight, -r)


def make_stadium_band(name: str, half_straight: float, r_in: float, r_out: float,
                      z: float, thickness: float, segs: int = 64,
                      bottom: bool = True, sides: bool = True):
    """扫一条体育场形**环带**（跑道面 / 安全区 / 分道线 / 中圈都由它派生）。

    `bottom` / `sides` 用于压面数：被 `SiteSlab` 垫在下面的层可以不封底，
    线宽 0.12 m 的分道线侧壁在 1.2 m 道宽下不可见，可以不封侧。
    """
    verts, faces = [], []
    n = segs

    def add_quad(p0, p1, p2, p3):
        b = len(verts)
        for (x, y) in (p0, p1, p2, p3):
            verts.append((x, y, z))
        faces.append((b, b + 1, b + 2, b + 3))
        b2 = len(verts)
        for (x, y) in (p0, p1, p2, p3):
            verts.append((x, y, z - thickness))
        faces.append((b2 + 3, b2 + 2, b2 + 1, b2))

    for i in range(n):
        t0, t1 = i / n, (i + 1) / n
        o0 = _stadium_point(t0, half_straight, r_out)
        o1 = _stadium_point(t1, half_straight, r_out)
        i0 = _stadium_point(t0, half_straight, r_in)
        i1 = _stadium_point(t1, half_straight, r_in)
        if bottom:
            add_quad(o0, o1, i1, i0)
        else:
            b = len(verts)
            for (x, y) in (o0, o1, i1, i0):
                verts.append((x, y, z))
            faces.append((b, b + 1, b + 2, b + 3))
        if not sides:
            continue
        # 内外侧壁（薄板从侧面看不是纸片）
        for (a, b) in ((o0, o1), (i1, i0)):
            b3 = len(verts)
            for (x, y) in (a, b):
                verts.append((x, y, z))
            for (x, y) in (a, b):
                verts.append((x, y, z - thickness))
            faces.append((b3, b3 + 2, b3 + 3, b3 + 1))

    mesh = bpy.data.meshes.new(name)
    mesh.from_pydata(verts, [], faces)
    mesh.validate()
    mesh.update()
    obj = bpy.data.objects.new(name, mesh)
    bpy.context.collection.objects.link(obj)
    return obj


def make_stadium_disc(name: str, half_straight: float, r: float, z: float, thickness: float,
                      segs: int = 64, bottom: bool = True):
    """体育场形**实心面**（内场草皮）；由中心向边界扇形三角化。"""
    verts, faces = [], []
    n = segs
    verts.append((0.0, 0.0, z))
    for i in range(n):
        x, y = _stadium_point(i / n, half_straight, r)
        verts.append((x, y, z))
    for i in range(n):
        faces.append((0, 1 + i, 1 + (i + 1) % n))
    if not bottom:
        mesh = bpy.data.meshes.new(name)
        mesh.from_pydata(verts, [], faces)
        mesh.validate()
        mesh.update()
        obj = bpy.data.objects.new(name, mesh)
        bpy.context.collection.objects.link(obj)
        return obj
    base = len(verts)
    verts.append((0.0, 0.0, z - thickness))
    for i in range(n):
        x, y = _stadium_point(i / n, half_straight, r)
        verts.append((x, y, z - thickness))
    for i in range(n):
        faces.append((base, base + 1 + (i + 1) % n, base + 1 + i))
    for i in range(n):
        a = _stadium_point(i / n, half_straight, r)
        b = _stadium_point((i + 1) / n, half_straight, r)
        b3 = len(verts)
        for (x, y) in (a, b):
            verts.append((x, y, z))
        for (x, y) in (a, b):
            verts.append((x, y, z - thickness))
        faces.append((b3, b3 + 1, b3 + 3, b3 + 2))
    mesh = bpy.data.meshes.new(name)
    mesh.from_pydata(verts, [], faces)
    mesh.validate()
    mesh.update()
    obj = bpy.data.objects.new(name, mesh)
    bpy.context.collection.objects.link(obj)
    return obj


def main() -> None:
    reset_scene()
    set_unit_meters(1.0)

    track, line, turf = [], [], []
    concrete, steel, fence, flood = [], [], [], []

    # ── ① 场地基面（混凝土硬化地，兼作跑道与内场之间的底层）──────────────
    concrete.append(make_box('SiteSlab', (SITE_HX * 2, SITE_HZ * 2, Z_SLAB), (0, 0, Z_BASE + Z_SLAB / 2)))
    # 消防车道：沿场地外缘的 4.0 m 环形通道。**不另铺面**（基面本身就是混凝土硬化地），
    # 只画两侧白色边线 —— 真实田径场的消防车道就是硬化地面上的划线通道。
    for sx in (-1, 1):
        for k, dx in enumerate((ROAD_W / 2,)):
            line.append(make_box(f'RoadEdgeNS_{sx}{k}', (0.15, SITE_HZ * 2 - 1.0, 0.02),
                                 (sx * (LANE_CX + dx), 0, Z_SLAB + 0.02)))
    for sy in (-1, 1):
        line.append(make_box(f'RoadEdgeEW_{sy}', (SITE_HX * 2 - 1.0, 0.15, 0.02),
                             (0, sy * (LANE_CY + ROAD_W / 2), Z_SLAB + 0.02)))

    # ── ② 跑道外安全区（3.0 m）+ 跑道面（r 20→27.32）──────────────────────
    # 分层贴地且都压在 SiteSlab 之上 ⇒ 一律 bottom=False；外沿有落差处才封侧。
    track.append(make_stadium_band('SafetyZone', HALF_STRAIGHT, R_OUT, R_OUT + SAFETY_OUT,
                                   Z_SURF, 0.10, segs=64, bottom=False))
    track.append(make_stadium_band('Runway', HALF_STRAIGHT, R_IN, R_OUT, Z_RUNWAY, 0.10,
                                   segs=64, bottom=False))
    # 内场草皮（stadium 形，r = R_IN）
    turf.append(make_stadium_disc('Infield', HALF_STRAIGHT, R_IN, Z_TURF, 0.10,
                                  segs=64, bottom=False))
    # 内场与跑道之间的路缘石（防止草皮侵入跑道）—— 有 0.10 m 落差，须封侧
    track.append(make_stadium_band('InfieldKerb', HALF_STRAIGHT, R_IN - 0.15, R_IN,
                                   Z_KERB, 0.04, segs=64, bottom=False))

    # ── ③ 分道线 5 条（6 道分 5 线，线宽 0.12 m）──────────────────────────
    for i in range(1, LANES):
        r = R_IN + i * LANE_W
        # 线宽 0.12 m 的标线：既不封底也不封侧（1.22 m 道宽下侧壁不可见）
        line.append(make_stadium_band(f'LaneLine_{i}', HALF_STRAIGHT, r - LINE_W / 2, r + LINE_W / 2,
                                      Z_RUNWAY + 0.01, 0.02, segs=48,
                                      bottom=False, sides=False))
    # 起跑线：两端弯道各一条（真实田径场起跑线在弯道 + 直道混合段，本件取弯道起点）
    for sx in (-1, 1):
        line.append(make_box(f'StartLine_{sx}', (0.12, R_OUT - R_IN, 0.02),
                             (sx * HALF_STRAIGHT, -(R_IN + R_OUT) / 2, Z_LINE)))

    # ── ④ 七人制足球场标线（60×32 m，人造草）─────────────────────────────
    hx, hz = PITCH_L / 2, PITCH_W / 2
    for tag, (sx, sz, px, pz) in {
        'Sideline': (PITCH_L, LINE_W, 0, hz - LINE_W / 2),
        'Sideline2': (PITCH_L, LINE_W, 0, -hz + LINE_W / 2),
        'GoalLine': (LINE_W, PITCH_W, hx - LINE_W / 2, 0),
        'GoalLine2': (LINE_W, PITCH_W, -hx + LINE_W / 2, 0),
        'Halfway': (LINE_W, PITCH_W, 0, 0),
    }.items():
        line.append(make_box(f'Pitch_{tag}', (sx, sz, 0.02), (px, pz, Z_LINE)))
    line.append(make_stadium_band('PitchCircle', 0.0, PITCH_CIRCLE_R - LINE_W / 2,
                                  PITCH_CIRCLE_R + LINE_W / 2, Z_LINE, 0.02, segs=48,
                                  bottom=False, sides=False))
    line.append(make_cylinder('PitchSpot', 0.10, 0.10, 0.02, 8, (0, 0, Z_LINE)))
    for sx in (-1, 1):
        line.append(make_box(f'PitchSpotB_{sx}', (0.10, 0.10, 0.02),
                             (sx * (hx - 11.0), 0, Z_LINE)))
        # 罚球区 / 球门区是**矩形框**不是实心块 —— `make_box` 出的是实心体，
        # 直接画 (40.32, 16.5) 会得到一整块白板（首版就如此，出图是内场中央一条白带）。
        # 这里统一走「四条边线」拼框。
        for tag, (bl, bw) in (('Box', (BOX_L, BOX_W)), ('GoalArea', (GOAL_AREA_L, GOAL_AREA_W))):
            bx0 = sx * (hx - bl / 2)
            line.append(make_box(f'Pitch{tag}_{sx}_a', (bl, LINE_W, 0.02),
                                 (bx0, bw / 2 - LINE_W / 2, Z_LINE)))
            line.append(make_box(f'Pitch{tag}_{sx}_b', (bl, LINE_W, 0.02),
                                 (bx0, -bw / 2 + LINE_W / 2, Z_LINE)))
            line.append(make_box(f'Pitch{tag}_{sx}_c', (LINE_W, bw, 0.02),
                                 (bx0 - sx * (bl / 2 - LINE_W / 2), 0, Z_LINE)))
        # 球门（横梁 + 双立柱）。
        # ⚠ 本场地铺在 Blender 的 **XY 平面**（长轴 X = 场长、宽轴 Y = 场宽），
        #   **垂直轴是 Z**：横梁「跨门宽」= Y 向 7.32 m ⇒ (0.12, GOAL_W, 0.12)；
        #   立柱「柱高」= Z 向 2.44 m ⇒ (0.12, 0.12, GOAL_H)。
        #   首版两者的 Y/Z 都写反了：立柱 pos 写成 (∓30, 1.22, ∓3.60) —— 把「沿门宽
        #   分置」写进了 Z（垂直轴），立柱埋到地下 3.60 m，随后的贴地钉扎又把整个 steel
        #   组抬了 3.66 m，15 m 灯杆凭空长高到 19.24 m。
        steel.append(make_box(f'GoalBar_{sx}', (0.12, GOAL_W, 0.12),
                              (sx * hx, 0, GOAL_H)))
        for sz in (-1, 1):
            steel.append(make_box(f'GoalPost_{sx}{sz}', (0.12, 0.12, GOAL_H),
                                  (sx * hx, sz * (GOAL_W / 2 - 0.06), GOAL_H / 2)))
        # 角旗
        for sz in (-1, 1):
            steel.append(make_cylinder(f'CornerFlag_{sx}{sz}', 0.05, 0.05, 1.50, 6,
                                       (sx * hx, sz * hz, Z_TURF + 0.75)))
            line.append(make_box(f'CornerFlagCloth_{sx}{sz}', (0.04, 0.25, 0.35),
                                 (sx * hx, sz * (hz - 0.2), Z_TURF + 1.30)))

    # ── ⑤ 主看台（Blender +Y 侧，56 m × 3 排）+ 后排栏杆 + 纵过道扶手 ──────
    y0 = R_OUT + SAFETY_OUT
    for r_i in range(STAND_ROWS):
        y = y0 + r_i * STAND_ROW_D + STAND_ROW_D / 2
        concrete.append(make_box(f'StandRow_{r_i}', (STAND_L, STAND_ROW_D, STAND_RISE * (r_i + 1) / 2),
                                 (0, y, STAND_RISE * (r_i + 1) / 2)))
        # 座椅排（示意：连续坐凳条）
        steel.append(make_box(f'SeatRow_{r_i}', (STAND_L - 1.0, 0.40, 0.06),
                              (0, y, STAND_RISE * (r_i + 1) + 0.45)))
    back_y = y0 + STAND_ROWS * STAND_ROW_D
    # 罩棚：钢柱 6 根 + 悬挑屋面（顶高 8.0 m，低于灯杆 15.58 m ⇒ 不改 bbox）。
    #   露天看台在远景里是「一条白线」，加罩棚后体育场的轮廓才立得起来。
    CANOPY_Z = 8.0
    CANOPY_D = 5.0                # 罩棚进深（悬挑覆盖后排，不越出场地）
    for i in range(6):
        steel.append(make_box(f'CanopyCol_{i}', (0.35, 0.35, CANOPY_Z - 2.8),
                              (-STAND_L / 2 + 5.0 + i * (STAND_L - 10.0) / 5.0,
                               back_y - 0.5, (CANOPY_Z + 2.8) / 2)))
    steel.append(make_box('CanopyDeck', (STAND_L + 6.0, CANOPY_D, 0.30),
                          (0, y0 + 1.5, CANOPY_Z)))
    for i in range(7):
        steel.append(make_box(f'CanopyBeam_{i}', (0.30, CANOPY_D, 0.45),
                              (-STAND_L / 2 - 2.5 + i * (STAND_L + 5.0) / 6.0,
                               y0 + 1.5, CANOPY_Z - 0.36)))
    # 后排栏杆 1.10 m（JGJ 31 室外看台后部）+ 前排矮栏杆
    for h, yy in ((STAND_RAIL_H, back_y + 0.10), (0.90, y0 + 0.10)):
        steel.append(make_box(f'StandRail_{h}', (STAND_L, 0.06, 0.06), (0, yy, h)))
        n_post = int(STAND_L / 2.5)
        for i in range(n_post + 1):
            steel.append(make_box(f'StandPost_{h}_{i}', (0.08, 0.08, h),
                                  (-STAND_L / 2 + i * (STAND_L / n_post), yy, h / 2)))
    # 4 根纵过道扶手（前后排高差 >0.50 m 时 JGJ 31 要求）
    for i, sx in enumerate((-24.0, -8.0, 8.0, 24.0)):
        steel.append(make_box(f'AisleRail_{i}', (0.06, STAND_ROWS * STAND_ROW_D, 0.06),
                              (sx, y0 + STAND_ROWS * STAND_ROW_D / 2, 0.90)))
    # 看台端部封边（避免从侧面看成一片空板）
    for sx in (-1, 1):
        concrete.append(make_box(f'StandEnd_{sx}', (0.4, STAND_ROWS * STAND_ROW_D,
                                                    STAND_RISE * STAND_ROWS),
                                 (sx * (STAND_L / 2 + 0.2), y0 + STAND_ROWS * STAND_ROW_D / 2,
                                  STAND_RISE * STAND_ROWS / 2)))

    # ── ⑥ 端部看台 ×2（弯道外侧，24 m × 2 排）────────────────────────────
    ex = HALF_STRAIGHT + R_OUT + SAFETY_OUT
    for sx in (-1, 1):
        for r_i in range(END_ROWS):
            x = sx * (ex + r_i * STAND_ROW_D + STAND_ROW_D / 2)
            concrete.append(make_box(f'EndStand_{sx}_{r_i}',
                                     (STAND_ROW_D, END_STAND_L, STAND_RISE * (r_i + 1) / 2),
                                     (x, 0, STAND_RISE * (r_i + 1) / 2)))
            steel.append(make_box(f'EndSeat_{sx}_{r_i}', (0.40, END_STAND_L - 1.0, 0.06),
                                  (x, 0, STAND_RISE * (r_i + 1) + 0.45)))
        steel.append(make_box(f'EndRail_{sx}', (0.06, END_STAND_L, 1.10),
                              (sx * (ex + END_ROWS * STAND_ROW_D + 0.10), 0, 1.10)))

    # ── ⑦ 灯杆 ×4（15.0 m，每杆 6 头灯盘）+ 爬梯 ─────────────────────────
    for sx in (-1, 1):
        for sy in (-1, 1):
            px, py = sx * LAMP_X, sy * LAMP_Y
            concrete.append(make_box(f'LampBase_{sx}{sy}', (1.2, 1.2, 0.40), (px, py, 0.20)))
            steel.append(make_taper(f'LampPole_{sx}{sy}', 0.32, 0.16, LAMP_H, 10,
                                    (px, py, 0.40 + LAMP_H / 2)))
            # 灯盘：6 头沿 Y 排开
            steel.append(make_box(f'LampBar_{sx}{sy}', (0.16, 5.0, 0.16),
                                  (px, py, 0.40 + LAMP_H + 0.10)))
            for k in range(LAMP_N_HEADS):
                ly = py - 2.5 + k * (5.0 / (LAMP_N_HEADS - 1))
                flood.append(make_box(f'LampHead_{sx}{sy}_{k}', (0.55, 0.70, 0.30),
                                      (px + 0.30, ly, 0.40 + LAMP_H - 0.10)))
            # 爬梯
            for k in range(8):
                steel.append(make_box(f'LampRung_{sx}{sy}_{k}', (0.05, 0.40, 0.05),
                                      (px + 0.38, py + 0.0, 1.2 + k * 1.6)))

    # ── ⑧ 围网 4.0 m（沿场地外缘，立柱 @6 m + 三道横杆 + 网片）────────────
    fx, fz = SITE_HX - 0.3, SITE_HZ - 0.3
    corners = [(-fx, -fz), (fx, -fz), (fx, fz), (-fx, fz)]
    for e in range(4):
        ax, ay = corners[e]
        bx2, by2 = corners[(e + 1) % 4]
        seg_len = math.hypot(bx2 - ax, by2 - ay)
        n = max(2, int(seg_len / FENCE_SPACING))
        along_x = abs(bx2 - ax) > abs(by2 - ay)
        for i in range(n + 1):
            t = i / n
            px = ax + (bx2 - ax) * t
            py = ay + (by2 - ay) * t
            steel.append(make_box(f'FencePost_{e}_{i}', (0.10, 0.10, FENCE_H), (px, py, FENCE_H / 2)))
        for h in (0.6, 2.0, FENCE_H - 0.1):
            steel.append(make_box(f'FenceRail_{e}_{h}', (seg_len if along_x else 0.06, 0.06
                                                          if along_x else seg_len, 0.06),
                                  ((ax + bx2) / 2, (ay + by2) / 2, h)))
        fence.append(make_box(f'FenceNet_{e}', (seg_len if along_x else 0.03, 0.03
                                                 if along_x else seg_len, FENCE_H - 0.3),
                              ((ax + bx2) / 2, (ay + by2) / 2, FENCE_H / 2)))

    # ── ⑨ 旗杆 ×3 + 器材室 + 检录门房 ───────────────────────────────────
    for i in range(FLAG_N):
        steel.append(make_taper(f'Flag_{i}', 0.08, 0.06, FLAGPOLE_H, 8,
                                (-10.0 + i * 2.0, -SITE_HZ + 2.5, Z_SLAB + FLAGPOLE_H / 2)))
    concrete.append(make_box('EquipRoom', (EQUIP_L, EQUIP_D, EQUIP_H),
                             (-30.0, -SITE_HZ + 4.0, Z_SLAB + EQUIP_H / 2)))
    steel.append(make_box('EquipRoof', (EQUIP_L + 0.4, EQUIP_D + 0.4, 0.16),
                          (-30.0, -SITE_HZ + 4.0, Z_SLAB + EQUIP_H + 0.08)))
    steel.append(make_box('EquipDoor', (1.0, 0.08, 2.1),
                          (-30.0, -SITE_HZ + 4.0 - EQUIP_D / 2 - 0.02, Z_SLAB + 1.05)))

    # ── ⑩ 按材质合并 ⇒ 7 primitive ───────────────────────────────────
    mat_track = make_material('Stadium_Track_Mat', CITY_PALETTE['track_surface'], rough=0.92, metal=0.0)
    mat_line = make_material('Stadium_TrackLine_Mat', CITY_PALETTE['paint_road'], rough=0.90, metal=0.0)
    mat_turf = make_material('Stadium_Turf_Mat', CITY_PALETTE['turf_sports'], rough=0.95, metal=0.0)
    mat_c = make_material('Stadium_Concrete_Mat', CITY_PALETTE['concrete'], rough=0.90, metal=0.0)
    mat_s = make_material('Stadium_Steel_Mat', CITY_PALETTE['steel'], rough=0.45, metal=0.70)
    mat_f = make_material('Stadium_Fence_Mat', CITY_PALETTE['fence_net'],
                          rough=0.60, metal=0.40, alpha=0.35)
    mat_fl = make_material('Stadium_Floodlight_Mat', CITY_PALETTE['floodlight'],
                           rough=0.25, metal=0.0, emissive=CITY_PALETTE['floodlight'],
                           emissive_intensity=0.4)
    groups = [
        (track, mat_track, 'Stadium_Track'),
        (line, mat_line, 'Stadium_TrackLine'),
        (turf, mat_turf, 'Stadium_Turf'),
        (concrete, mat_c, 'Stadium_Concrete'),
        (steel, mat_s, 'Stadium_Steel'),
        (fence, mat_f, 'Stadium_Fence'),
        (flood, mat_fl, 'Stadium_Floodlight'),
    ]
    # ⚠⚠ **join 前必须先 flatten 组内每个对象**（本文件踩过）：`join_objects` 把其余件
    #    折进**首件**的局部系（非等比 scale），之后再 `transform_apply` 折回来。
    #    本文件的 `steel` 组首件是球门横梁 `make_box((0.12, 7.32, 0.12), …)`
    #    —— 0.12 : 7.32 的非等比缩放让折算产生累积误差，灯杆组整体被拉高 ~23%
    #    （实测 zMax 19.24 m vs 实际 15.58 m）。等比缩放（路灯杆桩那种）不触发。
    #    逐件 flatten 后 join 退化为纯几何并集，与各件的 scale/位置无关。
    for objs, _m, _name in groups:
        for o in objs:
            if (all(abs(v - 1.0) < 1e-9 for v in o.scale)
                    and all(abs(v) < 1e-9 for v in o.location)
                    and all(abs(v) < 1e-9 for v in o.rotation_euler)):
                continue
            bpy.ops.object.select_all(action='DESELECT')
            o.select_set(True)
            bpy.context.view_layer.objects.active = o
            bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)

    joined = [join_objects(objs, name) for objs, _m, name in groups]
    for (_objs, mat, _name), obj in zip(groups, joined):
        assign_material(obj, mat)

    # ⚠ 全件米 → 世界单位（×0.1）；顺序：transform_apply flatten → ×0.1 → 钉扎 → 居中
    for obj in joined:
        bpy.ops.object.select_all(action='DESELECT')
        obj.select_set(True)
        bpy.context.view_layer.objects.active = obj
        bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
        obj.scale = (0.1, 0.1, 0.1)

    # 贴地钉扎。**只处理小量下探**：它的用途是纠正「倾斜件端头 / 收分柱帽」那几毫米到
    # 几厘米的误差（批次 45 首版即此用途）。若某组的最低点远低于 0，那多半不是贴地误差，
    # 而是**把「水平偏移」误当成「埋进地下」**了 —— 批次 47 的球门立柱就沿 Blender Z
    # （水平宽轴）分置在 ±3.60 m，几何完全正确，却让整组被抬 3.66 m，连带 15 m 的灯杆
    # 凭空长高 23%，而 bbox 守卫要超 ±5% 才报。超过容差只告警、**不抬**。
    for obj in joined:
        min_z = min(bb[2] for bb in obj.bound_box)
        if min_z >= 0:
            continue
        if -min_z > PIN_TOLERANCE_M:
            print(f'[pin] ⚠ {obj.name} 最低点在 {min_z:.3f} m（超过容差 {PIN_TOLERANCE_M} m）'
                  f'，**不钉扎** —— 查该组是否有把水平偏移写进 Z 的件', flush=True)
            continue
        obj.location.z = -0.1 * min_z

    center_content_xz(joined)

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv
                else '/tmp/sports_field.glb')
    export_glb(out_path)
    print(f'✅ sports_field.glb exported: {out_path} '
          f'({os.path.getsize(out_path)} bytes)', flush=True)


if __name__ == '__main__':
    main()
