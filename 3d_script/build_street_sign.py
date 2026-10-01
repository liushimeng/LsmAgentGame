#!/usr/bin/env python3
"""
build_street_sign — 批次 42「街具真实感」路名牌/标志杆（street_sign.glb）Blender 导出脚本。

── §27.0-1 真实结构调研（开工前先检索参考；本次 WebSearch 无可用返回，
   改用工程标准值 + 仓库既有资产口径，结论如下）────────────────────────
  · 城市路名牌 / 指路标志杆（参考：GB 5768 道路交通标志、城市支路路名牌通用图）：
    —— 杆 ⌀60~76 mm，高 2.4~2.8 m（牌底净高 ≥2.0 m，不碰行人头）；
    —— 抱箍 2 道 ⌀80 mm 环，牌背栓接固定；
    —— 牌面 0.60×0.35 m（路名牌）/ 0.45×0.45 m（禁令），厚 2~3 mm 铝板 + 反光膜；
    —— 牌缘折边 15~20 mm、圆角 R10（真实标牌冲压折边，防割手）；
    —— 底座法兰 0.20×0.20×0.10 m + 4 膨胀螺栓（人行道直埋法兰式）；
    —— 配色：指路蓝底白字 / 信息绿底白字；反光膜微粗糙（rough ~0.35）。
  · 本件取值（宽 × 高 × 深 = 0.62 × 2.65 × 0.12 m）：
    杆 ⌀0.068 → 0.058 锥度、总高 2.55（顶过牌顶）；牌面 0.60×0.35 @ 中心高 2.32；
    折边 0.02；抱箍 ×2（⌀0.086 环）；法兰 0.20²×0.10 + 4 螺栓。
  · **双变体同 GLB**：Sign_Traffic（蓝底）/ Sign_Info（绿底）沿 X 排开 0.80 m，
    调用方按对象名取节点（与 road/trash_can.glb 双桶先例一致）。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 **Z-up**；牌面朝 glTF +Z（= Blender −Y），与 Mailbox 投信口同向约定。
  · 世界单位 ×U=0.1；节点 identity；单件 X/Z 居中、minY=0（双变体以各自中心计）。
  · 交付尺寸（单件）：x = 0.062 / y = 0.265 / z = 0.012（≡ REAL_DIMS_M.streetSign）。

用法：
  blender --background --python build_street_sign.py -- \
    ClientWeb/src/assets/models/road/street_sign.glb
"""
import os
import sys

import bpy

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (  # noqa: E402
    reset_scene, set_unit_meters,
    make_box, make_cylinder, make_material, assign_material,
    join_objects, export_glb, weathered_pbr, shared_weathered,
    flatten_weathered_materials, CITY_PALETTE,
)

U = 0.1            # 米 → 世界单位

# ── 标志杆尺寸（米）──────────────────────────────────────────────────────
POLE_H = 2.65      # 杆高（顶 2.65 = 交付 Y；初版 2.55 矮 4% 被 AABB 判据拦下）
POLE_R_BOT = 0.034 # ⌀0.068（锥度：底粗顶细）
POLE_R_TOP = 0.029
PLATE_W, PLATE_H, PLATE_T = 0.62, 0.35, 0.022   # 含折边总宽 0.62 = 交付 X
PLATE_Z = 2.32     # 牌面中心高
FOLD = 0.020       # 折边
CLAMP_Z = (2.18, 2.46)   # 抱箍两道
CLAMP_R = 0.043
# 法兰进深 0.12 = 交付 Z（初版 0.20 会让底座吃掉牌面进深口径）
BASE_W, BASE_H = 0.11, 0.10   # 0.12 时整件进深 0.0128 超契约 0.012（+6.7%）
BOLT_OFF = 0.040   # 螺栓内收（外缘 ≤0.052 < 法兰半宽 0.06，进深不被螺栓吃掉）
BOLT_R = 0.012
VARIANT_DX = 0.80  # 两变体沿 X 间距


def _m(v):
    return v * U


