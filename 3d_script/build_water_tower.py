#!/usr/bin/env python3
"""
build_water_tower — 水塔（WaterTower）Blender headless 导出脚本（19-Blender3D模型集成）。

对应组件 ClientWeb/src/components/virtualCity/civic/WaterTower.tsx：
  - 4 斜腿 + 球罐 + 顶盖 + 字样带 + 检修梯（2 竖 + 5 横）
  - 全城坐标 (-22, 0, 2)

坐标与尺度规约（2026-09-27 批次 19 GLB 轴向/尺度回溯修正，全目录统一）：
  - **Blender 原生 Z-up**：x / y = 水平两向，z = 高度（地面 z = 0）。
    导出后 glTF X / Z = 水平、glTF Y = 高（minY = 0）。
  - 世界单位：1 单位 = 10 m。旧版把高度写在位置向量第 1 位（作者自定 Y-up），
    导出后整塔侧躺 + 尺寸 1.7× 偏大，本次修正。

目标包围盒（世界单位）：X 0.360（罐体 ⌀ 3.6 m）× Z 0.360 × Y 1.330（高 13.3 m），
地面中心 minY = 0。球罐 + 字样带共面给出 ⌀，顶珠给出总高。
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

STEEL = '#5a6270'
TANK = '#a8b4be'
TANK_DARK = '#6a747e'
LETTER = '#1a3a5a'

# ── 尺寸（世界单位；由目标包围盒反推）────────────────────────────────────
TANK_R = 0.180        # 球罐半径 ⇒ ⌀ = 0.360 = 目标 X / Z
TANK_Z = 1.060        # 球心高（0.880..1.240）
BAND_H = 0.060
CAP_R, CAP_H = 0.050, 0.060
FINIAL_R = 0.015
H = 1.330             # 目标总高（顶珠顶面；罐顶 1.295 → 顶珠 1.300..1.330）
LEG_X, LEG_W, LEG_H = 0.130, 0.030, 0.900
RAIL_X, RAIL_R, RAIL_H = TANK_R - 0.008, 0.008, 0.800


def build_water_tower() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 1. 4 支腿（方柱，内收在罐体投影内）
    legs = []
    for dx, dy in [(-LEG_X, -LEG_X), (-LEG_X, LEG_X), (LEG_X, -LEG_X), (LEG_X, LEG_X)]:
        leg = make_box(f'Leg_{dx:.2f}_{dy:.2f}', (LEG_W, LEG_W, LEG_H),
                       (dx, dy, LEG_H / 2))
        apply_pbr(leg, STEEL, rough=0.7, metal=0.6)
        legs.append(leg)

    # 2. 球罐（半径即目标半宽）
    tank = make_sphere('Tank', TANK_R, 20, (0, 0, TANK_Z))
    apply_pbr(tank, TANK, rough=0.6, metal=0.4)

    # 3. 字样带（沿罐体赤道的矮圆柱环，与罐体共面 ⇒ 不越包线）
    band = make_cylinder('LetterBand', TANK_R, TANK_R, BAND_H, 24, (0, 0, TANK_Z))
    apply_pbr(band, LETTER, rough=0.7, metal=0.0)

    # 4. 顶盖（罐顶小圆柱）
    cap = make_cylinder('TankCap', CAP_R, CAP_R, CAP_H, 16,
                        (0, 0, TANK_Z + TANK_R + CAP_H / 2 - 0.005))
    apply_pbr(cap, TANK_DARK, rough=0.6, metal=0.4)

    # 5. 顶珠（顶面 = 总高 H）
    finial = make_sphere('Finial', FINIAL_R, 10, (0, 0, H - FINIAL_R))
    apply_pbr(finial, TANK_DARK, rough=0.5, metal=0.5)

    # 6. 检修梯（2 竖 + 5 横，贴罐体外侧但不越 ⌀）
    ladder = []
    for sign in (-1, 1):
        rail = make_cylinder(f'LadderRail_{sign}', RAIL_R, RAIL_R, RAIL_H, 8,
                             (RAIL_X, sign * 0.030, 0.100 + RAIL_H / 2))
        apply_pbr(rail, STEEL, rough=0.7, metal=0.5)
        ladder.append(rail)
    for i in range(5):
        rung = make_box(f'LadderRung_{i}', (0.012, 0.060, 0.012),
                        (RAIL_X, 0, 0.180 + i * 0.160))
        apply_pbr(rung, STEEL, rough=0.7, metal=0.5)
        ladder.append(rung)

    all_objs = legs + [tank, band, cap, finial] + ladder
    return join_objects(all_objs, 'WaterTower')


if __name__ == '__main__':
    obj = build_water_tower()
    # 烘焙 join 残留 object transform ⇒ 导出节点 identity（<Model> 零旋转零 scale 直挂）
    # 注：__common__.export_glb 自 2026-09-27 起亦统一 bake_transforms()（同一目的）；
    # 此处显式保留，既让脚本自证，也防 __common__ 行为回退时静默复发。
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/water_tower.glb'
    export_glb(out_path)
