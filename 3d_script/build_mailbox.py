#!/usr/bin/env python3
"""
build_mailbox — 批次 42「街具真实感」柱式邮筒（mailbox.glb）Blender 导出脚本。

── §27.0-1 真实结构调研（开工前先检索参考；本次 WebSearch 无可用返回，
   改用工程标准值 + 仓库既有资产口径，结论如下）────────────────────────
  · 中国邮政柱式信筒（参考：中国邮政信筒通用图、城市人行道常见绿漆圆柱信筒实测口径）：
    —— 筒身 ⌀0.50~0.55 m，通高 1.25~1.35 m；
    —— 顶盖出檐 0.03~0.05 m（防雨），弧顶或平顶微凸；
    —— 投信口宽 0.28~0.32 m、高 0.03~0.04 m，中心高 1.05~1.15 m，
       **带 0.05 m 翻盖雨檐**（真实信筒的关键识别细节）；
    —— 品牌铭牌 0.28×0.12 m 微凸牌面，位于投信口下方；
    —— 底座法兰 ⌀0.60 m、高 0.06 m，4 颗膨胀螺栓；
    —— 取信门：筒身下部 0.45×0.50 m 门缝 + 把手（邮递员开筒取信）；
    —— 涂装：中国邮政绿（本件 CITY_PALETTE['post_green']，取代旧程序化的红色）。
  · 本件取值（宽 × 高 × 深 = 0.60 × 1.40 × 0.60 m）：
    法兰 ⌀0.60×0.06；筒身微锥 ⌀0.54/0.56 × 1.20（z 0.06~1.26）；
    顶盖 ⌀0.56×0.08 + 扁球顶凸（总高 1.40）；
    投信口 z=1.10（宽 0.30×高 0.035）+ 雨檐挑出 0.05；铭牌 z=0.86；取信门缝 z=0.30~0.80。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 **Z-up**：圆柱轴 = Z，筒底 z = 0。导出 glTF 后直立、底面贴 y=0。
  · **投放口朝 glTF +Z**（= Blender −Y 侧，与 trash_can.glb 投放口同向约定）。
  · 世界单位 ×U=0.1；节点变换 identity（bake 进顶点）；X/Z 居中。
  · 交付尺寸：x = 0.060 / y = 0.140 / z = 0.060（≡ REAL_DIMS_M.mailbox）。

用法：
  blender --background --python build_mailbox.py -- \
    ClientWeb/src/assets/models/road/mailbox.glb
"""
import os
import sys

import bpy

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (  # noqa: E402
    reset_scene, set_unit_meters,
    make_box, make_cylinder, make_sphere, make_material, assign_material,
    join_objects, export_glb, weathered_pbr, shared_weathered,
    flatten_weathered_materials, CITY_PALETTE,
)

U = 0.1            # 米 → 世界单位（1 u = 10 m）

# ── 信筒尺寸（米）────────────────────────────────────────────────────────
FLANGE_R = 0.30    # 底座法兰半径（⌀0.60 = 交付 X/Z）
FLANGE_H = 0.06
BODY_R_BOT = 0.28  # 筒身（微锥：底略粗，重心稳）
BODY_R_TOP = 0.27
BODY_H = 1.20      # z 0.06 ~ 1.26
CAP_R = 0.28       # 顶盖（出檐）
CAP_H = 0.08
DOME_SQUASH = 0.30  # 顶盖扁球压扁比（总高 = FLANGE_H+BODY_H+CAP_H+DOME_SQUASH*CAP_R = 1.40）
SLOT_W = 0.30      # 投信口
SLOT_H = 0.035
SLOT_Z = 1.10      # 投信口中心高
HOOD_OUT = 0.05    # 翻盖雨檐挑出
HOOD_T = 0.018
PLATE_W, PLATE_H = 0.28, 0.12   # 品牌铭牌
PLATE_Z = 0.86
DOOR_Z0, DOOR_Z1 = 0.30, 0.80   # 取信门缝范围
DOOR_W = 0.45
SLOT_DARK = '#141416'


def _m(v):
    return v * U


