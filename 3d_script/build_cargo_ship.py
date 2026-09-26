#!/usr/bin/env python3
"""
build_cargo_ship — 集装箱货船（cargo_ship.glb）Blender headless 导出脚本
（批次 26「城市坐标系统与四缘环境带」§2.3，南·海洋带慢速巡航用）。

⚠ 单位口径：**世界单位**（1 单位 = 10 m，见前端
  ClientWeb/src/components/virtualCity/cityScale.ts::METERS_PER_UNIT）。
  与既有 GLB（civic/city_hall、road/trash_can 等）一致。
  米制尺寸常量必须 ×U（U=0.1）换算，否则前端 scale 全错。

几何（米制 → 世界单位，Blender 原生 Z-up，pivot 在水线中心 (0,0,0)，
船长 60m = 6u 沿 x）：
  - 船体：黑色主箱体 60×10×4m（z ∈ [-1.4, 2.6]m，水线下吃水 1.4m）
  - 红色舷线：薄箱体环带贴水线（z ≈ 0）
  - 舰桥：白色 3 层塔楼（船尾 -x 侧），含深色窗带
  - 集装箱堆：3 色（红/蓝/绿）箱体 6×2.4×2.4m，船中 5 列 × 2 层错落

朝向规约同 build_trash_can.py：Blender Z-up → glTF/three.js 中 +x 为船头
方向、船底在水线（y=0）下，前端 <Model> 置于海面 plane 即自然吃水，
useFrame 巡航时按航向加 rotation.y。

调用：
  cd 3d_script && blender --background --python build_cargo_ship.py -- <输出.glb>
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
    make_box, apply_pbr, join_objects, export_glb,
)

# 米 → 世界单位（1 世界单位 = 10 m，cityScale.METERS_PER_UNIT）
U = 0.1

HULL = '#1c1e22'
WATERLINE = '#b03a2e'
BRIDGE = '#e8e6e0'
WINDOW = '#22303c'
CONTAINER_COLORS = ['#b03a2e', '#2e5aa8', '#3a8a45']


def build_cargo_ship() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    objs = []

    # 船体（x ∈ [-30, 30]m，z ∈ [-1.4, 2.6]m）
    hull = make_box('Hull', (60.0 * U, 10.0 * U, 4.0 * U), (0, 0, 0.6 * U))
    apply_pbr(hull, HULL, rough=0.6, metal=0.3)
    objs.append(hull)

    # 船头楔形（四棱锥横放，尖端朝 +x；先烘焙旋转再按世界轴缩放，
    # 保证横截面菱形与船体 10×4m 精确对齐、左右对称）
    bpy.ops.mesh.primitive_cone_add(vertices=4, radius1=1.0, radius2=0.0,
                                    depth=2.0, location=(0, 0, 0))
    bow = bpy.context.active_object
    bow.name = 'Bow'
    bow.rotation_euler = (0, math.radians(90), 0)   # 锥轴 → +x，尖端朝船头
    bpy.ops.object.transform_apply(rotation=True)   # 烘焙旋转，局部轴=世界轴
    bow.scale = (4.0 * U, 5.0 * U, 2.0 * U)         # 长 8m / 宽 10m / 高 4m
    bow.location = (34.0 * U, 0, 0.6 * U)           # 底面贴船体前端 x=3.0u
    apply_pbr(bow, HULL, rough=0.6, metal=0.3)
    objs.append(bow)

    # 红色舷线（贴水线 z=0 薄环带）
    stripe = make_box('WaterlineStripe', (60.4 * U, 10.4 * U, 0.6 * U), (0, 0, 0.0))
    apply_pbr(stripe, WATERLINE, rough=0.55, metal=0.2)
    objs.append(stripe)

    # 舰桥（船尾 -x 侧，3 层白色塔楼 + 深色窗带）
    bridge_1 = make_box('Bridge_1', (8.0 * U, 9.0 * U, 3.0 * U), (-24.0 * U, 0, 4.1 * U))
    apply_pbr(bridge_1, BRIDGE, rough=0.6, metal=0.05)
    objs.append(bridge_1)
    bridge_2 = make_box('Bridge_2', (6.5 * U, 8.0 * U, 2.6 * U), (-24.0 * U, 0, 6.9 * U))
    apply_pbr(bridge_2, BRIDGE, rough=0.6, metal=0.05)
    objs.append(bridge_2)
    windows = make_box('BridgeWindows', (6.7 * U, 8.2 * U, 0.9 * U), (-24.0 * U, 0, 7.6 * U))
    apply_pbr(windows, WINDOW, rough=0.25, metal=0.4)
    objs.append(windows)
    bridge_3 = make_box('Bridge_3', (5.0 * U, 6.5 * U, 2.2 * U), (-24.0 * U, 0, 9.3 * U))
    apply_pbr(bridge_3, BRIDGE, rough=0.6, metal=0.05)
    objs.append(bridge_3)

    # 烟囱（舰桥顶）
    funnel = make_box('Funnel', (2.2 * U, 3.0 * U, 2.4 * U), (-25.5 * U, 0, 11.6 * U))
    apply_pbr(funnel, WATERLINE, rough=0.6, metal=0.1)
    objs.append(funnel)

    # 集装箱堆（船中 5 列 × 2 层，3 色循环，层间错位）
    for col in range(5):
        x = -12.0 + col * 6.5          # 米
        for row in range(2):
            z = 3.8 + row * 2.5        # 米
            for lane in (-1, 1):
                color = CONTAINER_COLORS[(col + row + (0 if lane < 0 else 1)) % 3]
                box = make_box(f'Container_{col}_{row}_{lane}',
                               (6.0 * U, 4.4 * U, 2.4 * U), (x * U, lane * 2.3 * U, z * U))
                apply_pbr(box, color, rough=0.7, metal=0.15)
                objs.append(box)

    joined = join_objects(objs, 'CargoShip')
    bpy.ops.object.select_all(action='DESELECT')
    joined.select_set(True)
    bpy.context.view_layer.objects.active = joined
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    return joined


if __name__ == '__main__':
    build_cargo_ship()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/cargo_ship.glb'
    export_glb(out_path)
    print(f'[size] {os.path.getsize(out_path)} bytes', flush=True)
