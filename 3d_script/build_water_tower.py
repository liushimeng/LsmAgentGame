#!/usr/bin/env python3
"""
build_water_tower — 水塔（WaterTower）Blender headless 导出脚本（19-Blender3D模型集成）。

原组件 ClientWeb/src/components/wealth/civic/WaterTower.tsx：
  - 4 斜腿（cylinder）+ 球罐 + 顶盖 + 字样带 + 检修梯（2 竖 + 6 横）
  - 全城坐标 (8, 0, -7)
"""
import bpy
import sys
import os
import math
_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (
    reset_scene, set_unit_meters,
    make_box, make_cylinder, make_sphere, make_cone, apply_pbr, join_objects, export_glb,
)

STEEL = '#5a6270'
TANK = '#a8b4be'
TANK_DARK = '#6a747e'
LETTER = '#1a3a5a'


def build_water_tower() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 4 斜腿（每腿 0.03 半径，0.8 高，从地面 (0.18, 0, ±0.18) 向中心 (0, 0.8, 0) 倾）
    legs = []
    for dx, dz in [(0.18, 0.18), (-0.18, 0.18), (0.18, -0.18), (-0.18, -0.18)]:
        # 用 box 当斜腿（更稳的视觉），长 0.85 宽 0.04
        leg = make_box(f'Leg_{dx}_{dz}', (0.04, 0.85, 0.04),
                       (dx / 2, 0.4, dz / 2))
        apply_pbr(leg, STEEL, rough=0.7, metal=0.6)
        legs.append(leg)

    # 球罐（半径 0.4，居中 y = 1.0..1.8）
    tank = make_sphere('Tank', 0.4, 16, (0, 1.4, 0))
    apply_pbr(tank, TANK, rough=0.6, metal=0.4)

    # 顶盖（小圆锥 y=1.8..1.95）
    cap = make_cone('TankCap', r=0.42, h=0.12, segs=16,
                    pos=(0, 1.86, 0))
    apply_pbr(cap, TANK_DARK, rough=0.6, metal=0.4)

    # 字样带（环 y=1.4 处 0.04m 厚；box 简化）
    band = make_box('LetterBand', (0.85, 0.06, 0.85), (0, 1.4, 0))
    apply_pbr(band, LETTER, rough=0.7, metal=0.0)

    # 检修梯（2 竖柱 + 6 横档）
    ladder = []
    for sign in (-1, 1):
        rail = make_cylinder(f'LadderRail_{sign}', 0.008, 0.008, 1.0, 6,
                             (0.4, 0.5, sign * 0.42))
        apply_pbr(rail, STEEL, rough=0.7, metal=0.5)
        ladder.append(rail)
    for i in range(6):
        rung = make_box(f'LadderRung_{i}', (0.005, 0.005, 0.07),
                        (0.4, 0.1 + i * 0.16, 0))
        apply_pbr(rung, STEEL, rough=0.7, metal=0.5)
        ladder.append(rung)

    all_objs = legs + [tank, cap, band] + ladder
    return join_objects(all_objs, 'WaterTower')


if __name__ == '__main__':
    build_water_tower()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/water_tower.glb'
    export_glb(out_path)
