#!/usr/bin/env python3
"""
build_rooftop_plant — 批次 53「建筑屋顶真实感」屋顶设备机组（civic/rooftop_plant.glb）。

── 为什么需要这件 GLB ───────────────────────────────────────────────────
  旧口径（`building_shapes::rooftopAccent`）的屋顶设备是 3 个裸几何：
    水箱 = 圆柱 ⌀0.3 m × 高 0.3 m、通风管 = 圆柱 ⌀0.16 m × 0.35 m。
  ① **尺寸差 8 倍** —— 真实屋顶不锈钢水箱 ⌀2.0~2.4 m × 2.5~3.0 m；
  ② **被埋** —— slab / pavilion 的檐口是覆盖全屋面的实心板，两者都落在
     y ∈ [0, 0.5 m] 内，**全城 180 栋楼的水箱从未被看见**（批次 53 A1 已揭盖）；
  ③ **表达力不足** —— 冷却塔的风机罩 / 进风百叶 / 支腿减振垫、空调外机的
     侧百叶与托架、爬梯护笼，这些细节盒几何做不出来。

  本件把「一个标准屋顶设备组团」做成可实例化的 GLB：全城屋顶共用 1 件
  `GlbInstanced` ⇒ **7 draw call 与实例数无关**。

── §27.0-1 真实形态调研（逐条依据见方案 53 §2.2）──────────────────────
  | 部件       | 真实规格                                       | 依据                    | 本件取值            |
  |------------|------------------------------------------------|-------------------------|---------------------|
  | 水箱       | 圆柱 ⌀2.0~2.4 m × 高 2.5~3.0 m；⌀400~500 人孔  | 12S101 / 冷却塔配套水箱 | ⌀2.20 × 筒高 2.60  |
  | 水箱支腿   | 4~6 根 × 0.4~0.6 m                           | 同上                    | 4 根 ⌀0.09 × 0.50  |
  | 冷却塔     | 150T 方形横流式 3800×2600×3720 mm（长宽高）    | AH-150L/S 产品样本      | 3.80 × 2.60 × 3.72 |
  | 塔顶风机   | 轴流风机 ⌀2000 + 玻璃钢风筒                    | 同上                    | 罩 ⌀2.00 + 风筒 ⌀1.70×0.55 |
  | 塔侧进风   | 两侧进风百叶窗（镀锌/FRP）                     | 同上                    | 百叶 2 面 × 6 片    |
  | 空调外机   | 三匹 900×300×700 mm + 侧百叶 + 顶部风机罩     | 空调外机支架尺寸通例    | 0.90 × 0.30 × 0.70 ×2 |
  | 检修爬梯   | 梯宽 400~500；踏步间距 300；>7 m 加护笼        | 钢结构厂房检修爬梯规范  | 梯宽 0.45 / 踏步 0.30 |
  | 护笼       | ⌀700，每 750 一圈                             | 同上                    | ⌀0.70 × 3 圈       |
  | 减振垫     | 屋面安装须加钢支腿 + 橡胶减振垫 + 防风锚固     | 冷却塔安装规范          | 支腿 0.40 + 垫 ⌀0.40 |

  ⚠ **护笼取舍**：屋面梯落地高度 ≤ 1.5 m，远低于规范 7 m 阈值，**按规范从严
  不加护笼**。但冷却塔的检修平台高 0.40 m + 塔高 3.72 m，检修人员登塔属高处
  作业，故**平台围栏按 1100 mm 防护栏**（IBC 1015.3 坠落防护 1067 mm 的中国等价值）
  设置，护笼本体不设。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 **Z-up**、米制；导出自动 Yup ⇒ three.js 直立。
  · **X = 东西（设备区长向 8.60 m）**，**Y = 南北（3.80+0.70 通道 = 5.20 m）**。
  · 节点 identity（尺寸烘进顶点）；**minY = 0**（屋面 = 支腿底 / 减振垫底）。
  · `center_content_xz()`：本件沿 Y 不对称（北侧 0.70 m 检修通道），必须居中。

── 材质槽（7，按材质分桶 ⇒ 7 个 primitive/DC）──────────────────────────
  Tank_Steel / Tank_Leg / Tower_Shell / Tower_Fan / AC_Unit /
  Steel_Frame / Ladder

用法：
  blender --background --python build_rooftop_plant.py -- \
    ClientWeb/src/assets/models/civic/rooftop_plant.glb
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
    make_box, make_box_wdh, make_cylinder, make_cone, make_taper,
    make_material, assign_material, join_objects, weld_split_vertices, strip_uvs,
    export_glb, center_content_xz, CITY_PALETTE,
)

# ── 平面布局（米；X 东 / Y 北，+Y = 检修通道侧）─────────────────────────
# 设备区 8.37 (X) × 5.40 (Y)。沿 X 三段，西→东：水箱 / 冷却塔（含检修平台）/ 空调外机。
# 检修爬梯**贴冷却塔 +Y 面**（真实冷却塔的塔侧检修梯），不另立孤立梯子。
TANK_CX = -3.00          # 水箱中心 X
TANK_Y = -0.30           # 水箱中心 Y
TOWER_CX = 0.30          # 冷却塔中心 X（长轴沿 Y）
TOWER_Y = 0.30           # 冷却塔中心 Y
AC_CX = 3.30             # 空调外机组中心 X
AC_Y = -0.30             # 空调外机组中心 Y

# ── 水箱（⌀2.20 × 筒高 2.60，4 支腿 0.50）───────────────────────────────
TANK_R = 1.10
TANK_BODY_H = 2.60
TANK_LEG_H = 0.50
TANK_LID_H = 0.18
TANK_LEG_R = 0.045
TANK_MANHOLE_R = 0.25     # ⌀0.50 人孔

# ── 冷却塔（150T 方形横流式 3.80 × 2.60 × 3.72）────────────────────────
# ⚠ 3.72 m 是**样本整塔高（含风筒与风机罩）**，支腿另计 0.40 m。
#   故塔顶绝对标高 = TW_LEG_H + TW_H = 4.12 m，全件最高点。
TW_L = 2.60              # X 向
TW_W = 3.80              # Y 向（长轴）
TW_H = 3.72              # 支腿顶 → 风机罩顶（含风筒）
TW_LEG_H = 0.40
TW_BODY_H = TW_H - 0.92  # 塔体净高（顶部 0.92 m 留给风筒 + 风机罩）
TW_PAD_R = 0.20          # ⌀0.40 减振垫
TW_COWL_R = 1.00         # ⌀2.00 风机罩
TW_STACK_R = 0.85        # ⌀1.70 风筒
TW_RIBS = 6              # 每侧加强筋 / 百叶片数

# ── 空调外机（2 台 0.90 × 0.30 × 0.70 + 托架 0.35）──────────────────────
AC_W, AC_D, AC_H = 0.90, 0.30, 0.70
AC_BRACKET_H = 0.35
AC_GAP = 0.14             # 两台间距
AC_FAN_R = 0.24           # 顶部风机罩半径

# ── 检修平台 / 围栏 / 爬梯 ──────────────────────────────────────────────
DECK_H = 0.40
DECK_T = 0.06
RAIL_H = 1.10             # 防护栏 1100 mm（IBC 1015.3 坠落防护 1067 mm 中国等价）
RAIL_R = 0.035
LADDER_W = 0.45
LADDER_STEP = 0.30
LADDER_STEPS = 9
LADDER_R = 0.03

P = CITY_PALETTE


def _tank():
    """304 不锈钢水箱：筒体 + 浅拱顶盖 + ⌀0.50 人孔 + 4 支腿 + 横撑。"""
    body, legs = [], []
    z0 = TANK_LEG_H
    body.append(make_cylinder("Tank_Body", TANK_R, TANK_R, TANK_BODY_H, 24,
                              (TANK_CX, TANK_Y, z0 + TANK_BODY_H / 2)))
    # 顶盖：浅拱（taper 上收）
    body.append(make_cylinder("Tank_Lid", TANK_R * 0.34, TANK_R, TANK_LID_H, 24,
                              (TANK_CX, TANK_Y, z0 + TANK_BODY_H + TANK_LID_H / 2)))
    # 人孔：⌀0.50 短筒 + 翻边（凸出筒顶）
    mh_z = z0 + TANK_BODY_H + TANK_LID_H
    body.append(make_cylinder("Tank_Manhole", TANK_MANHOLE_R, TANK_MANHOLE_R, 0.14, 12,
                              (TANK_CX, TANK_Y, mh_z + 0.07)))
    body.append(make_cylinder("Tank_ManholeFlange", TANK_MANHOLE_R + 0.05,
                              TANK_MANHOLE_R + 0.05, 0.03, 12,
                              (TANK_CX, TANK_Y, mh_z + 0.14)))
    # 支腿：4 根 ⌀0.09 × 0.50，交替两圈横撑
    lx, ly = TANK_R * 0.70, TANK_R * 0.70
    for sx in (-1, 1):
        for sy in (-1, 1):
            legs.append(make_cylinder("Tank_Leg", TANK_LEG_R, TANK_LEG_R, TANK_LEG_H, 8,
                                      (TANK_CX + sx * lx, TANK_Y + sy * ly, TANK_LEG_H / 2)))
    for zz in (TANK_LEG_H * 0.34, TANK_LEG_H * 0.80):
        for sy in (-1, 1):
            legs.append(make_cylinder("Tank_Brace", TANK_LEG_R * 0.6, TANK_LEG_R * 0.6,
                                      lx * 2, 6, (TANK_CX, TANK_Y + sy * ly, zz), (0, math.pi / 2, 0)))
        for sx in (-1, 1):
            legs.append(make_cylinder("Tank_Brace", TANK_LEG_R * 0.6, TANK_LEG_R * 0.6,
                                      ly * 2, 6, (TANK_CX + sx * lx, TANK_Y, zz), (math.pi / 2, 0, 0)))
    return body, legs


def _tower():
    """方形横流式冷却塔：塔体 + 两侧进风百叶 + 竖向加强筋 + 集水盘。"""
    shell, frame = [], []
    hx, hy = TW_L / 2, TW_W / 2
    z0 = TW_LEG_H
    # 塔体主体
    shell.append(make_box_wdh("Tower_Shell", TW_L, TW_W, TW_BODY_H,
                              (TOWER_CX, TOWER_Y, z0 + TW_BODY_H / 2)))
    # 底部集水盘（略外扩）
    shell.append(make_box_wdh("Tower_Sump", TW_L + 0.10, TW_W + 0.10, 0.24,
                              (TOWER_CX, TOWER_Y, z0 + 0.12)))
    # 两侧进风百叶（±X 面）：6 片水平叶片 + 上下边框
    for sx in (-1, 1):
        face = sx * (hx + 0.02)
        for i in range(TW_RIBS):
            zz = z0 + 0.42 + i * ((TW_BODY_H - 0.84) / (TW_RIBS - 1))
            # 叶片略外倾 20°（进风百叶的导流角）
            shell.append(make_box("Tower_Louver", (0.05, TW_W * 0.86, 0.16),
                                  (TOWER_CX + face, TOWER_Y, zz), (0, 0, math.radians(-20))))
        frame.append(make_box("Tower_LouverFrame", (0.06, TW_W * 0.90, 0.06),
                              (TOWER_CX + face + sx * 0.03, TOWER_Y, z0 + 0.36)))
        frame.append(make_box("Tower_LouverFrame", (0.06, TW_W * 0.90, 0.06),
                              (TOWER_CX + face + sx * 0.03, TOWER_Y, z0 + TW_BODY_H - 0.36)))
    # 四角竖向加强筋
    for sx in (-1, 1):
        for sy in (-1, 1):
            frame.append(make_box("Tower_Rib", (0.10, 0.10, TW_BODY_H - 0.20),
                                  (TOWER_CX + sx * (hx - 0.03), TOWER_Y + sy * (hy - 0.03),
                                   z0 + (TW_BODY_H - 0.20) / 2)))
    # 支腿 4 根 + 减振垫
    for sx in (-1, 1):
        for sy in (-1, 1):
            px = TOWER_CX + sx * (hx - 0.18)
            py = TOWER_Y + sy * (hy - 0.18)
            frame.append(make_box("Tower_Leg", (0.12, 0.12, TW_LEG_H), (px, py, TW_LEG_H / 2)))
            frame.append(make_cylinder("Tower_Pad", TW_PAD_R, TW_PAD_R, 0.04, 10,
                                       (px, py, 0.02)))
    return shell, frame


def _tower_fan():
    """塔顶轴流风机：风筒 + ⌀2.00 风机罩（4 片叶片 + 中心毂）。"""
    out = []
    z_top = TW_LEG_H + TW_H          # 4.12 m = 全件最高点
    z_body_top = TW_LEG_H + TW_BODY_H
    stack_h = z_top - z_body_top     # 0.92 m
    out.append(make_cylinder("Tower_Stack", TW_STACK_R * 0.94, TW_STACK_R, stack_h * 0.62, 20,
                             (TOWER_CX, TOWER_Y, z_body_top + stack_h * 0.31)))
    # 风机罩：低矮锥台 + 4 片倾斜叶片 + 中心毂
    z_cowl = z_body_top + stack_h * 0.62
    out.append(make_cylinder("Tower_Cowl", TW_COWL_R, TW_STACK_R * 0.94, stack_h * 0.38, 24,
                             (TOWER_CX, TOWER_Y, z_cowl + stack_h * 0.19)))
    for i in range(4):
        a = math.pi * 2 * i / 4
        out.append(make_box("Tower_Blade", (0.62, 0.05, 0.03),
                            (TOWER_CX + math.cos(a) * 0.42, TOWER_Y + math.sin(a) * 0.42,
                             z_cowl + stack_h * 0.30),
                            (0, 0, a + math.radians(28))))
    out.append(make_cylinder("Tower_Hub", 0.18, 0.22, 0.18, 10,
                             (TOWER_CX, TOWER_Y, z_cowl + stack_h * 0.36)))
    return out


def _ac_units():
    """空调外机 2 台：机壳 + 侧进风百叶 + 顶部风机罩 + 底部托架。"""
    shells, frames = [], []
    for i in range(2):
        cx = AC_CX + (i - 0.5) * (AC_W + AC_GAP)
        cz = AC_Y
        # 托架 2 根角钢
        for sx in (-1, 1):
            frames.append(make_box("AC_Bracket", (0.05, 0.05, AC_BRACKET_H),
                                   (cx + sx * (AC_W / 2 - 0.08), cz, AC_BRACKET_H / 2)))
        z0 = AC_BRACKET_H
        shells.append(make_box_wdh("AC_Shell", AC_W, AC_D, AC_H, (cx, cz, z0 + AC_H / 2)))
        # 侧进风百叶（面向 −Y，即迎风面）
        for k in range(5):
            zz = z0 + 0.10 + k * ((AC_H - 0.20) / 4)
            shells.append(make_box("AC_Louver", (AC_W * 0.82, 0.03, 0.06),
                                   (cx, cz - AC_D / 2 - 0.015, zz)))
        # 顶部风机罩
        frames.append(make_cylinder("AC_FanGuard", AC_FAN_R, AC_FAN_R, 0.10, 16,
                                    (cx, cz, z0 + AC_H + 0.05)))
        for k in range(3):
            zz = z0 + AC_H + 0.11
            frames.append(make_box("AC_FanBar", (AC_FAN_R * 1.7, 0.025, 0.02),
                                   (cx, cz, zz), (0, 0, math.pi * k / 3)))
        # 背面提手槽
        for sx in (-1, 1):
            shells.append(make_box("AC_Handle", (0.16, 0.03, 0.05),
                                   (cx + sx * 0.24, cz + AC_D / 2 + 0.01, z0 + AC_H * 0.62)))
    return shells, frames


def _deck_rail():
    """冷却塔检修平台（角钢框 + 格栅条）+ 1100 mm 防护栏（临通道三面）。

    平台比塔体**每侧多出 0.50 m（X）/ 0.80 m（+Y 检修走道）**，
    否则整块平台被塔体盖住，只剩一圈悬空栏杆读不出「站台」。
    """
    deck, rails = [], []
    px, py = TOWER_CX, TOWER_Y
    pw, pd = TW_L + 1.00, TW_W + 1.60
    hx, hy = pw / 2, pd / 2
    z = DECK_H
    # 角钢边框
    for sy in (-1, 1):
        deck.append(make_box("Deck_Frame", (pw, 0.08, DECK_T), (px, py + sy * (hy - 0.04), z)))
    for sx in (-1, 1):
        deck.append(make_box("Deck_Frame", (0.08, pd - 0.16, DECK_T), (px + sx * (hx - 0.04), py, z)))
    # 格栅条（沿 Y 铺 9 根，读作格栅而非实心板）
    for i in range(9):
        xx = px - hx + 0.14 + (pw - 0.28) * i / 8
        deck.append(make_box("Deck_Grate", (0.05, pd - 0.22, 0.04), (xx, py, z + 0.01)))
    # 防护栏：+X / +Y / −Y 三面（−X 面紧邻水箱，检修动线自 +Y 走道进入）
    # ⚠ 每段显式给出「中心点 + 长度 + 朝向」，不写 (x0, x1, yy) 混坐标 ——
    #   混写会让东西段的端点误取另一个轴的坐标，栏杆整根飞出平台 2.25 m。
    spans = [
        (px, py + hy, pw, False),   # 北：沿 X
        (px, py - hy, pw, False),   # 南：沿 X
        (px + hx, py, pd, True),    # 东：沿 Y
    ]
    for cxr, cyr, len_, along_y in spans:
        for zz in (z + RAIL_H, z + RAIL_H * 0.55):
            rot = (math.pi / 2, 0, 0) if along_y else (0, math.pi / 2, 0)
            rails.append(make_cylinder("Rail_Bar", RAIL_R, RAIL_R, len_, 8, (cxr, cyr, zz), rot))
    for (sx, sy) in [(px - hx, py + hy), (px + hx, py + hy), (px - hx, py - hy), (px + hx, py - hy)]:
        rails.append(make_cylinder("Rail_Post", RAIL_R, RAIL_R, RAIL_H, 8, (sx, sy, z + RAIL_H / 2)))
    return deck, rails


def _ladder():
    """冷却塔塔侧检修直梯：2 根边梁 + 8 级踏步 + 顶部扶手。

    贴塔体 +Y 面（检修走道侧）竖立。**下端站在检修平台上**（z 起于 DECK_H）——
    这才是有平台时的正确构造：平台即登塔通道，梯子只负责塔体那一段。
    （早期版本让梯子从屋面 z=0 直起，结果**穿透平台格栅**，平台上凭空一根梯子。）

    梯高 3.20 − 0.40 = 2.80 m，远低于规范的 7 m 护笼阈值，**按规范不加护笼**。
    顶部不设扶手 —— 直梯通至塔顶检修口，无独立登顶平台。
    """
    out = []
    hx = LADDER_W / 2
    ly = TOWER_Y + TW_W / 2 + 0.22          # 离塔面 0.22 m（梯梁半厚 + 背离）
    z0 = DECK_H                             # 站在检修平台上
    top = TW_LEG_H + TW_BODY_H              # 3.20 m，与塔顶检修口齐平
    h = top - z0
    for sx in (-1, 1):
        out.append(make_cylinder("Ladder_Stringer", LADDER_R, LADDER_R, h, 8,
                                 (TOWER_CX + sx * hx, ly, z0 + h / 2)))
    for i in range(1, LADDER_STEPS + 1):
        out.append(make_cylinder("Ladder_Step", LADDER_R, LADDER_R, LADDER_W, 8,
                                 (TOWER_CX, ly, z0 + h * i / (LADDER_STEPS + 1)),
                                 (0, math.pi / 2, 0)))
    return out


def main():
    reset_scene()
    set_unit_meters(1.0)

    tank_body, tank_legs = _tank()
    tw_shell, tw_frame = _tower()
    tw_fan = _tower_fan()
    ac_sh, ac_fr = _ac_units()
    deck, rails = _deck_rail()
    ladder = _ladder()

    groups = [
        (tank_body, make_material("Tank_Steel", P['tank_steel'], 0.34, 0.85), "Tank_Steel"),
        (tank_legs, make_material("Tank_Leg", P['ladder_galv'], 0.48, 0.80), "Tank_Leg"),
        (tw_shell, make_material("Tower_Shell", P['tower_frp'], 0.62, 0.05), "Tower_Shell"),
        (tw_fan, make_material("Tower_Fan", P['tower_louver'], 0.52, 0.55), "Tower_Fan"),
        (ac_sh, make_material("AC_Unit", P['ac_shell'], 0.44, 0.60), "AC_Unit"),
        (tw_frame + ac_fr + deck + rails, make_material("Steel_Frame", P['grate_deck'], 0.55, 0.75), "Steel_Frame"),
        (ladder, make_material("Ladder", P['ladder_galv'], 0.50, 0.80), "Ladder"),
    ]

    joined = []
    for objs, mat, name in groups:
        if not objs:
            continue
        obj = join_objects(objs, name)
        assign_material(obj, mat)
        joined.append(obj)

    # 全件米 → 世界单位（×0.1）。join 保留首个对象的 location/scale 变换
    # （其余件烘进顶点），必须先 transform_apply flatten 再统一缩放。
    for obj in joined:
        bpy.ops.object.select_all(action='DESELECT')
        obj.select_set(True)
        bpy.context.view_layer.objects.active = obj
        bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
        obj.scale = (0.1, 0.1, 0.1)

    # 贴地钉扎：支腿底 / 减振垫底为最低点。
    for obj in joined:
        min_z = min(bb[2] for bb in obj.bound_box)
        if min_z < 0:
            obj.location.z = -0.1 * min_z

    # 批次 50 起：合并前焊缝 + 剥 UV，显著压体积（construction_site 靠此守住 500 KB 线）。
    for obj in joined:
        weld_split_vertices(obj, threshold=1.0e-4)
        strip_uvs(obj)

    center_content_xz(joined)

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv
                else '/tmp/rooftop_plant.glb')
    export_glb(out_path)
    print(f'✅ rooftop_plant.glb exported: {out_path} '
          f'({os.path.getsize(out_path)} bytes)', flush=True)


if __name__ == '__main__':
    main()
