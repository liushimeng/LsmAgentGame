#!/usr/bin/env python3
"""
build_bridge_rail — 批次 38 运河桥栏杆段（bridge_rail.glb），沿桥长平铺。

── §27.0-1 真实结构调研 ─────────────────────────────────────────────────
  · 城市桥梁人行护栏（参考：CJJ 城市桥梁设计规范人行道栏杆、常规钢栏杆详图）
    —— 栏杆总高 1.10~1.30 m（本件取 1.15 m）；立柱间距 1.5~2.5 m（本件 2.0 m）；
    —— 扶手（top rail）宽 8~12 cm；中间横杆 1~2 道，或竖向栏杆（baluster）；
    —— 钢质栏杆立柱 8~12 cm 见方。
  · 仓库口径对齐：材质取 CITY_PALETTE['steel'] / ['steel_dark']（CLAUDE.md §27.3）；
    与 CanalExtras 两岸护栏柱（1.2 m 高、钢灰）同族。

── 段长与拼接（可沿桥长平铺）────────────────────────────────────────────
  · 段长 2.0 m：立柱 1 根位于段**中心**（x=0），横杆跨满 2.0 m。
    平铺时相邻段的横杆首尾相接、立柱每 2.0 m 一根 —— 立柱若放段两端会
    在拼接处重合 z-fight，故放中心（同路灯/垃圾桶的实例化常识）。
  · 竖杆 2 根位于 x = ±0.65，段拼接后每 2 m 出现 2 根，疏密接近真实栏杆。

── 坐标与尺度规约（§27.3）───────────────────────────────────────────────
  · Blender Z-up：X = 桥长（平铺方向），Y = 桥宽向（栏杆厚度方向），Z = 高。
  · 1 世界单位 = 10 m ⇒ 全部尺寸 ×U=0.1。禁止按真实米直接导出。
  · 节点变换 identity（export_glb 内置 bake_transforms）；minY = 0、X/Z 居中。

── 交付尺寸（世界单位 / 真实米）──────────────────────────────────────────
  段长 X = 0.200 u（2.0 m）；栏杆高 Y = 0.115 u（1.15 m）；厚 Z ≈ 0.012 u（0.12 m）。
  主导轴 X（段长 > 栏高）⇒ verify_glb_aabb 需 axis='x_flat' 判据（长条件正确摆放）。

用法：
  blender --background --python build_bridge_rail.py -- \
    ClientWeb/src/assets/models/road/bridge_rail.glb
"""
import os
import sys

import bpy

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (  # noqa: E402
    reset_scene, set_unit_meters,
    make_box, join_objects, export_glb, weathered_pbr, CITY_PALETTE,
)

U = 0.1  # 米 → 世界单位

# ── 断面尺寸（米）─────────────────────────────────────────────────────────
SEG_LEN = 2.0        # 段长（平铺周期）
RAIL_H = 1.15        # 栏杆总高
POST_W = 0.10        # 立柱见方
POST_D = 0.10
TOP_W, TOP_H = 0.12, 0.08    # 扶手宽×高（顶面 = RAIL_H）
MID_W, MID_H = 0.07, 0.05    # 中间横杆
MID_Z = 0.68                 # 中间横杆底高
BAL_W = 0.045                # 竖杆见方
BAL_H = RAIL_H - TOP_H       # 竖杆高 = 扶手底（底 z = 0 贴桥面）
BAL_XS = (-0.65, 0.65)       # 竖杆位置（段内）


def _m(v):
    return v * U


def flatten_weathered_materials(obj):
    """weathered_pbr 的 Mix/Noise 链会让 glTF 导出丢掉 Base Color（导出白模，批次 38 实测）。
    导出前解链回常量：基色取 Mix 的 A 输入（CITY_PALETTE 值），粗糙度取 Math 的 addend。
    磨损只存在于 Blender 场景预览（glTF 无 Noise 对应物）。与 build_canal_bank 同文。"""
    for mat in obj.data.materials:
        if not mat or not mat.use_nodes:
            continue
        bsdf = mat.node_tree.nodes.get('Principled BSDF')
        if bsdf is None:
            continue
        bc = bsdf.inputs['Base Color']
        if bc.is_linked:
            node = bc.links[0].from_node
            col = (1.0, 1.0, 1.0, 1.0)
            if node.bl_idname == 'ShaderNodeMix':
                for s in node.inputs:
                    if s.name == 'A' and s.type == 'RGBA':
                        col = tuple(s.default_value)
                        break
            for lk in list(bc.links):
                mat.node_tree.links.remove(lk)
            bc.default_value = col
        rc = bsdf.inputs['Roughness']
        if rc.is_linked:
            node = rc.links[0].from_node
            rough = 0.8
            if node.bl_idname == 'ShaderNodeMath':
                rough = float(node.inputs[2].default_value)
            for lk in list(rc.links):
                mat.node_tree.links.remove(lk)
            rc.default_value = rough


def build_bridge_rail() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()
    objs = []

    # 立柱（段中心，x=0；钢深）
    post = make_box('Post',
                    (_m(POST_W), _m(POST_D), _m(RAIL_H)),
                    (0, 0, _m(RAIL_H / 2)))
    weathered_pbr(post, CITY_PALETTE['steel_dark'], rough=0.55, metal=0.85,
                  wear=0.30, scale=8.0)
    objs.append(post)

    # 扶手（跨满段长；顶面 = RAIL_H）
    top = make_box('TopRail',
                   (_m(SEG_LEN), _m(TOP_W), _m(TOP_H)),
                   (0, 0, _m(RAIL_H - TOP_H / 2)))
    weathered_pbr(top, CITY_PALETTE['steel'], rough=0.45, metal=0.90,
                  wear=0.25, scale=6.0)
    objs.append(top)

    # 中间横杆
    mid = make_box('MidRail',
                   (_m(SEG_LEN), _m(MID_W), _m(MID_H)),
                   (0, 0, _m(MID_Z + MID_H / 2)))
    weathered_pbr(mid, CITY_PALETTE['steel'], rough=0.45, metal=0.90,
                  wear=0.25, scale=6.0)
    objs.append(mid)

    # 竖杆 ×2（下端抵桥面，上端接扶手底）
    for i, bx in enumerate(BAL_XS):
        bal = make_box(f'Baluster{i}',
                       (_m(BAL_W), _m(BAL_W), _m(BAL_H)),
                       (_m(bx), 0, _m(RAIL_H - TOP_H - BAL_H / 2)))
        weathered_pbr(bal, CITY_PALETTE['steel_dark'], rough=0.55, metal=0.85,
                      wear=0.30, scale=8.0)
        objs.append(bal)

    joined = join_objects(objs, 'BridgeRail')
    return joined


if __name__ == '__main__':
    obj = build_bridge_rail()
    flatten_weathered_materials(obj)   # 否则 GLB 白模（见函数文档）
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/bridge_rail.glb'
    export_glb(out_path)
