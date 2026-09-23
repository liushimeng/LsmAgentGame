#!/usr/bin/env python3
"""
build_police_station — 警局（PoliceStation）Blender headless 导出脚本（19-Blender3D模型集成）。

原组件 ClientWeb/src/components/wealth/civic/PoliceStation.tsx：
  - 主屋 + 腰线 + 门厅雨棚 + 2 立柱 + 警灯柱 + 巡逻车
  - 全城坐标 (10, 0, 8)
"""
import bpy
import sys
import os
_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (
    reset_scene, set_unit_meters,
    make_box, make_cylinder, make_cone, apply_pbr, join_objects, export_glb,
)

WALL = '#bfc4c8'
WALL_DARK = '#6a747e'
BAND = '#1a3a5a'
DOOR_GLASS = '#1f3a52'
LAMP_BLUE = '#3060ff'
LAMP_RED = '#ff3030'
PATROL_WHITE = '#f0f0f0'
PATROL_BLUE = '#1a3a5a'


def build_police_station() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 主屋（1.2×0.7×0.8）
    main = make_box('MainBuilding', (1.2, 0.7, 0.8), (0, 0.35, 0))
    apply_pbr(main, WALL, rough=0.9, metal=0.0)

    # 屋顶
    roof = make_box('Roof', (1.24, 0.04, 0.84), (0, 0.72, 0))
    apply_pbr(roof, WALL_DARK, rough=0.7, metal=0.1)

    # 蓝色腰线
    band = make_box('BlueBand', (1.22, 0.04, 0.02), (0, 0.55, 0.41))
    apply_pbr(band, BAND, rough=0.7, metal=0.0)

    # 门厅雨棚（z=+0.5 朝街）
    canopy = make_box('PorchCanopy', (0.8, 0.04, 0.18),
                      (0, 0.74, 0.5))
    apply_pbr(canopy, WALL_DARK, rough=0.7, metal=0.1)

    # 2 立柱
    cols = []
    for dx in (-0.32, 0.32):
        c = make_box(f'PorchCol_{dx}', (0.04, 0.7, 0.04),
                     (dx, 0.35, 0.5))
        apply_pbr(c, WALL_DARK, rough=0.7, metal=0.1)
        cols.append(c)

    # 警灯柱（y=0..1.2, x=-0.55）
    lamp_pole = make_cylinder('LampPole', 0.015, 0.015, 1.2, 8,
                              (-0.55, 0.6, -0.4))
    apply_pbr(lamp_pole, '#222222', rough=0.7, metal=0.5)

    # 警灯顶（蓝红双灯）
    lamp_top = make_box('LampTop', (0.08, 0.06, 0.04), (-0.55, 1.22, -0.4))
    apply_pbr(lamp_top, LAMP_BLUE, rough=0.4, metal=0.0,
              emissive=LAMP_BLUE, emissive_intensity=1.2)
    lamp_top_r = make_box('LampTopR', (0.08, 0.06, 0.04), (-0.55, 1.16, -0.4))
    apply_pbr(lamp_top_r, LAMP_RED, rough=0.4, metal=0.0,
              emissive=LAMP_RED, emissive_intensity=1.2)

    # 巡逻车（白底蓝条）
    patrol_body = make_box('PatrolBody', (0.32, 0.14, 0.14),
                           (0.0, 0.07, 0.55))
    apply_pbr(patrol_body, PATROL_WHITE, rough=0.6, metal=0.1)
    patrol_strip = make_box('PatrolStrip', (0.32, 0.04, 0.14),
                            (0.0, 0.10, 0.55))
    apply_pbr(patrol_strip, PATROL_BLUE, rough=0.6, metal=0.1)
    patrol_cab = make_box('PatrolCab', (0.1, 0.12, 0.14),
                          (-0.2, 0.06, 0.55))
    apply_pbr(patrol_cab, PATROL_WHITE, rough=0.6, metal=0.1)

    all_objs = ([main, roof, band, canopy, lamp_pole, lamp_top, lamp_top_r,
                 patrol_body, patrol_strip, patrol_cab] + cols)
    return join_objects(all_objs, 'PoliceStation')


if __name__ == '__main__':
    build_police_station()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/police_station.glb'
    export_glb(out_path)