def _build_one(tag: str, plate_hex: str, x_offset: float):
    """单根标志杆 + 牌面（Blender Z-up，件中心 x=0 → 平移到 x_offset）。"""
    objs = []
    steel = CITY_PALETTE['steel']
    dark = CITY_PALETTE['steel_dark']

    # 杆（锥度）
    pole = make_cylinder(f'Pole_{tag}', _m(POLE_R_TOP), _m(POLE_R_BOT), _m(POLE_H), 16,
                         (0, 0, _m(POLE_H / 2)))
    objs.append(pole)
    pole_mat = make_material(f'Sign_{tag}_Pole_Mat', steel, rough=0.42, metal=0.85)
    assign_material(pole, pole_mat)

    # 法兰底座 + 4 螺栓
    base = make_box(f'Base_{tag}', (_m(BASE_W), _m(BASE_W), _m(BASE_H)),
                    (0, 0, _m(BASE_H / 2)))
    objs.append(base)
    bolt_mat = make_material(f'Sign_{tag}_Metal_Mat', steel, rough=0.45, metal=0.85)
    for i, (sx, sy) in enumerate(((-1, -1), (-1, 1), (1, -1), (1, 1))):
        bolt = make_cylinder(f'Bolt_{tag}{i}', _m(0.016), _m(0.016), _m(0.020), 8,
                             (_m(sx * BOLT_OFF), _m(sy * BOLT_OFF), _m(BASE_H + 0.010)))
        assign_material(bolt, bolt_mat)
        objs.append(bolt)

    # 抱箍 ×2（扁环：短圆柱贴杆）
    clamp_mat = make_material(f'Sign_{tag}_Struct_Mat', dark, rough=0.52, metal=0.72)
    for i, cz in enumerate(CLAMP_Z):
        clamp = make_cylinder(f'Clamp_{tag}{i}', _m(CLAMP_R), _m(CLAMP_R), _m(0.035), 16,
                              (0, 0, _m(cz)))
        assign_material(clamp, clamp_mat)
        objs.append(clamp)

    # 牌面（朝 −Y）：面板 + 折边框 + 反光膜面
    plate_back = make_box(f'Plate_{tag}', (_m(PLATE_W), _m(PLATE_T), _m(PLATE_H)),
                          (0, _m(-(POLE_R_TOP + PLATE_T / 2 + 0.008)), _m(PLATE_Z)))
    assign_material(plate_back, clamp_mat)
    objs.append(plate_back)

    face_mat = make_material(f'Sign_{tag}_Face_Mat', plate_hex,
                             rough=0.35, metal=0.05)
    face = make_box(f'Face_{tag}', (_m(PLATE_W - FOLD), _m(0.006), _m(PLATE_H - FOLD)),
                    (0, _m(-(POLE_R_TOP + PLATE_T + 0.010)), _m(PLATE_Z)))
    assign_material(face, face_mat)
    objs.append(face)

    # 白色字带（反光膜白条，做"路名文字区"示意）
    stripe = make_box(f'Stripe_{tag}', (_m(PLATE_W - FOLD * 2.2), _m(0.004), _m(PLATE_H * 0.34)),
                      (0, _m(-(POLE_R_TOP + PLATE_T + 0.015)), _m(PLATE_Z + 0.012)))
    assign_material(stripe, make_material(f'Sign_{tag}_Stripe_Mat',
                                          CITY_PALETTE['reflect_white'],
                                          rough=0.32, metal=0.05))
    objs.append(stripe)

    # 折边（牌缘四周，微凸收边）
    fold_mat = clamp_mat
    for tag2, sz, ps in (
        ('T', (PLATE_W, PLATE_T + 0.008, FOLD), (0, -(POLE_R_TOP + PLATE_T / 2 + 0.006), PLATE_Z + PLATE_H / 2 - FOLD / 2)),
        ('B', (PLATE_W, PLATE_T + 0.008, FOLD), (0, -(POLE_R_TOP + PLATE_T / 2 + 0.006), PLATE_Z - PLATE_H / 2 + FOLD / 2)),
        ('L', (FOLD, PLATE_T + 0.008, PLATE_H), (-PLATE_W / 2 + FOLD / 2, -(POLE_R_TOP + PLATE_T / 2 + 0.006), PLATE_Z)),
        ('R', (FOLD, PLATE_T + 0.008, PLATE_H), (PLATE_W / 2 - FOLD / 2, -(POLE_R_TOP + PLATE_T / 2 + 0.006), PLATE_Z)),
    ):
        fold = make_box(f'Fold_{tag}_{tag2}', (_m(sz[0]), _m(sz[1]), _m(sz[2])),
                        (_m(ps[0]), _m(ps[1]), _m(ps[2])))
        assign_material(fold, fold_mat)
        objs.append(fold)

    # 平移到变体位
    for o in objs:
        if o.name.startswith(('Base_', 'Clamp_', 'Fold_', 'Plate_')):
            assign_material(o, clamp_mat)
        o.location = (o.location[0] + _m(x_offset), o.location[1], o.location[2])
    return objs


def build_street_sign():
    reset_scene()
    set_unit_meters()

    traffic = _build_one('Traffic', CITY_PALETTE['sign_blue'], -VARIANT_DX / 2)
    join_objects(traffic, 'Sign_Traffic')

    info = _build_one('Info', CITY_PALETTE['sign_green'], VARIANT_DX / 2)
    join_objects(info, 'Sign_Info')


if __name__ == '__main__':
    build_street_sign()
    for ob in bpy.context.scene.objects:
        if ob.name in ('Sign_Traffic', 'Sign_Info'):
            flatten_weathered_materials(ob)
        bpy.ops.object.select_all(action='DESELECT')
        ob.select_set(True)
        bpy.context.view_layer.objects.active = ob
        bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/street_sign.glb'
    export_glb(out_path)
    print(f'[build_street_sign] done -> {out_path}', flush=True)
