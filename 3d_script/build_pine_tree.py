#!/usr/bin/env python3
"""
build_pine_tree — 针叶树（pine_tree.glb）Blender headless 导出脚本
（批次 26「城市坐标系统与四缘环境带」§2.3，北雪山山脚植被带 + 东森林混种用）。

⚠ 单位口径：**世界单位**（1 单位 = 10 m，见前端
  ClientWeb/src/components/virtualCity/cityScale.ts::METERS_PER_UNIT）。
  与既有 GLB（civic/city_hall、road/trash_can 等）一致。
  米制尺寸常量必须 ×U（U=0.1）换算，否则前端 scale 全错。

几何（米制 → 世界单位，Blender 原生 Z-up，pivot 在地面中心 (0,0,0)，总高 5.4m = 0.54u）：
  - 棕干：圆柱 r=0.14m，h=1.4m
  - 3 层深绿圆锥树冠（自下而上 r=1.7/1.3/0.9m，层间错落咬合）

朝向规约同 build_trash_can.py：Blender Z-up → glTF/three.js 直立贴地，
前端 <Model> / InstancedMesh 零旋转直挂。

调用：
  cd 3d_script && blender --background --python build_pine_tree.py -- <输出.glb>
"""
import bpy
import sys
import os

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (  # noqa: E402
    reset_scene, set_unit_meters,
    make_cylinder, apply_pbr, join_objects, export_glb,
)

# 米 → 世界单位（1 世界单位 = 10 m，cityScale.METERS_PER_UNIT）
U = 0.1

TRUNK = '#5a4634'
LEAF_DARK = '#2f6b33'
LEAF_MID = '#35793a'
LEAF_LIGHT = '#3f8a45'


def _cone(name: str, r_bot: float, r_top: float, h: float, segs: int, pos):
    """真圆锥（__common__.make_cone 是历史遗留直柱实现，本脚本不依赖）。入参 = 世界单位。"""
    bpy.ops.mesh.primitive_cone_add(vertices=max(segs, 8), radius1=r_bot,
                                    radius2=r_top, depth=h, location=pos)
    obj = bpy.context.active_object
    obj.name = name
    return obj


def build_pine_tree() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    objs = []

    # 主干（z ∈ [0, 0.14u]）
    trunk = make_cylinder('Trunk', 0.12 * U, 0.14 * U, 1.4 * U, 8, (0, 0, 0.7 * U))
    apply_pbr(trunk, TRUNK, rough=0.95, metal=0.0)
    objs.append(trunk)

    # 3 层圆锥树冠（层间 40% 咬合防露缝，真圆锥层叠出针叶树轮廓）
    tiers = [
        ('Tier_0', 1.7, 2.2, 0.9, LEAF_DARK),   # z ∈ [0.9, 3.1]m
        ('Tier_1', 1.3, 2.0, 2.3, LEAF_MID),    # z ∈ [2.3, 4.3]m
        ('Tier_2', 0.9, 1.8, 3.6, LEAF_LIGHT),  # z ∈ [3.6, 5.4]m
    ]
    for name, r, h, z_base, color in tiers:
        cone = _cone(name, r * U, 0.0, h * U, 8, (0, 0, (z_base + h / 2) * U))
        apply_pbr(cone, color, rough=0.9, metal=0.0)
        objs.append(cone)

    joined = join_objects(objs, 'PineTree')
    bpy.ops.object.select_all(action='DESELECT')
    joined.select_set(True)
    bpy.context.view_layer.objects.active = joined
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    return joined


if __name__ == '__main__':
    build_pine_tree()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/pine_tree.glb'
    export_glb(out_path)
    print(f'[size] {os.path.getsize(out_path)} bytes', flush=True)