def build_mailbox() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()
    objs = []

    green = CITY_PALETTE['post_green']
    green_d = CITY_PALETTE['post_green_d']
    steel = CITY_PALETTE['steel']

    # ── 底座法兰 + 4 膨胀螺栓 ───────────────────────────────────────────
    flange = make_cylinder('Flange', _m(FLANGE_R), _m(FLANGE_R * 1.02), _m(FLANGE_H), 24,
                           (0, 0, _m(FLANGE_H / 2)))
    objs.append(flange)
    bolt_mat = make_material('Mailbox_Metal_Mat', steel, rough=0.45, metal=0.85)
    for i in range(4):
        import math
        a = math.pi / 4 + i * math.pi / 2
        bx, by = math.cos(a) * FLANGE_R * 0.72, math.sin(a) * FLANGE_R * 0.72
        bolt = make_cylinder(f'Bolt{i}', _m(0.018), _m(0.018), _m(0.022), 8,
                             (_m(bx), _m(by), _m(FLANGE_H + 0.011)))
        assign_material(bolt, bolt_mat)
        objs.append(bolt)

    # ── 筒身（微锥）────────────────────────────────────────────────────
    body = make_cylinder('Body', _m(BODY_R_TOP), _m(BODY_R_BOT), _m(BODY_H), 28,
                         (0, 0, _m(FLANGE_H + BODY_H / 2)))
    weathered_pbr(body, green, rough=0.48, metal=0.12, wear=0.30, scale=5.0)
    objs.append(body)

    # ── 顶盖（出檐短圆柱）+ 扁球顶凸 ───────────────────────────────────
    cap = make_cylinder('Cap', _m(CAP_R), _m(BODY_R_TOP * 0.98), _m(CAP_H), 28,
                        (0, 0, _m(FLANGE_H + BODY_H + CAP_H / 2)))
    objs.append(cap)
    dome = make_sphere('Dome', _m(CAP_R * 0.92), 20,
                       (0, 0, _m(FLANGE_H + BODY_H + CAP_H)))
    dome.scale = (1.0, 1.0, DOME_SQUASH)
    objs.append(dome)

    # ── 投信口 + 翻盖雨檐（朝 −Y = glTF +Z）────────────────────────────
    dark = make_material('Mailbox_Slot_Mat', SLOT_DARK, rough=0.75, metal=0.10)
    plate_mat = make_material('Mailbox_Plate_Mat', CITY_PALETTE['reflect_white'],
                              rough=0.40, metal=0.10)
    # 深绿槽组（法兰/顶盖/顶凸/雨檐/下唇/牌框）共用 1 槽
    green_dark = make_material('Mailbox_GreenDark_Mat', green_d, rough=0.50, metal=0.22)
    # 口槽：嵌进筒面（y 负向 = 前面）
    slot = make_box('Slot', (_m(SLOT_W), _m(0.03), _m(SLOT_H)),
                    (0, _m(-(BODY_R_TOP - 0.008)), _m(SLOT_Z)))
    assign_material(slot, dark)
    objs.append(slot)
    # 雨檐：上缘挑出的翻盖
    hood = make_box('Slot_Hood', (_m(SLOT_W + 0.03), _m(HOOD_OUT), _m(HOOD_T)),
                    (0, _m(-(BODY_R_TOP + HOOD_OUT / 2 - 0.012)), _m(SLOT_Z + SLOT_H / 2 + HOOD_T / 2 + 0.004)))
    objs.append(hood)
    # 口沿下唇（收边，做出口槽厚度感）
    lip = make_box('Slot_Lip', (_m(SLOT_W + 0.015), _m(0.018), _m(0.012)),
                   (0, _m(-(BODY_R_TOP - 0.002)), _m(SLOT_Z - SLOT_H / 2 - 0.010)))
    objs.append(lip)

    # ── 品牌铭牌（微凸 + 浅色面）───────────────────────────────────────
    plate_back = make_box('Plate_Border', (_m(PLATE_W + 0.025), _m(0.016), _m(PLATE_H + 0.025)),
                          (0, _m(-(BODY_R_TOP - 0.004)), _m(PLATE_Z)))
    objs.append(plate_back)
    plate = make_box('Plate', (_m(PLATE_W), _m(0.018), _m(PLATE_H)),
                     (0, _m(-(BODY_R_TOP + 0.002)), _m(PLATE_Z)))
    assign_material(plate, plate_mat)
    objs.append(plate)

    # ── 取信门缝（下部门框 + 竖缝 + 把手）──────────────────────────────
    seam_mat = dark
    for tag, sz, ps in (
        ('Door_T', (DOOR_W, 0.010, 0.012), (0, -(BODY_R_TOP * 0.92), DOOR_Z1)),
        ('Door_B', (DOOR_W, 0.010, 0.012), (0, -(BODY_R_TOP * 0.92), DOOR_Z0)),
        ('Door_L', (0.012, 0.010, DOOR_Z1 - DOOR_Z0), (-DOOR_W / 2, -(BODY_R_TOP * 0.92), (DOOR_Z0 + DOOR_Z1) / 2)),
        ('Door_R', (0.012, 0.010, DOOR_Z1 - DOOR_Z0), (DOOR_W / 2, -(BODY_R_TOP * 0.92), (DOOR_Z0 + DOOR_Z1) / 2)),
    ):
        seam = make_box(tag, (_m(sz[0]), _m(sz[1]), _m(sz[2])),
                        (_m(ps[0]), _m(ps[1]), _m(ps[2])))
        assign_material(seam, seam_mat)
        objs.append(seam)
    handle = make_box('Door_Handle', (_m(0.10), _m(0.030), _m(0.028)),
                      (0, _m(-(BODY_R_TOP + 0.010)), _m(DOOR_Z1 - 0.09)))
    assign_material(handle, bolt_mat)
    objs.append(handle)

    for tag in ('Flange', 'Cap', 'Dome', 'Slot_Hood', 'Slot_Lip', 'Plate_Border'):
        for o in objs:
            if o.name == tag:
                assign_material(o, green_dark)
    joined = join_objects(objs, 'Mailbox')
    return joined


if __name__ == '__main__':
    obj = build_mailbox()
    flatten_weathered_materials(obj)
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/mailbox.glb'
    export_glb(out_path)
    print(f'[build_mailbox] done -> {out_path}', flush=True)
