#!/usr/bin/env python3
"""
build_comm_tower — 通信塔（CommTower）Blender headless 导出脚本（19-Blender3D模型集成）。

原组件 ClientWeb/src/components/wealth/civic/CommTower.tsx：
  - 4 角立柱（u(0.3) cylinder, u(20) 高）+ 6 层横撑 + 顶端机舱 + 3 面微波板 + 2 红障碍灯
  - 全城坐标 (-6, 0, 10)

GLB pivot 落 (0, 0, 0) 在地面；调用方 <Model position={[-6, 0, 10]}> 即贴位。
塔高 2.0m（含机舱），微波板 0.5m，机舱 0.4m。
"""
import bpy
import sys
import os
_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (
    reset_scene, set_unit_meters,
    make_box, make_cylinder, make_sphere, apply_pbr, join_objects, export_glb,
)

STEEL = '#7a8088'
STEEL_DARK = '#3a3f44'
DISH = '#dcdcdc'
WARNING_RED = '#ff3a2a'


def build_comm_tower() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 4 角立柱（x=±0.15, z=±0.15；高度 2.0m）
    legs = []
    for dx, dz in [(-0.15, -0.15), (-0.15, 0.15), (0.15, -0.15), (0.15, 0.15)]:
        leg = make_cylinder(f'Leg_{dx}_{dz}', 0.03, 0.03, 2.0, 8,
                           (dx, 1.0, dz))
        apply_pbr(leg, STEEL, rough=0.7, metal=0.6)
        legs.append(leg)

    # 6 层横撑（每层 y = 0.4 + i*0.3, i ∈ 0..5）
    braces = []
    for i in range(6):
        y = 0.4 + i * 0.3
        # 4 边横撑
        for (x1, z1, x2, z2) in [
            (-0.15, -0.15, -0.15, 0.15),
            (-0.15, 0.15, 0.15, 0.15),
            (0.15, 0.15, 0.15, -0.15),
            (0.15, -0.15, -0.15, -0.15),
        ]:
            cx, cz = (x1 + x2) / 2, (z1 + z2) / 2
            dx_b, dz_b = x2 - x1, z2 - z1
            length = (dx_b ** 2 + dz_b ** 2) ** 0.5
            # 旋转：默认沿 X 轴，让其朝 (dx, dz) 方向
            import math
            angle = math.atan2(dz_b, dx_b)
            brace = make_box(f'Brace_{i}', (length, 0.02, 0.02),
                             (cx, y, cz), rot=(0, 0, -angle))
            apply_pbr(brace, STEEL, rough=0.7, metal=0.6)
            braces.append(brace)

    # 顶端机舱（y = 1.95..2.05）
    cabin = make_box('Cabin', (0.4, 0.1, 0.4), (0, 2.0, 0))
    apply_pbr(cabin, STEEL_DARK, rough=0.6, metal=0.4)

    # 3 面微波板（y=2.05 周围）
    dishes = []
    for i, rot_z in enumerate([0, 2.0944, 4.1888]):  # 0, 120°, 240°
        dish = make_box(f'Dish_{i}', (0.32, 0.32, 0.04),
                        (0.25, 2.1, 0), rot=(0, 0, rot_z))
        apply_pbr(dish, DISH, rough=0.5, metal=0.3)
        dishes.append(dish)

    # 2 红障碍灯（塔顶 + 中部）
    warn1 = make_sphere('WarnLight_top', 0.04, 8, (0, 2.06, 0))
    apply_pbr(warn1, WARNING_RED, rough=0.4, metal=0.0,
              emissive=WARNING_RED, emissive_intensity=1.2)
    warn2 = make_sphere('WarnLight_mid', 0.04, 8, (0, 1.4, 0.15))
    apply_pbr(warn2, WARNING_RED, rough=0.4, metal=0.0,
              emissive=WARNING_RED, emissive_intensity=1.2)

    all_objs = legs + braces + [cabin] + dishes + [warn1, warn2]
    return join_objects(all_objs, 'CommTower')


if __name__ == '__main__':
    build_comm_tower()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/comm_tower.glb'
    export_glb(out_path)
