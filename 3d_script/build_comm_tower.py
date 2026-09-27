#!/usr/bin/env python3
"""
build_comm_tower — 通信塔（CommTower）Blender headless 导出脚本（19-Blender3D模型集成）。

对应组件 ClientWeb/src/components/virtualCity/civic/CommTower.tsx：
  - 4 角立柱 + 6 层横撑 + 塔基 4 垫脚 + 顶端机舱 + 3 面微波板 + 2 红障碍灯
  - 全城坐标 (-6, 0, 10)

坐标与尺度规约（2026-09-27 批次 19 GLB 轴向/尺度回溯修正，全目录统一）：
  - **Blender 原生 Z-up**：x / y = 水平两向，z = 高度（地面 z = 0）。
    导出后 glTF X / Z = 水平、glTF Y = 高（minY = 0）。
  - 世界单位：1 单位 = 10 m。旧版把塔高 2.0 写在位置向量第 1 位（作者自定 Y-up），
    导出后整塔侧躺（高 20.7 m 躺在 glTF Z 轴上）+ 塔身过粗，本次修正。

目标包围盒（世界单位）：X 1.000（塔基 10 m 见方）× Z 1.000 × Y 2.050（高 20.5 m），
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

STEEL = '#7a8088'
STEEL_DARK = '#3a3f44'
DISH = '#dcdcdc'
WARNING_RED = '#ff3a2a'

# ── 尺寸（世界单位；由目标包围盒反推）────────────────────────────────────
FOOT = 0.500          # 塔基外缘半宽 ⇒ X = Z = 1.000
LEG_X, LEG_R = 0.440, 0.030
BASE_PAD, PAD_H = 0.160, 0.040
LEG_H = 2.000         # 立柱高
BRACE_LEVELS = (0.40, 0.70, 1.00, 1.30, 1.60, 1.85)
BRACE_T = 0.020
CABIN_W, CABIN_H = 0.600, 0.110       # 机舱（1.93..2.04）
DISH_T, DISH_S = 0.020, 0.300
H = 2.050             # 总高（含顶灯）


def build_comm_tower() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 1. 4 角立柱（0..2.00）
    legs = []
    for dx, dy in [(-LEG_X, -LEG_X), (-LEG_X, LEG_X), (LEG_X, -LEG_X), (LEG_X, LEG_X)]:
        leg = make_cylinder(f'Leg_{dx:.2f}_{dy:.2f}', LEG_R, LEG_R, LEG_H, 10,
                            (dx, dy, LEG_H / 2))
        apply_pbr(leg, STEEL, rough=0.7, metal=0.6)
        legs.append(leg)

    # 2. 塔基垫脚 ×4（外缘 = ±FOOT ⇒ 定义目标 X / Z）
    pads = []
    for dx, dy in [(-1, -1), (-1, 1), (1, -1), (1, 1)]:
        pad = make_box(f'BasePad_{dx}_{dy}', (BASE_PAD, BASE_PAD, PAD_H),
                       (dx * (FOOT - BASE_PAD / 2), dy * (FOOT - BASE_PAD / 2), PAD_H / 2))
        apply_pbr(pad, STEEL_DARK, rough=0.9, metal=0.2)
        pads.append(pad)

    # 3. 6 层横撑（每层 4 边：2 沿 X + 2 沿 Y）
    braces = []
    span = 2 * LEG_X
    for i, z in enumerate(BRACE_LEVELS):
        for sy in (1, -1):
            bx = make_box(f'BraceX_{i}_{sy}', (span, BRACE_T, BRACE_T), (0, sy * LEG_X, z))
            apply_pbr(bx, STEEL, rough=0.7, metal=0.6)
            braces.append(bx)
        for sx in (1, -1):
            by = make_box(f'BraceY_{i}_{sx}', (BRACE_T, span, BRACE_T), (sx * LEG_X, 0, z))
            apply_pbr(by, STEEL, rough=0.7, metal=0.6)
            braces.append(by)

    # 4. 顶端机舱（1.93..2.04）
    cabin = make_box('Cabin', (CABIN_W, CABIN_W, CABIN_H), (0, 0, LEG_H - CABIN_H / 2 + 0.04))
    apply_pbr(cabin, STEEL_DARK, rough=0.6, metal=0.4)

    # 5. 3 面微波板（贴在塔身外缘，收在 ±FOOT 内）
    dishes = []
    dish_z = 1.600
    for name, (px, py) in (('Dish_Xp', (FOOT - DISH_T / 2, 0)),
                           ('Dish_Xn', (-(FOOT - DISH_T / 2), 0)),
                           ('Dish_Yp', (0, FOOT - DISH_T / 2))):
        # 面朝 ±X 的板：薄向 = X；面朝 +Y 的板：薄向 = Y
        dish = make_box(name, (DISH_T, DISH_S, DISH_S) if px else (DISH_S, DISH_T, DISH_S),
                        (px, py, dish_z))
        apply_pbr(dish, DISH, rough=0.5, metal=0.3)
        dishes.append(dish)

    # 6. 航空障碍灯 ×2（塔顶灯定义总高 H）
    warn_top = make_sphere('WarnLight_top', 0.012, 12, (0, 0, H - 0.012))
    apply_pbr(warn_top, WARNING_RED, rough=0.4, metal=0.0,
              emissive=WARNING_RED, emissive_intensity=1.2)
    warn_mid = make_sphere('WarnLight_mid', 0.010, 10, (LEG_X, 0, 1.200))
    apply_pbr(warn_mid, WARNING_RED, rough=0.4, metal=0.0,
              emissive=WARNING_RED, emissive_intensity=1.2)

    all_objs = legs + pads + braces + [cabin] + dishes + [warn_top, warn_mid]
    return join_objects(all_objs, 'CommTower')


if __name__ == '__main__':
    obj = build_comm_tower()
    # 烘焙 join 残留 object transform ⇒ 导出节点 identity（<Model> 零旋转零 scale 直挂）
    # 注：__common__.export_glb 自 2026-09-27 起亦统一 bake_transforms()（同一目的）；
    # 此处显式保留，既让脚本自证，也防 __common__ 行为回退时静默复发。
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/comm_tower.glb'
    export_glb(out_path)
