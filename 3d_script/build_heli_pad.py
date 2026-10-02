#!/usr/bin/env python3
"""
build_heli_pad — 批次 46「城市公用设施真实感」直升机停机坪（civic/heli_pad.glb）。

── 重制理由：批次 18-AA 的程序化停机坪是玩具 ────────────────────────────
  现状 `civic/HeliPad.tsx` = 1 个 ⌀28 m 的 `CircleGeometry` 坪面 + 1 个
  `RingGeometry(12.5, 13)` 白环 + 3 块 H 笔画 + 1 根锥形风向袋杆 + 4 颗边灯球，
  共 4 mesh。**⌀28 m 的 TLOF 尺度本身是对的**（中型机位），但：坪面是零厚度
  圆片（无台体/无压顶/无坪面涂层）、无排水与检修构造、无 TLOF 着陆区灯
  （真实停机坪的核心助航设施）、边灯只有 4 颗、风向袋是「杆 + 锥」。

── §27.0-1 真实形态调研（结论先行，逐条依据见方案 46 §2.3）──────────────
  | 部件 | 真实规格                              | 依据                          | 本件取值 |
  |------|---------------------------------------|-------------------------------|----------|
  | TLOF | 按旋翼直径 D 的 1.5 倍；中型机位 ⌀15~30 m | ICAO Annex 14 Vol.II Heliports | ⌀28.0（沿用现值，已合规）|
  | FATO | 白色圆环，线宽 0.4~0.9 m              | ICAO 5.2 标线                 | R 13.0~13.7 ⇒ 线宽 0.70 m |
  | H 标识| 白色，笔画 0.4~0.5 m，高 ≈ 标称直径 1/3| ICAO 5.1.1                    | 高 9.0 m、笔画宽 0.80 m |
  | TLOF灯| 绿色内嵌着陆区灯（TLOF inset lights） | EASA CAT.437 / ICAO CAP.437   | R 11.0 均布 8 盏 ⌀0.36 |
  | 边灯 | 白色，间距 ≤7.5 m                    | ICAO 5.4                      | R 13.5 均布 12 盏，间距 7.07 m |
  | 风向袋| 红白相间 5 段，杆高 3~4 m           | 直升机场目视助航标准            | 杆 ⌀0.08×3.60；袋 5 段 ⌀0.55→0.30 |
  | 承重 | 高架式钢桁架 / 地面式须有台体与排水   | MH 5013-2023                  | 混凝土台体 0.45 + 压顶环 + 涂层坪面 |

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 **Z-up**、米制；导出自动 Yup ⇒ three.js 直立。
  · 坪面为**轴对称圆台** ⇒ 水平包围盒天然居中（center_content_xz 幂等）。
  · 节点 identity；minY = 0（台体底面贴地）。

── 材质槽（9，按材质分桶 ⇒ 9 个 primitive/DC）──────────────────────────
  HeliPad_Concrete / _ConcreteDark / _Deck / _Steel / _White /
  _TLOF_Green / _EdgeLight / _Warn / _WindsockRed（9 槽）

用法：
  blender --background --python build_heli_pad.py -- \
    ClientWeb/src/assets/models/civic/heli_pad.glb
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
    make_box, make_cylinder, make_taper, make_sphere,
    make_material, assign_material, join_objects,
    export_glb, center_content_xz, CITY_PALETTE,
)

# ── 台体（地面式停机坪：混凝土台体 + 压顶环 + 防滑涂层坪面）──────────────
DECK_R = 14.00                    # 台体半径 ⇒ TLOF 标称直径 28.0 m
BASE_H = 0.45                     # 台体高
LIP_R, LIP_H = 14.10, 0.10        # 压顶环（外挑 0.10，出挑即"女儿墙"）
SURF_R, SURF_H = 13.80, 0.04      # 涂层坪面
SURF_Z = BASE_H + SURF_H          # 涂层顶面标高 0.49
# ⚠ 标线的**叠压次序**（三者同在 ⌀26 m 内，任何一层压错都会被上层整盘盖掉）：
#   0.49 FATO 外盘(白,⌀27.4) → 0.50 FATO 内覆(坪面色,⌀26.0) → 0.53 H 标识 / 着陆区灯
#   即「环」= 外盘减内覆，「H」与绿灯必须再高一层。
FATO_OUT, FATO_IN = 13.70, 13.00  # FATO 白色圆环内外半径（线宽 0.70 m）
MARK_T = 0.03                     # 标线厚度
MARK_Z = 0.53                     # H 标识 / 着陆区灯的底面标高
H_LEG_W, H_LEG_H, H_GAP = 0.80, 9.00, 4.60   # H 笔画宽 / 高 / 两竖中心距

# ── 助航灯 ──────────────────────────────────────────────────────────────
TLOF_N, TLOF_R, TLOF_LAMP_R = 8, 11.00, 0.18
EDGE_N, EDGE_R, EDGE_LAMP_R = 12, 13.50, 0.10
EDGE_PED_R, EDGE_PED_H = 0.13, 0.26

# ── 地面设施 ────────────────────────────────────────────────────────────
SOCK_POLE_H, SOCK_POLE_R = 3.60, 0.04
SOCK_XY = (-11.50, 3.00)
SOCK_SEG = 5
SOCK_L0, SOCK_L1 = 0.55, 0.30     # 袋口 / 袋尾直径
SOCK_Z = 3.32
FIRE_BOX = (0.90, 0.60, 0.80)
SIGN_POST_H = 1.50


def main() -> None:
    reset_scene()
    set_unit_meters(1.0)

    concrete, concrete_dark, deck = [], [], []
    steel, white = [], []
    tlof_green, edge_light, warn, sock_red = [], [], [], []

    # ── ① 台体 + 压顶环 + 涂层坪面 ──────────────────────────────────────
    concrete.append(make_cylinder('PadBase', DECK_R, DECK_R, BASE_H, 48, (0, 0, BASE_H / 2.0)))
    concrete_dark.append(make_cylinder('PadLip', LIP_R, LIP_R, LIP_H, 48,
                                       (0, 0, BASE_H - LIP_H / 2.0)))
    deck.append(make_cylinder('PadSurface', SURF_R, SURF_R, SURF_H, 48,
                              (0, 0, BASE_H + SURF_H / 2.0)))

    # ── ② FATO 白色圆环（外盘 + 内覆 ⇒ 0.70 m 环带）──────────────────────
    white.append(make_cylinder('FatoRing', FATO_OUT, FATO_OUT, 0.02, 48,
                               (0, 0, SURF_Z + 0.01)))
    deck.append(make_cylinder('FatoInner', FATO_IN, FATO_IN, 0.03, 48,
                              (0, 0, SURF_Z + 0.035)))

    # ── ③ H 标识（两竖 + 一横；高 = 标称直径的 1/3 = 9.33，取 9.00）──────
    for sx in (-1, 1):
        white.append(make_box(f'HLeg_{sx}', (H_LEG_W, H_LEG_H, MARK_T),
                              (sx * H_GAP / 2.0, 0, MARK_Z + MARK_T / 2.0)))
    white.append(make_box('HCross', (H_GAP + H_LEG_W, H_LEG_W, MARK_T),
                          (0, 0, MARK_Z + MARK_T / 2.0 + 0.004)))

    # ── ④ TLOF 着陆区内嵌绿灯 ×8 ───────────────────────────────────────
    for i in range(TLOF_N):
        a = math.radians(i * 360.0 / TLOF_N)
        tlof_green.append(make_cylinder(f'Tlof_{i}', TLOF_LAMP_R, TLOF_LAMP_R, 0.05, 10,
                                        (TLOF_R * math.cos(a), TLOF_R * math.sin(a),
                                         MARK_Z + 0.025)))

    # ── ⑤ 边灯 ×12（基座 + 灯球，白色，间距 7.07 m ≤ ICAO 的 7.5 m）──────
    for i in range(EDGE_N):
        a = math.radians(i * 360.0 / EDGE_N)
        ex, ey = EDGE_R * math.cos(a), EDGE_R * math.sin(a)
        steel.append(make_cylinder(f'EdgePed_{i}', EDGE_PED_R, EDGE_PED_R, EDGE_PED_H, 8,
                                   (ex, ey, BASE_H + EDGE_PED_H / 2.0)))
        edge_light.append(make_sphere(f'EdgeLamp_{i}', EDGE_LAMP_R, 8,
                                      (ex, ey, BASE_H + EDGE_PED_H)))

    # ── ⑥ 风向袋（杆 + 5 段红白渐缩袋体）──────────────────────────────
    sx0, sy0 = SOCK_XY
    concrete_dark.append(make_cylinder('SockBase', 0.28, 0.32, 0.20, 10,
                                       (sx0, sy0, SURF_Z + 0.10)))
    steel.append(make_cylinder('SockPole', SOCK_POLE_R, SOCK_POLE_R, SOCK_POLE_H, 8,
                               (sx0, sy0, SURF_Z + SOCK_POLE_H / 2.0)))
    steel.append(make_cylinder('SockCollar', 0.07, 0.07, 0.10, 8,
                               (sx0, sy0, SOCK_Z - 0.10)))
    seg_len = 0.44
    for i in range(SOCK_SEG):
        r0 = SOCK_L0 / 2.0 + (SOCK_L1 / 2.0 - SOCK_L0 / 2.0) * (i / SOCK_SEG)
        r1 = SOCK_L0 / 2.0 + (SOCK_L1 / 2.0 - SOCK_L0 / 2.0) * ((i + 1) / SOCK_SEG)
        seg = make_taper(f'Sock_{i}', r0, r1, seg_len, 10,
                         (sx0 + 0.10 + seg_len * (i + 0.5), sy0, SOCK_Z - 0.03 * i))
        seg.rotation_euler = (0, math.pi / 2, 0)   # 圆台轴 Z → 指向 +X（顺风）
        # 红白相间 5 段：R-W-R-W-R（ICAO / MH 5013 通行做法，两端均为红）
        (sock_red if i % 2 == 0 else white).append(seg)

    # ── ⑦ 消防器材箱 + 停机坪铭牌（均在 TLOF 环带外的坪面边缘）───────────
    steel.append(make_box('FireBox', FIRE_BOX, (7.80, -11.20, SURF_Z + FIRE_BOX[2] / 2.0)))
    warn.append(make_box('FireBoxBand', (FIRE_BOX[0] + 0.04, FIRE_BOX[1] + 0.04, 0.16),
                         (7.80, -11.20, SURF_Z + FIRE_BOX[2] * 0.74)))
    steel.append(make_box('SignPost', (0.07, 0.07, SIGN_POST_H),
                          (-6.20, 12.40, SURF_Z + SIGN_POST_H / 2.0)))
    white.append(make_box('SignPlate', (0.90, 0.05, 0.60),
                          (-6.20, 12.40, SURF_Z + SIGN_POST_H + 0.25)))

    # ── ⑧ 坪边检修爬梯（沿压顶环外侧，3 级踏步 + 双立杆）───────────────
    for k, dz in enumerate((0.10, 0.30)):
        steel.append(make_box(f'LadderStep_{k}', (0.30, 0.42, 0.05),
                              (DECK_R - 0.22, 6.20, dz)))

    # ── ⑨ 按材质合并 ⇒ 8 primitive ───────────────────────────────────
    mat_c = make_material('HeliPad_Concrete_Mat', CITY_PALETTE['concrete'], rough=0.88, metal=0.0)
    mat_cd = make_material('HeliPad_ConcreteDark_Mat', CITY_PALETTE['concrete_dark'],
                           rough=0.90, metal=0.0)
    mat_d = make_material('HeliPad_Deck_Mat', CITY_PALETTE['helipad_deck'], rough=0.80, metal=0.0)
    mat_s = make_material('HeliPad_Steel_Mat', CITY_PALETTE['steel'], rough=0.45, metal=0.75)
    mat_w = make_material('HeliPad_White_Mat', CITY_PALETTE['reflect_white'], rough=0.70, metal=0.0)
    mat_g = make_material('HeliPad_TLOF_Green_Mat', CITY_PALETTE['tlof_green'],
                          rough=0.30, metal=0.0, emissive=CITY_PALETTE['tlof_green'],
                          emissive_intensity=0.5)
    mat_e = make_material('HeliPad_EdgeLight_Mat', CITY_PALETTE['lamp_warm'],
                          rough=0.30, metal=0.0, emissive=CITY_PALETTE['lamp_warm'],
                          emissive_intensity=0.5)
    mat_n = make_material('HeliPad_Warn_Mat', CITY_PALETTE['warn_amber'], rough=0.55, metal=0.20)
    mat_r = make_material('HeliPad_WindsockRed_Mat', CITY_PALETTE['windsock_red'],
                          rough=0.80, metal=0.0)
    groups = [
        (concrete, mat_c, 'HeliPad_Concrete'),
        (concrete_dark, mat_cd, 'HeliPad_ConcreteDark'),
        (deck, mat_d, 'HeliPad_Deck'),
        (steel, mat_s, 'HeliPad_Steel'),
        (white, mat_w, 'HeliPad_White'),
        (tlof_green, mat_g, 'HeliPad_TLOF_Green'),
        (edge_light, mat_e, 'HeliPad_EdgeLight'),
        (warn, mat_n, 'HeliPad_Warn'),
        (sock_red, mat_r, 'HeliPad_WindsockRed'),
    ]
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

    for obj in joined:
        min_z = min(bb[2] for bb in obj.bound_box)
        if min_z < 0:
            obj.location.z = -0.1 * min_z

    center_content_xz(joined)

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv
                else '/tmp/heli_pad.glb')
    export_glb(out_path)
    print(f'✅ heli_pad.glb exported: {out_path} '
          f'({os.path.getsize(out_path)} bytes)', flush=True)


if __name__ == '__main__':
    main()
