#!/usr/bin/env python3
"""
build_park_playground — 批次 45「公园设施真实感」儿童游乐组合
（civic/park_playground.glb，重制：滑梯 + 秋千同场）。

── 重制理由 ─────────────────────────────────────────────────────────────
  现状 `civic/ParkExtras.tsx::playgroundParts` = 1 根方柱 + 1 块倾斜红板（滑梯）
  + 秋千架（2 柱 + 横梁 + 2 绳 + 2 板）—— 无平台、无护栏、无爬梯踏步、
  无滑道边板，材质合并后塑料/钢构不可辨。

── §27.0-1 真实规格调研（GB/T 27689 小型游乐设施 + GB 19272 器材安全）────
  | 部件 | 真实规格                                  | 依据                     | 本件取值 |
  |------|-------------------------------------------|--------------------------|----------|
  | 平台高| 3~6 岁 0.9~1.2 m；6~12 岁 1.2~1.5 m      | GB/T 27689               | 1.20 m |
  | 滑道 | 长 ≈ 平台高×2（30°~35°），宽 0.45~0.5     | 同上                     | 2.20 m（32°），宽 0.48 + 0.15 边板 |
  | 爬梯 | 踏步间距 ≤ 0.25、踏面 ⌀≥0.025             | 同上                     | 5 踏 ⌀0.025（间距 0.20）|
  | 立柱 | 镀锌钢管 ⌀0.089~0.114                    | GB 19272-2011（φ89 采购）| ⌀0.089 |
  | 秋千 | 梁高 = 座高 0.45 + 摆动余量；A 型架       | GB 19272 挤夹间隙条款     | 梁 2.26 m、链 ⌀0.012 ×2/位、双摆位 |
  | 间距 | 滑梯与秋千跌落空间 ≥ 1.5 m                | 场地设计惯例             | 滑梯沿 +X（z=0 中线），秋千在 +Y 侧 1.35 |

  布局（Blender 坐标，米）：爬梯 (-X) → 平台 (1.05~2.05, 0) → 滑道 → 出料段
  (+4.2)；秋千架横梁沿 X 于 y=+1.35。长轴 = X 属正确摆放（axis='x_flat'）。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Z-up 米制；节点 identity；minY=0；X/Z 内容盒居中（秋千 +Y 偏置由
    center_content_xz 统一吸收）。
  · 材质槽 4：镀锌黄钢构 / 深灰钢（链条）/ 滑道红塑料 / 结构蓝塑料。

用法：
  blender --background --python build_park_playground.py -- \
    ClientWeb/src/assets/models/civic/park_playground.glb
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
    make_box, make_taper,
    make_material, assign_material, join_objects,
    export_glb, center_content_xz, CITY_PALETTE,
)

POST_R = 0.0445          # 立柱半径（⌀0.089，GB 19272 采购规格）
PLAT_H = 1.20            # 平台高
SLIDE_PITCH = math.radians(32.0)
SWING_Y = 1.35           # 秋千中线（滑梯中线为 y=0）
CHAIN_R = 0.012


def main():
    reset_scene()
    set_unit_meters()

    steel_y, steel_d, plastic_r, plastic_b = [], [], [], []

    # ── 滑梯单元（y=0 中线，沿 +X）──────────────────────────────────────
    # ① 立柱 ×4：全部通高到顶棚（2.45）—— 预览判读返工：首版前柱只到平台口
    #    1.35，顶棚前半悬挑无支撑（iso 判读「顶棚悬挑过多」即此）。
    for x, y, h in [
        (1.10, -0.45, 2.45), (1.10, 0.45, 2.45),    # 后柱（-X 侧）
        (2.00, -0.45, 2.45), (2.00, 0.45, 2.45),    # 前柱（+X 侧）
    ]:
        steel_y.append(make_taper(f'Post_{x}_{y}', POST_R, POST_R, h, 8, (x, y, h / 2)))
    # ② 平台铺板 + 三面护栏（滑道口朝 +X，爬梯口朝 -X：两端开口）
    plastic_b.append(make_box('Platform', (1.05, 1.00, 0.06), (1.55, 0, PLAT_H)))
    steel_y.append(make_box('Rail_Back', (0.05, 1.00, 0.50), (1.08, 0, PLAT_H + 0.30)))
    for s in (-1, 1):
        steel_y.append(make_box(f'Rail_Side_{s}', (0.95, 0.05, 0.50),
                                (1.55, s * 0.47, PLAT_H + 0.30)))
    # ③ 滑道（32° 下坡 + 两侧边板 + 出料段平坡）
    run, drop = 1.85, 1.15
    length = math.hypot(run, drop)                      # 2.18
    cx, cz = 2.05 + run / 2, PLAT_H - drop / 2
    plastic_r.append(make_box('SlideBed', (length + 0.06, 0.48, 0.05), (cx, 0, cz),
                              rot=(0, SLIDE_PITCH, 0)))
    # 滑道法向（局部 +Z）：(sinθ, 0, cosθ) —— 边板沿法向抬 0.10
    nx, nz = math.sin(SLIDE_PITCH), math.cos(SLIDE_PITCH)
    for s in (-1, 1):
        plastic_r.append(make_box(
            f'SlideRail_{s}', (length, 0.05, 0.16),
            (cx + nx * 0.10, s * 0.265, cz + nz * 0.10),
            rot=(0, SLIDE_PITCH, 0)))
    plastic_r.append(make_box('SlideRunout', (0.36, 0.48, 0.05), (4.02, 0, 0.05)))
    # ④ 爬梯（平台 -X 口 → 地面）：双弦杆 + 5 踏
    lad_run, lad_drop = 0.70, PLAT_H
    lad_len = math.hypot(lad_run, lad_drop)
    lad_pitch = math.atan2(lad_drop, lad_run)
    for s in (-1, 1):
        steel_y.append(make_box(
            f'LadderString_{s}', (lad_len, 0.06, 0.07),
            (1.05 - lad_run / 2, s * 0.30, PLAT_H / 2 - 0.02),
            rot=(0, lad_pitch, math.pi)))               # X 自平台口向下
    for k in range(1, 6):
        t = k / 6.0
        plastic_b.append(make_taper(
            f'Rung_{k}', 0.025, 0.025, 0.66, 8,
            (1.05 - lad_run * t, 0, PLAT_H - 0.12 - (lad_drop - 0.24) * t),
            rot=(math.pi / 2, 0, 0)))
    # ⑤ 顶棚（双坡彩板，脊沿 X）
    for s in (-1, 1):
        plastic_r.append(make_box(
            f'Canopy_{s}', (1.30, 0.72, 0.04),
            (1.55, s * 0.31, 2.36 + s * 0.0), rot=(s * math.radians(20), 0, 0)))

    # ── 秋千单元（y=+1.35 中线，横梁沿 X）──────────────────────────────
    beam_len, beam_z = 2.40, 2.26
    bx0, bx1 = 0.80 - beam_len / 2, 0.80 + beam_len / 2   # 梁 -0.40 ~ 2.00
    steel_y.append(make_taper('SwingBeam', POST_R, POST_R, beam_len, 8,
                              ((bx0 + bx1) / 2, SWING_Y, beam_z),
                              rot=(0, math.pi / 2, 0)))
    leg_len = math.hypot(0.60, beam_z)
    leg_tilt = math.atan2(0.60, beam_z)
    for x in (bx0, bx1):
        for s in (-1, 1):
            steel_y.append(make_taper(
                f'SwingLeg_{x}_{s}', POST_R, POST_R, leg_len, 8,
                (x, SWING_Y + s * 0.30, beam_z / 2),
                rot=(s * leg_tilt, 0, 0)))
    for seat_x in (0.25, 1.35):
        for s in (-1, 1):
            steel_d.append(make_taper(
                f'Chain_{seat_x}_{s}', CHAIN_R, CHAIN_R, beam_z - 0.45, 6,
                (seat_x + s * 0.18, SWING_Y, (beam_z + 0.45) / 2)))
        plastic_b.append(make_box(f'SwingSeat_{seat_x}', (0.45, 0.20, 0.05),
                                  (seat_x, SWING_Y, 0.42)))

    # ── 合并（材质槽 4）─────────────────────────────────────────────────
    mat_steel_y = make_material('Play_SteelY_Mat', CITY_PALETTE['play_steel_yellow'],
                                rough=0.45, metal=0.6)
    mat_steel_d = make_material('Play_SteelD_Mat', CITY_PALETTE['steel_dark'],
                                rough=0.35, metal=0.8)
    mat_red = make_material('Play_Slide_Mat', CITY_PALETTE['play_plastic_red'],
                            rough=0.40, metal=0.0)
    mat_blue = make_material('Play_Frame_Mat', CITY_PALETTE['play_plastic_blue'],
                             rough=0.45, metal=0.0)
    groups = [
        (steel_y, mat_steel_y, 'Playground_SteelY'),
        (steel_d, mat_steel_d, 'Playground_SteelD'),
        (plastic_r, mat_red, 'Playground_Slide'),
        (plastic_b, mat_blue, 'Playground_Frame'),
    ]
    joined = [join_objects(objs, name) for objs, _m, name in groups]
    for (_objs, mat, _name), obj in zip(groups, joined):
        assign_material(obj, mat)

    # ⚠ 全件米 → 世界单位（×0.1）：同 build_palm_tree 的 U=0.1 契约。
    # ⚠ 先 flatten（join 保留首对象变换；make_box 件的 scale 也在其中），再 ×0.1。
    for obj in joined:
        bpy.ops.object.select_all(action='DESELECT')
        obj.select_set(True)
        bpy.context.view_layer.objects.active = obj
        bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
        obj.scale = (0.1, 0.1, 0.1)

    # 贴地钉扎：倾斜件的柱帽边缘会低于 0（秋千 A 腿 / 倾斜板条端头），
    # 按「本对象 bbox 最低点」整体抬升钉扎到 minY=0（位置由导出烘焙）。
    for obj in joined:
        min_z = min(bb[2] for bb in obj.bound_box)
        if min_z < 0:
            obj.location.z = -0.1 * min_z

    # 秋千 +Y 偏置 ⇒ 内容盒 X/Z 居中
    center_content_xz(joined)

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv
                else '/tmp/park_playground.glb')
    export_glb(out_path)
    print(f'✅ park_playground.glb exported: {out_path} '
          f'({os.path.getsize(out_path)} bytes)', flush=True)


if __name__ == '__main__':
    main()
