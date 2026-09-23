#!/usr/bin/env python3
"""
build_city_hall — 市政厅（CityHall） Blender headless 导出脚本（19-Blender3D模型集成）。

尺寸对齐 ClientWeb/src/components/wealth/civic/CityHall.tsx 的 cityScale.u() 米制。
组件原形态（替换前的 fallback）：
  - 3 层主楼 box + 屋顶 + 4 柱门廊 + 钟楼 + 4 面金色钟 + 3 台阶 + 旗杆 + 旗
  - 全城坐标 (-9, 0, -1.7)

GLB 内 pivot 落 (0, 0, 0) 在地面；调用方 <Model position={[-9, 0, -1.7]}> 即贴位。
"""
import bpy
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

# ── 颜色（与原组件一致 / 微调）─────────────────────────────────────────
STONE = '#a8a4a0'
STONE_DARK = '#7a7a76'
ROOF = '#3a3f4a'
CLOCK_GOLD = '#d4a017'
WINDOW_GLASS = '#1f3a52'
FLAG_RED = '#c8302c'

# ── 主楼 14×9×8 米 → Blender (1.4, 0.9, 0.8) ─────────────────────────────
MAIN_W, MAIN_H, MAIN_D = 1.4, 0.9, 0.8
ROOF_H = 0.04


def build_city_hall() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 1. 主楼（地面起 0..0.9）
    main = make_box('MainBuilding', (MAIN_W, MAIN_H, MAIN_D), (0, MAIN_H / 2, 0))
    apply_pbr(main, STONE, rough=0.9, metal=0.0)

    # 2. 屋顶（y = 0.9..0.94）
    roof = make_box('Roof', (MAIN_W + 0.04, ROOF_H, MAIN_D + 0.04),
                    (0, MAIN_H + ROOF_H / 2, 0))
    apply_pbr(roof, ROOF, rough=0.7, metal=0.1)

    # 3. 门廊 4 柱（±0.45, ±0.15；z=+0.5 朝街；y=0..0.9）
    porch_cols = []
    for dx in (-0.45, -0.15, 0.15, 0.45):
        c = make_cylinder(f'PorchCol_{dx:.2f}', 0.03, 0.03, 0.9, 8,
                          (dx, MAIN_H / 2, MAIN_D / 2 - 0.05))
        apply_pbr(c, STONE_DARK, rough=0.9, metal=0.0)
        porch_cols.append(c)

    # 4. 门廊雨棚（y=0.93）
    canopy = make_box('PorchCanopy', (1.2, 0.04, 0.2),
                      (0, MAIN_H + ROOF_H + 0.02, MAIN_D / 2 - 0.05))
    apply_pbr(canopy, STONE_DARK, rough=0.9, metal=0.0)

    # 5. 三级台阶（z = +0.7..+0.85）
    steps = []
    for i in range(3):
        step_w = 0.8 - i * 0.06
        step_d = 0.06
        step_h = 0.03
        s = make_box(f'Step_{i}', (step_w, step_h, step_d),
                     (0, step_h / 2 + i * step_h, MAIN_D / 2 + 0.03 + i * (step_d + 0.02)))
        apply_pbr(s, STONE_DARK, rough=0.9, metal=0.0)
        steps.append(s)

    # 6. 钟楼（y = 0.94..1.6；居中 0.32×0.7×0.32）
    tower = make_box('ClockTower', (0.32, 0.7, 0.32),
                     (0, MAIN_H + ROOF_H + 0.35, 0))
    apply_pbr(tower, STONE_DARK, rough=0.9, metal=0.0)

    # 7. 四面钟（每面贴一个 0.06×0.06 薄板，金色）
    clock_faces = []
    clock_y = MAIN_H + ROOF_H + 0.35
    for rot_z, off in [
        (0, (0, clock_y, 0.162)),
        (1.5708, (0.162, clock_y, 0)),
        (3.1416, (0, clock_y, -0.162)),
        (-1.5708, (-0.162, clock_y, 0)),
    ]:
        face = make_box('ClockFace', (0.06, 0.06, 0.005), off, rot=(0, 0, rot_z))
        apply_pbr(face, CLOCK_GOLD, rough=0.4, metal=0.6,
                  emissive=CLOCK_GOLD, emissive_intensity=0.5)
        clock_faces.append(face)

    # 8. 钟楼尖顶（圆锥）
    spire = make_cone('Spire', r=0.2, h=0.3, segs=8,
                      pos=(0, MAIN_H + ROOF_H + 0.7 + 0.15, 0))
    apply_pbr(spire, ROOF, rough=0.7, metal=0.1)

    # 9. 旗杆（在门廊一侧，x=-0.8）
    flag_pole = make_cylinder('FlagPole', 0.006, 0.006, 1.2, 6,
                              (-0.8, 0.6, MAIN_D / 2 + 0.04))
    apply_pbr(flag_pole, '#222222', rough=0.7, metal=0.5)

    flag = make_box('Flag', (0.14, 0.09, 0.002),
                    (-0.74, 1.1, MAIN_D / 2 + 0.04))
    apply_pbr(flag, FLAG_RED, rough=0.7, metal=0.0)

    # 把所有 group 一起 join（保持 mesh 数稳定，~12 个最终 mesh；不要 join 让它们
    # 各自独立便于运行时选材质替换）
    all_objs = [main, roof, canopy, tower, spire, flag_pole, flag] + porch_cols + steps + clock_faces
    return join_objects(all_objs, 'CityHall')


if __name__ == '__main__':
    build_city_hall()
    # 命令: blender --background --python build_city_hall.py -- /abs/path/to/city_hall.glb
    if '--' in sys.argv:
        out_path = sys.argv[sys.argv.index('--') + 1]
    else:
        out_path = '/tmp/city_hall.glb'
    export_glb(out_path)
