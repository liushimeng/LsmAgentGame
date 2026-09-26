#!/usr/bin/env python3
"""
build_sailboat — 小帆船（sailboat.glb）Blender headless 导出脚本
（批次 26「城市坐标系统与四缘环境带」§2.3，南·海洋带近岸点缀 ×2 用）。

⚠ 单位口径：**世界单位**（1 单位 = 10 m，见前端
  ClientWeb/src/components/virtualCity/cityScale.ts::METERS_PER_UNIT）。
  与既有 GLB（civic/city_hall、road/trash_can 等）一致。
  米制尺寸常量必须 ×U（U=0.1）换算，否则前端 scale 全错。

几何（米制 → 世界单位，Blender 原生 Z-up，pivot 在水线中心 (0,0,0)，
船长 8m = 0.8u 沿 x）：
  - 船体：白色箱体 8×2.2×1.2m（z ∈ [-0.5, 0.7]m）+ 船头四棱锥楔
  - 桅杆：圆柱 r=0.07m h=7.5m
  - 主帆：三棱锥压扁（奶白色），三角后掠
  - 前帆：小三棱锥压扁（同色系略深）

朝向规约同 build_trash_can.py：Blender Z-up → glTF/three.js 中 +x 为船头、
船底贴水线，前端 <Model> 置于海面 plane 即自然浮水。

调用：
  cd 3d_script && blender --background --python build_sailboat.py -- <输出.glb>
"""
import bpy
import sys
import os
import math

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (  # noqa: E402
    reset_scene, set_unit_meters,
    make_box, make_cylinder, apply_pbr, join_objects, export_glb,
)

# 米 → 世界单位（1 世界单位 = 10 m，cityScale.METERS_PER_UNIT）
U = 0.1

HULL = '#f2f0ea'
DECK = '#d8cfc0'
MAST = '#8a6f4d'
SAIL = '#f6f2e6'
SAIL_JIB = '#ede6d2'


def _triangle_sail(name: str, r: float, h: float, pos, color, thin: float = 0.06):
    """三棱锥（vertices=3）压扁成三角帆，默认轴沿 z（竖立）。入参单位 = 世界单位。"""
    bpy.ops.mesh.primitive_cone_add(vertices=3, radius1=r, radius2=0.0,
                                    depth=h, location=(0, 0, 0))
    obj = bpy.context.active_object
    obj.name = name
    obj.scale = (1.0, thin, 1.0)   # 沿 y 压扁 → 帆面朝向 ±y
    obj.location = pos
    apply_pbr(obj, color, rough=0.75, metal=0.0)
    return obj


def build_sailboat() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    objs = []

    # 船体（x ∈ [-4, 4]m，z ∈ [-0.5, 0.7]m）
    hull = make_box('Hull', (8.0 * U, 2.2 * U, 1.2 * U), (0, 0, 0.1 * U))
    apply_pbr(hull, HULL, rough=0.5, metal=0.05)
    objs.append(hull)

    # 船头楔（四棱锥横放，尖端 +x；先烘焙旋转再按世界轴缩放，
    # 横截面菱形与船体 2.2×1.2m 精确对齐、左右对称）
    bpy.ops.mesh.primitive_cone_add(vertices=4, radius1=1.0, radius2=0.0,
                                    depth=2.0, location=(0, 0, 0))
    bow = bpy.context.active_object
    bow.name = 'Bow'
    bow.rotation_euler = (0, math.radians(90), 0)   # 锥轴 → +x
    bpy.ops.object.transform_apply(rotation=True)   # 烘焙旋转，局部轴=世界轴
    bow.scale = (1.0 * U, 1.1 * U, 0.6 * U)         # 长 2m / 宽 2.2m / 高 1.2m
    bow.location = (5.0 * U, 0, 0.1 * U)            # 底面贴船体前端 x=0.4u
    apply_pbr(bow, HULL, rough=0.5, metal=0.05)
    objs.append(bow)

    # 甲板（薄板）
    deck = make_box('Deck', (7.6 * U, 1.9 * U, 0.15 * U), (0, 0, 0.75 * U))
    apply_pbr(deck, DECK, rough=0.7, metal=0.0)
    objs.append(deck)

    # 桅杆（z ∈ [0.07u, 0.82u]，略偏船头）
    mast = make_cylinder('Mast', 0.06 * U, 0.07 * U, 7.5 * U, 8, (0.6 * U, 0, 4.45 * U))
    apply_pbr(mast, MAST, rough=0.8, metal=0.0)
    objs.append(mast)

    # 主帆（桅杆后侧 -x；帆面落在 x-z 平面）
    mainsail = _triangle_sail('MainSail', 3.2 * U, 5.5 * U, (-0.9 * U, 0, 3.95 * U), SAIL)
    objs.append(mainsail)

    # 前帆（桅杆前侧 +x，较小）
    jib = _triangle_sail('Jib', 2.0 * U, 3.6 * U, (1.9 * U, 0, 3.2 * U), SAIL_JIB)
    objs.append(jib)

    joined = join_objects(objs, 'Sailboat')
    bpy.ops.object.select_all(action='DESELECT')
    joined.select_set(True)
    bpy.context.view_layer.objects.active = joined
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    return joined


if __name__ == '__main__':
    build_sailboat()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/sailboat.glb'
    export_glb(out_path)
    print(f'[size] {os.path.getsize(out_path)} bytes', flush=True)
