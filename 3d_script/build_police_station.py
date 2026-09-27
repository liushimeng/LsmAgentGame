#!/usr/bin/env python3
"""
build_police_station — 警局（PoliceStation）Blender headless 导出脚本（19-Blender3D模型集成）。

对应组件 ClientWeb/src/components/virtualCity/civic/PoliceStation.tsx：
  - 主屋 + 屋顶檐口 + 蓝色腰线 + 门厅雨棚 + 2 立柱 + 警灯柱（红蓝双灯）+ 巡逻车
  - 全城坐标 (10, 0, 8)

坐标与尺度规约（2026-09-27 批次 19 GLB 轴向/尺度回溯修正，全目录统一）：
  - **Blender 原生 Z-up**：x = 面宽，y = 进深（**-Y = 临街正向**），z = 高度（地面 z = 0）。
    导出后 glTF X = 面宽、glTF Y = 屋高（minY = 0）、glTF Z = 进深（+Z = 临街正向）。
  - 世界单位：1 单位 = 10 m。旧版整栋侧躺 + 尺寸 1.5~2.5× 偏大，本次修正。

目标包围盒（世界单位）：X 1.210（面宽 12.1 m）× Z 0.700（进深 7 m）× Y 0.660（高 6.6 m），
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
    make_box, make_cylinder, apply_pbr, join_objects, export_glb,
)

WALL = '#bfc4c8'
WALL_DARK = '#6a747e'
BAND = '#1a3a5a'
DOOR_GLASS = '#1f3a52'
LAMP_BLUE = '#3060ff'
LAMP_RED = '#ff3030'
PATROL_WHITE = '#f0f0f0'

# ── 尺寸（世界单位；由目标包围盒反推）────────────────────────────────────
W, D, H = 1.210, 0.700, 0.660        # 面宽 / 进深 / 总高（雨棚与警灯顶面）
MAIN_W, MAIN_D, MAIN_H = 1.160, 0.620, 0.580
ROOF_D, ROOF_H = 0.040, 0.040        # 檐口（0.58..0.62，定义 W / D）
STREET_Y = -D / 2                    # 临街面


def build_police_station() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 主屋
    main = make_box('MainBuilding', (MAIN_W, MAIN_D, MAIN_H), (0, 0, MAIN_H / 2))
    apply_pbr(main, WALL, rough=0.9, metal=0.0)

    # 屋顶檐口（定义目标面宽 W 与进深 D）
    roof = make_box('Roof', (W, D, ROOF_H), (0, 0, MAIN_H + ROOF_H / 2))
    apply_pbr(roof, WALL_DARK, rough=0.7, metal=0.1)

    # 蓝色腰线（沿墙一圈，略凸出主屋墙面）
    band = make_box('BlueBand', (MAIN_W + 0.010, MAIN_D + 0.020, 0.040),
                    (0, 0, 0.420))
    apply_pbr(band, BAND, rough=0.7, metal=0.0)

    # 门厅雨棚（临街侧；顶面 = H）
    canopy = make_box('PorchCanopy', (0.800, 0.120, 0.040),
                      (0, STREET_Y + 0.060, MAIN_H + ROOF_H + 0.020))
    apply_pbr(canopy, WALL_DARK, rough=0.7, metal=0.1)

    # 2 立柱
    cols = []
    for dx in (-0.32, 0.32):
        c = make_box(f'PorchCol_{dx}', (0.050, 0.050, MAIN_H), (dx, STREET_Y + 0.060, MAIN_H / 2))
        apply_pbr(c, WALL_DARK, rough=0.7, metal=0.1)
        cols.append(c)

    # 警灯柱（红蓝双灯；顶面 = H）
    lamp_pole = make_cylinder('LampPole', 0.015, 0.015, 0.620, 10,
                              (-0.550, 0.300, 0.310))
    apply_pbr(lamp_pole, '#222222', rough=0.7, metal=0.5)

    lamp_blue = make_box('LampTopBlue', (0.080, 0.080, 0.050), (-0.550, 0.300, H - 0.025))
    apply_pbr(lamp_blue, LAMP_BLUE, rough=0.4, metal=0.0,
              emissive=LAMP_BLUE, emissive_intensity=1.2)
    lamp_red = make_box('LampTopRed', (0.080, 0.080, 0.050), (-0.550, 0.300, H - 0.075))
    apply_pbr(lamp_red, LAMP_RED, rough=0.4, metal=0.0,
              emissive=LAMP_RED, emissive_intensity=1.2)

    # 巡逻车（白底蓝条，停在门前）
    patrol_body = make_box('PatrolBody', (0.320, 0.140, 0.130), (0.300, -0.240, 0.065))
    apply_pbr(patrol_body, PATROL_WHITE, rough=0.6, metal=0.1)
    patrol_strip = make_box('PatrolStrip', (0.100, 0.145, 0.040), (0.300, -0.240, 0.090))
    apply_pbr(patrol_strip, BAND, rough=0.6, metal=0.1)
    patrol_cab = make_box('PatrolCab', (0.100, 0.130, 0.080), (0.220, -0.240, 0.170))
    apply_pbr(patrol_cab, PATROL_WHITE, rough=0.6, metal=0.1)

    all_objs = ([main, roof, band, canopy, lamp_pole, lamp_blue, lamp_red,
                 patrol_body, patrol_strip, patrol_cab] + cols)
    return join_objects(all_objs, 'PoliceStation')


if __name__ == '__main__':
    obj = build_police_station()
    # 烘焙 join 残留 object transform ⇒ 导出节点 identity（<Model> 零旋转零 scale 直挂）
    # 注：__common__.export_glb 自 2026-09-27 起亦统一 bake_transforms()（同一目的）；
    # 此处显式保留，既让脚本自证，也防 __common__ 行为回退时静默复发。
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/police_station.glb'
    export_glb(out_path)
