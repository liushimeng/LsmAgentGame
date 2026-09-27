#!/usr/bin/env python3
"""
build_city_hall — 市政厅（CityHall） Blender headless 导出脚本（19-Blender3D模型集成）。

尺寸对齐 ClientWeb/src/components/virtualCity/civic/CityHall.tsx 的 cityScale.u() 米制。
组件原形态（GLB 缺席时的 fallback）：
  - 3 层主楼 box + 屋顶 + 4 柱门廊 + 钟楼 + 4 面金色钟 + 3 台阶 + 旗杆 + 旗
  - 全城坐标 (-9, 0, -1.7)

坐标与尺度规约（2026-09-27 批次 19 GLB 轴向/尺度回溯修正，全目录统一）：
  - **Blender 原生 Z-up**：x = 面宽，y = 进深（**-Y = 临街正向**），z = 高度（地面 z = 0）。
    导出后 glTF X = 面宽、glTF Y = 楼高（minY = 0）、glTF Z = 进深（+Z = 临街正向）。
  - 世界单位：1 单位 = 10 m。旧版把高度写进位置向量第 1 位（作者自定 Y-up），
    导出后整栋楼侧躺（高度 19.9 m 躺在水平轴上），本次修正。

目标包围盒（世界单位）：X 1.440（面宽 14.4 m）× Z 0.840（进深 8.4 m）× Y 2.000（高 20 m），
地面中心 minY = 0。
"""
import bpy
import math
import sys
import os
# Blender --background 模式不把脚本所在目录加到 sys.path,需手动补
_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (
    reset_scene, set_unit_meters,
    make_box, make_cylinder, make_cone, apply_pbr, join_objects, export_glb,
)

# ── 颜色（与 fallback 组件一致 / 微调）───────────────────────────────────
STONE = '#a8a4a0'
STONE_DARK = '#7a7a76'
ROOF = '#3a3f4a'
CLOCK_GOLD = '#d4a017'
WINDOW_GLASS = '#1f3a52'
FLAG_RED = '#c8302c'

# ── 尺寸（世界单位；由目标包围盒反推）────────────────────────────────────
W, D, H = 1.440, 0.840, 2.000        # 面宽 / 进深 / 总高（含钟楼尖顶）
MAIN_W, MAIN_D, MAIN_H = 1.400, 0.800, 0.900
ROOF_D, ROOF_H = 0.040, 0.040
TOWER_W, TOWER_H = 0.320, 0.700      # 钟楼（0.94..1.64）
SPIRE_R, SPIRE_H = 0.200, 0.360      # 尖顶（1.64..2.00）
STREET_Y = -D / 2                    # 临街面（-Y）


def build_city_hall() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 1. 主楼（地面起 0..0.90）
    main = make_box('MainBuilding', (MAIN_W, MAIN_D, MAIN_H), (0, 0, MAIN_H / 2))
    apply_pbr(main, STONE, rough=0.9, metal=0.0)

    # 2. 屋顶檐口（同时定义目标面宽 W 与进深 D）
    roof = make_box('Roof', (W, D, ROOF_H), (0, 0, MAIN_H + ROOF_H / 2))
    apply_pbr(roof, ROOF, rough=0.7, metal=0.1)

    # 3. 门廊 4 柱（临街面内侧一列，y 收在檐口投影内）
    porch_cols = []
    for dx in (-0.45, -0.15, 0.15, 0.45):
        c = make_cylinder(f'PorchCol_{dx:.2f}', 0.03, 0.03, MAIN_H, 10,
                          (dx, STREET_Y + 0.08, MAIN_H / 2))
        apply_pbr(c, STONE_DARK, rough=0.9, metal=0.0)
        porch_cols.append(c)

    # 4. 门廊雨棚（临街侧檐下）
    canopy = make_box('PorchCanopy', (1.200, 0.120, 0.040),
                      (0, STREET_Y + 0.06, MAIN_H + ROOF_H + 0.020))
    apply_pbr(canopy, STONE_DARK, rough=0.9, metal=0.0)

    # 5. 三级台阶（临街面前缘，收在进深包线内）
    steps = []
    for i in range(3):
        step_w = 0.80 - i * 0.06
        step_d = 0.050
        step_h = 0.030
        s = make_box(f'Step_{i}', (step_w, step_d, step_h),
                     (0, STREET_Y + step_d / 2 + i * step_d, step_h / 2 + i * step_h))
        apply_pbr(s, STONE_DARK, rough=0.9, metal=0.0)
        steps.append(s)

    # 6. 钟楼（0.94..1.64）
    tower = make_box('ClockTower', (TOWER_W, TOWER_W, TOWER_H),
                     (0, 0, MAIN_H + ROOF_H + TOWER_H / 2))
    apply_pbr(tower, STONE_DARK, rough=0.9, metal=0.0)

    # 7. 四面钟（金字圆面，贴在钟楼四面中央）
    clock_faces = []
    clock_z = MAIN_H + ROOF_H + TOWER_H / 2
    for rot_z, off in [
        (0, (0, -(TOWER_W / 2 + 0.0025), clock_z)),
        (math.pi / 2, (TOWER_W / 2 + 0.0025, 0, clock_z)),
        (math.pi, (0, TOWER_W / 2 + 0.0025, clock_z)),
        (-math.pi / 2, (-(TOWER_W / 2 + 0.0025), 0, clock_z)),
    ]:
        face = make_box('ClockFace', (0.060, 0.005, 0.060), off, rot=(0, 0, rot_z))
        apply_pbr(face, CLOCK_GOLD, rough=0.4, metal=0.6,
                  emissive=CLOCK_GOLD, emissive_intensity=0.5)
        clock_faces.append(face)

    # 8. 钟楼尖顶（顶面 = 总高 H）
    spire = make_cone('Spire', r=SPIRE_R, h=SPIRE_H, segs=8,
                      pos=(0, 0, MAIN_H + ROOF_H + TOWER_H + SPIRE_H / 2))
    apply_pbr(spire, ROOF, rough=0.7, metal=0.1)

    # 9. 旗杆 + 旗（主楼侧前方，收在包线内）
    flag_pole = make_cylinder('FlagPole', 0.006, 0.006, 1.200, 8,
                              (-0.660, STREET_Y + 0.06, 0.600))
    apply_pbr(flag_pole, '#222222', rough=0.7, metal=0.5)

    flag = make_box('Flag', (0.140, 0.006, 0.090),
                    (-0.600, STREET_Y + 0.06, 1.100))
    apply_pbr(flag, FLAG_RED, rough=0.7, metal=0.0)

    all_objs = [main, roof, canopy, tower, spire, flag_pole, flag] + porch_cols + steps + clock_faces
    return join_objects(all_objs, 'CityHall')


if __name__ == '__main__':
    obj = build_city_hall()
    # 烘焙 join 残留 object transform ⇒ 导出节点 identity（<Model> 零旋转零 scale 直挂）
    # 注：__common__.export_glb 自 2026-09-27 起亦统一 bake_transforms()（同一目的）；
    # 此处显式保留，既让脚本自证，也防 __common__ 行为回退时静默复发。
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    # 命令: blender --background --python build_city_hall.py -- /abs/path/to/city_hall.glb
    if '--' in sys.argv:
        out_path = sys.argv[sys.argv.index('--') + 1]
    else:
        out_path = '/tmp/city_hall.glb'
    export_glb(out_path)
