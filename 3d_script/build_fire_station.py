#!/usr/bin/env python3
"""
build_fire_station — 消防站（FireStation）Blender headless 导出脚本（19-Blender3D模型集成）。

原组件 ClientWeb/src/components/wealth/civic/FireStation.tsx：
  - 主屋（box 1.4×0.6×0.8）+ 白色腰线 + 2 车库门 + 滑杆塔 + 警灯 + 红消防车
  - 全城坐标 (-12, 0, 6)
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

WALL = '#d8d4cc'
WALL_DARK = '#9a948c'
BAND = '#f5f5f0'
DOOR_RED = '#c8302c'
DOOR_GRAY = '#3a3a3a'
LAMP_BLUE = '#3060ff'
LAMP_RED = '#ff3030'


def build_fire_station() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 主屋（1.4×0.6×0.8，地面 0..0.6）
    main = make_box('MainBuilding', (1.4, 0.6, 0.8), (0, 0.3, 0))
    apply_pbr(main, WALL, rough=0.9, metal=0.0)

    # 屋顶（坡顶伪装 0.04 厚）
    roof = make_box('Roof', (1.44, 0.04, 0.84), (0, 0.62, 0))
    apply_pbr(roof, WALL_DARK, rough=0.7, metal=0.1)

    # 白色腰线（y=0.4, z=0.4 朝街）
    band = make_box('WhiteBand', (1.42, 0.04, 0.02), (0, 0.4, 0.41))
    apply_pbr(band, BAND, rough=0.9, metal=0.0)

    # 2 车库门（z=0.4 朝街）
    doors = []
    for dx in (-0.4, 0.4):
        door = make_box(f'GarageDoor_{dx}', (0.4, 0.36, 0.02),
                        (dx, 0.18, 0.41))
        apply_pbr(door, DOOR_RED, rough=0.7, metal=0.1)
        doors.append(door)

    # 滑杆塔（y=0..1.4, x=0.65）
    pole_tower = make_box('PoleTower', (0.06, 1.4, 0.06), (0.65, 0.7, -0.35))
    apply_pbr(pole_tower, WALL_DARK, rough=0.7, metal=0.5)

    # 警灯（塔顶）
    lamp = make_cylinder('WarnLamp', 0.04, 0.04, 0.04, 8, (0.65, 1.42, -0.35))
    apply_pbr(lamp, LAMP_RED, rough=0.4, metal=0.0,
              emissive=LAMP_RED, emissive_intensity=1.5)

    # 简化红消防车（单 box 替代原组件 FireTruck）
    truck_body = make_box('FireTruckBody', (0.3, 0.16, 0.14),
                          (0, 0.08, 0.55))
    apply_pbr(truck_body, DOOR_RED, rough=0.6, metal=0.1)
    truck_cab = make_box('FireTruckCab', (0.1, 0.14, 0.14),
                         (-0.18, 0.07, 0.55))
    apply_pbr(truck_cab, DOOR_RED, rough=0.6, metal=0.1)

    all_objs = [main, roof, band, pole_tower, lamp, truck_body, truck_cab] + doors
    return join_objects(all_objs, 'FireStation')


if __name__ == '__main__':
    build_fire_station()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/fire_station.glb'
    export_glb(out_path)
