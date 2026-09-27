#!/usr/bin/env python3
"""
build_fire_station — 消防站（FireStation）Blender headless 导出脚本（19-Blender3D模型集成）。

对应组件 ClientWeb/src/components/virtualCity/civic/FireStation.tsx：
  - 主屋 + 屋顶 + 白色腰线 + 2 车库门 + 滑杆塔 + 警灯 + 红消防车
  - 全城坐标 (-12, 0, 6)

坐标与尺度规约（2026-09-27 批次 19 GLB 轴向/尺度回溯修正，全目录统一）：
  - **Blender 原生 Z-up**：x = 面宽，y = 进深（**-Y = 临街正向**），z = 高度（地面 z = 0）。
    导出后 glTF X = 面宽、glTF Y = 屋高（minY = 0）、glTF Z = 进深（+Z = 临街正向）。
  - 世界单位：1 单位 = 10 m。旧版整栋侧躺 + 尺寸偏大，本次修正。

目标包围盒（世界单位）：X 1.810（面宽 18.1 m）× Z 0.800（进深 8 m）× Y 0.850（滑杆塔灯高 8.5 m），
地面中心 minY = 0。
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

WALL = '#d8d4cc'
WALL_DARK = '#9a948c'
BAND = '#f5f5f0'
DOOR_RED = '#c8302c'
LAMP_RED = '#ff3030'

# ── 尺寸（世界单位；由目标包围盒反推）────────────────────────────────────
W, D, H = 1.810, 0.800, 0.850
MAIN_W, MAIN_D, MAIN_H = 1.800, 0.780, 0.700
BAND_D, BAND_H = 0.800, 0.030        # 白色腰线（定义目标面宽 W 与进深 D）
TOWER_W, TOWER_H = 0.080, 0.800      # 滑杆塔
STREET_Y = -D / 2


def build_fire_station() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 主屋（地面 0..0.70）
    main = make_box('MainBuilding', (MAIN_W, MAIN_D, MAIN_H), (0, 0, MAIN_H / 2))
    apply_pbr(main, WALL, rough=0.9, metal=0.0)

    # 白色腰线（沿墙一圈，定义目标面宽 W 与进深 D）
    band = make_box('WhiteBand', (W, BAND_D, BAND_H), (0, 0, 0.420))
    apply_pbr(band, BAND, rough=0.9, metal=0.0)

    # 2 车库门（临街面，flush 在进深包线内）
    doors = []
    for i, dx in enumerate((-0.420, 0.420)):
        door = make_box(f'GarageDoor_{i}', (0.440, 0.020, 0.440),
                        (dx, STREET_Y + 0.010, 0.220))
        apply_pbr(door, DOOR_RED, rough=0.7, metal=0.1)
        doors.append(door)

    # 屋顶
    roof = make_box('Roof', (1.780, 0.760, 0.040), (0, 0, MAIN_H + 0.020))
    apply_pbr(roof, WALL_DARK, rough=0.7, metal=0.1)

    # 滑杆塔（0..0.80）
    tower = make_box('PoleTower', (TOWER_W, TOWER_W, TOWER_H), (0.720, -0.300, TOWER_H / 2))
    apply_pbr(tower, WALL_DARK, rough=0.7, metal=0.5)

    # 塔顶灯杆 + 警灯（顶面 = H）
    lamp_rod = make_cylinder('LampRod', 0.005, 0.005, 0.040, 8, (0.720, -0.300, 0.815))
    apply_pbr(lamp_rod, '#222222', rough=0.7, metal=0.5)
    lamp = make_sphere('WarnLamp', 0.015, 12, (0.720, -0.300, H - 0.015))
    apply_pbr(lamp, LAMP_RED, rough=0.4, metal=0.0,
              emissive=LAMP_RED, emissive_intensity=1.5)

    # 简化红消防车（单 body + 驾驶室）
    truck_body = make_box('FireTruckBody', (0.300, 0.140, 0.130), (-0.300, -0.280, 0.065))
    apply_pbr(truck_body, DOOR_RED, rough=0.6, metal=0.1)
    truck_cab = make_box('FireTruckCab', (0.100, 0.130, 0.110), (-0.400, -0.280, 0.185))
    apply_pbr(truck_cab, DOOR_RED, rough=0.6, metal=0.1)

    all_objs = [main, band, roof, tower, lamp_rod, lamp, truck_body, truck_cab] + doors
    return join_objects(all_objs, 'FireStation')


if __name__ == '__main__':
    obj = build_fire_station()
    # 烘焙 join 残留 object transform ⇒ 导出节点 identity（<Model> 零旋转零 scale 直挂）
    # 注：__common__.export_glb 自 2026-09-27 起亦统一 bake_transforms()（同一目的）；
    # 此处显式保留，既让脚本自证，也防 __common__ 行为回退时静默复发。
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/fire_station.glb'
    export_glb(out_path)
