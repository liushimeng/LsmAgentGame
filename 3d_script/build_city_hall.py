#!/usr/bin/env python3
"""
build_city_hall — 市政厅（CityHall）批次 90「地标真实感重制」。

── 重制理由：批次 19/29 的首版是「玩具积木」────────────────────────────
  四面光板无门窗、无山花、无线脚、钟面无指针（规范 §6 已记录未返工缺陷：
  钟楼与屋顶材质完全同参）。本批按新古典市政厅真实构造全量重制，并首次
  挂贴图烘焙通路（3d_script/bake_texture.py）—— Stone/StoneDark/Roof 三桶
  烘 256² baseColor（经年污渍），全城首批带贴图 GLB。

── §27.0-1 真实形态调研（新古典主义小型市政厅，如英式 town hall）────────
  | 部件      | 真实规格                              | 本件取值                |
  |-----------|---------------------------------------|-------------------------|
  | 体量      | 3 层主楼 + 中央钟塔，对称五开间        | 14.4×8.4×10.8 m 主楼    |
  | 勒脚      | 糙石/重琢石基座 1~1.5 m               | 14.2×8.2×1.2 m          |
  | 门廊      | 4 柱塔司干柱式 + 檐部 + 三角山花       | ⌀0.44 柱×4 + 山花 h1.5  |
  | 窗        | 木框窗 + 石窗台/过梁，五开间×3 层      | 41 樘（框+玻璃+窗台）   |
  | 线脚      | 楼层束腰线 + 檐部（额枋/齿饰/冠板）    | 2 道 + 3 段檐部 + 托架  |
  | 钟塔      | 百叶钟室 + 四面钟（指针/刻度）+ 方锥顶  | 钟 ⌀1.7 m + 角尖塔×4    |
  | 塔顶      | 方锥顶 + 风向杆 + 旗杆                | 总高 20.0 m（表值）     |

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Blender 原生 Z-up、**建模即米制**（导出前统一 ×0.1 转世界单位）。
  · -Y = 临街正向（three.js +Z）。尺寸唯一事实来源 REAL_DIMS_M.cityHall
    = 14.4 × 8.4 × 20.0（x/z 由檐口冠板定义、y 总高由塔顶风向杆定义），本批不变。
  · 节点 identity；minY = 0；X/Z 居中（center_content_xz）。

── 材质槽（9 ⇒ 9 primitive/DC；材质名与前端 litMaterials 键同源）─────────
  CityHall_Stone(烘) / _StoneDark(烘) / _Roof(烘) / _Glass / _Clock(夜 0.6) /
  _Door / _Bronze / _Lantern(夜 1.6) / _Flag

用法：
  blender --background --python build_city_hall.py -- \
    ClientWeb/src/assets/models/civic/city_hall.glb
"""
import math
import os
import sys

import bpy

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (  # noqa: E402
    reset_scene, set_unit_meters,
    make_box_wdh as box, make_cylinder as cyl, make_prism, make_sphere,
    make_material, assign_material, join_objects, export_glb,
    center_content_xz, shared_weathered, CITY_PALETTE,
)
from bake_texture import smart_uv, bake_bucket, wire_baked  # noqa: E402

# ── 尺寸（米）───────────────────────────────────────────────────────────
W, D, H = 14.4, 8.4, 20.0           # AABB 契约（REAL_DIMS_M.cityHall）
BODY_W, BODY_D = 14.0, 8.0          # 主楼墙身（勒脚上）
BASE_W, BASE_D, BASE_H = 14.2, 8.2, 1.2
BODY_TOP = 10.8                     # 主楼檐部底
CORONA_TOP = 11.35                  # 檐口冠板顶（定义 X/Z 包线）
F1, F2, F3 = 2.6, 5.8, 9.0          # 三层窗带中心高
WIN_W, WIN_H = 1.05, 1.7
BAYS_X = (-5.2, -2.6, 0.0, 2.6, 5.2)
SIDE_WIN_Y = (-1.8, 1.8)
STREET_Y = -BODY_D / 2              # -4.0 临街墙皮
# 门廊几何常量在 §⑦ 分节内联（外凸式，收在 ±4.2 包线内）
# 钟塔
TW = 3.5                            # 塔身
TOWER_SHAFT = (11.6, 14.2)
CLOCK_STAGE = (14.42, 17.0)
CLOCK_Z = 15.7
CLOCK_R = 0.85
SPIRE_BASE_Z, SPIRE_TOP = 17.2, 19.0
FINIAL_TOP = 20.0                   # 总高（风向杆）
POLE_X, POLE_Y = -5.3, 2.6          # 旗杆（塔顶风向杆在 19.75..20.0）
POLE_TOP = 19.55


def make_rect_pyramid(name, w, d, h, pos):
    """矩形底四棱锥（庑殿/方锥顶），from_pydata 直建（+底面省略，不可见）。"""
    mesh = bpy.data.meshes.new(name + '_Mesh')
    hw, hd = w / 2.0, d / 2.0
    x0, y0, z0 = pos
    verts = [(-hw, -hd, 0), (hw, -hd, 0), (hw, hd, 0), (-hw, hd, 0), (0, 0, h)]
    faces = [(0, 1, 4), (1, 2, 4), (2, 3, 4), (3, 0, 4)]
    mesh.from_pydata(verts, [], faces)
    mesh.update()
    obj = bpy.data.objects.new(name, mesh)
    bpy.context.collection.objects.link(obj)
    obj.location = (x0, y0, z0)
    return obj


def main() -> None:
    reset_scene()
    set_unit_meters(1.0)

    stone, stone_dark, roof = [], [], []
    glass, clock, door = [], [], []
    bronze, lantern, flag = [], [], []

    # ── ① 勒脚 + 主楼墙身 ───────────────────────────────────────────────
    stone_dark.append(box('RusticatedBase', BASE_W, BASE_D, BASE_H, (0, 0, BASE_H / 2)))
    stone.append(box('MainBody', BODY_W, BODY_D, BODY_TOP - BASE_H,
                     (0, 0, BASE_H + (BODY_TOP - BASE_H) / 2)))
    # 楼层束腰线脚 ×2（微出墙皮 0.03）
    for zc in (4.2, 7.4):
        stone_dark.append(box(f'String_{zc:.0f}', BODY_W + 0.06, BODY_D + 0.06, 0.18,
                              (0, 0, zc)))

    # ── ② 檐部（额枋 + 冠板 + 冠顶线 = X/Z 包线；齿饰托架沿正背两面）────
    stone_dark.append(box('Frieze', BODY_W + 0.1, BODY_D + 0.1, 0.35, (0, 0, 10.975)))
    stone_dark.append(box('Corona', W - 0.1, D - 0.1, 0.12, (0, 0, 11.21)))
    stone_dark.append(box('Cyma', W, D, 0.08, (0, 0, 11.31)))       # ← AABB X/Z
    for i in range(17):
        bx = -6.6 + i * 0.82
        for sy in (1, -1):
            stone_dark.append(box(f'Mutule_{i}_{sy}', 0.20, 0.26, 0.30,
                                  (bx, sy * (D / 2 - 0.28), 10.72)))

    # ── ③ 庑殿式短屋顶（方锥，石板瓦）──────────────────────────────────
    roof.append(make_rect_pyramid('HipRoof', 13.6, 7.6, 1.5, (0, 0, CORONA_TOP)))

    # ── ④ 烟囱 ×2（屋顶装饰性烟囱 + 顶管）───────────────────────────────
    for cx, cy in ((-4.6, -2.0), (4.6, -2.0)):
        stone_dark.append(box(f'Chimney_{cx:.0f}', 0.95, 0.95, 1.7, (cx, cy, CORONA_TOP + 0.85)))
        stone_dark.append(cyl(f'ChimPot_{cx:.0f}', 0.16, 0.16, 0.42, 8,
                              (cx, cy, CORONA_TOP + 1.9)))

    # ── ⑤ 中央前凸 pavilion（无布尔管线下一切「凹入」必须「外凸」表达）──
    #    正面中央 6.6 m 宽 × 0.15 m 前凸全身高的横竖体，门廊/大门/门廊层窗挂其上。
    PAV_W, PAV_FACE = 6.6, -4.08
    stone.append(box('CentralPavilion', PAV_W, 0.08, BODY_TOP - BASE_H,
                     (0, (STREET_Y + PAV_FACE) / 2, BASE_H + (BODY_TOP - BASE_H) / 2)))

    # ── ⑥ 窗（框 + 玻璃 + 窗台；五开间×3 层正背 + 侧面 2×3×2）────────────
    #    axis='y'：face 是墙皮 Y 坐标、out=±1 外法向；axis='x' 同理（山墙面）。
    #    pavilion 区（|x| ≤ 3.3）的正面窗挂 pavilion 面前，其余挂主墙面前。
    def add_window(axis, lateral, face, out, zc, w=WIN_W, h=WIN_H):
        n = f'Win_{axis}_{lateral:+.1f}_{zc:.0f}{w:.1f}'
        fi = face - out * 0.01      # 框心内收 1 cm ⇒ 前脸最多与包线平齐
        gi = face - out * 0.015
        si = face - out * 0.05
        if axis == 'y':
            stone_dark.append(box(n + '_F', w + 0.22, 0.12, h + 0.22, (lateral, fi, zc)))
            glass.append(box(n + '_G', w, 0.05, h, (lateral, gi, zc)))
            stone_dark.append(box(n + '_S', w + 0.30, 0.20, 0.13,
                                  (lateral, si, zc - h / 2 - 0.10)))
        else:
            stone_dark.append(box(n + '_F', 0.12, w + 0.22, h + 0.22, (fi, lateral, zc)))
            glass.append(box(n + '_G', 0.05, w, h, (gi, lateral, zc)))
            stone_dark.append(box(n + '_S', 0.20, w + 0.30, 0.13,
                                  (si, lateral, zc - h / 2 - 0.10)))

    for zc in (F1, F2, F3):                                   # 正面
        for x in BAYS_X:
            if abs(x) < PAV_W / 2:                            # pavilion 区
                if zc == F1:
                    if abs(x) < 1.3:
                        continue                              # 中央门位
                    add_window('y', x, PAV_FACE, -1, zc, w=0.75, h=1.5)   # 门廊层窄窗
                else:
                    add_window('y', x, PAV_FACE, -1, zc)
            else:
                add_window('y', x, STREET_Y, -1, zc)
    for zc in (F1, F2, F3):                                   # 背面整排
        for x in BAYS_X:
            add_window('y', x, -STREET_Y, 1, zc)
    for zc in (F1, F2, F3):                                   # 两侧
        for y in SIDE_WIN_Y:
            add_window('x', y, BODY_W / 2, 1, zc)
            add_window('x', y, -BODY_W / 2, -1, zc)

    # ── ⑦ 门廊（外凸式，全部收在 ±4.2 包线内）：阶梯门台 + 4 柱 + 檐部 + 山花
    STOOP_W, STOOP_TOP = 6.4, 0.54
    for i, (run, zb) in enumerate(((0.57, 0.0), (0.45, 0.18), (0.33, 0.36))):
        stone_dark.append(box(f'Stair_{i}', STOOP_W, run, 0.18,
                              (0, -4.19 + run / 2, zb + 0.09)))
    PCOL_X, PCOL_R, PCOL_Y = (-2.55, -0.85, 0.85, 2.55), 0.17, -4.03
    for x in PCOL_X:
        stone_dark.append(box(f'ColPlinth_{x:+.2f}', 0.50, 0.50, 0.22, (x, PCOL_Y, 0.65)))
        stone_dark.append(cyl(f'ColBase_{x:+.2f}', 0.21, 0.21, 0.12, 14, (x, PCOL_Y, 0.82)))
        stone.append(cyl(f'ColShaft_{x:+.2f}', 0.165, 0.19, 5.42, 16, (x, PCOL_Y, 3.59)))
        stone_dark.append(cyl(f'ColCap_{x:+.2f}', 0.21, 0.165, 0.20, 14, (x, PCOL_Y, 6.40)))
        stone_dark.append(box(f'ColAbacus_{x:+.2f}', 0.52, 0.52, 0.15, (x, PCOL_Y, 6.575)))
    ENT_Y, ENT_D = -4.03, 0.32                                # 檐部深度方向中心
    stone_dark.append(box('Architrave', 6.9, ENT_D, 0.5, (0, ENT_Y, 6.90)))
    stone_dark.append(box('PorticoFrieze', 7.1, ENT_D, 0.45, (0, ENT_Y, 7.375)))
    # 山花：檐下冠板 + 两坡斜檐 + 凹入山墙面（prism，apex 8.90）
    stone_dark.append(box('PedCornice', 7.2, 0.34, 0.22, (0, ENT_Y, 7.71)))
    PED_B, PED_HH = 7.82, 1.26
    stone_dark.append(make_prism('Tympanum', 6.4, PED_HH, 0.30, (0, ENT_Y, PED_B)))
    slope_len = math.hypot(3.2, PED_HH) + 0.12
    for s in (1, -1):
        stone_dark.append(box(f'Raking_{s}', slope_len, 0.30, 0.26,
                              (s * 1.6, ENT_Y, PED_B + PED_HH / 2 + 0.05),
                              rot=(0, s * math.atan2(PED_HH, 3.2), 0)))

    # ── ⑧ 大门（双扇 + 门框 + 气窗，挂 pavilion 面）+ 门侧壁灯 ×2 ────────
    stone_dark.append(box('DoorFrame', 2.3, 0.07, 3.1, (0, PAV_FACE + 0.02, 2.09)))
    door.append(box('DoorLeafL', 0.95, 0.06, 2.7, (-0.50, PAV_FACE + 0.01, 1.89)))
    door.append(box('DoorLeafR', 0.95, 0.06, 2.7, (0.50, PAV_FACE + 0.01, 1.89)))
    glass.append(box('Fanlight', 1.9, 0.05, 0.4, (0, PAV_FACE + 0.015, 3.44)))
    for x in (-1.62, 1.62):
        bronze.append(box(f'LanternBox_{x:+.2f}', 0.24, 0.14, 0.52, (x, PAV_FACE + 0.02, 3.05)))
        lantern.append(box(f'LanternGlow_{x:+.2f}', 0.17, 0.09, 0.40, (x, PAV_FACE + 0.01, 3.03)))
        bronze.append(make_rect_pyramid(f'LanternCap_{x:+.2f}', 0.32, 0.19, 0.18,
                                        (x, PAV_FACE + 0.02, 3.31)))

    # ── ⑨ 钟塔：基座 → 塔身 → 钟室（四面钟）→ 檐口 → 方锥顶 + 角尖塔 ──
    stone_dark.append(box('TowerPlinth', 3.95, 3.95, 0.28, (0, 0, CORONA_TOP + 0.14)))
    stone.append(box('TowerShaft', TW, TW, TOWER_SHAFT[1] - TOWER_SHAFT[0],
                     (0, 0, (TOWER_SHAFT[0] + TOWER_SHAFT[1]) / 2)))
    stone_dark.append(box('TowerBand', 3.9, 3.9, 0.22, (0, 0, 14.31)))
    stone.append(box('ClockStage', 3.6, 3.6, CLOCK_STAGE[1] - CLOCK_STAGE[0],
                     (0, 0, (CLOCK_STAGE[0] + CLOCK_STAGE[1]) / 2)))
    stone_dark.append(box('ClockCornice', 3.95, 3.95, 0.20, (0, 0, 17.10)))
    # 四面钟：青铜环 + 乳白盘 + 指针 + 刻度（盘面法向 = 外墙法向）
    def add_clock(nx, ny):
        """(nx, ny) = 外墙法向单位向量（±X 或 ±Y 面）。盘心在 CLOCK_Z。"""
        cx, cy = nx * (TW / 2 + 0.06), ny * (TW / 2 + 0.06)
        axis_rot = (math.pi / 2, 0, 0) if ny else (0, math.pi / 2, 0)
        n = f'Clock_{nx:+.0f}{ny:+.0f}'
        bronze.append(cyl(n + '_Bezel', CLOCK_R + 0.10, CLOCK_R + 0.10, 0.10, 20,
                          (cx, cy, CLOCK_Z), rot=axis_rot))
        clock.append(cyl(n + '_Face', CLOCK_R, CLOCK_R, 0.06, 20,
                         (cx + nx * 0.03, cy + ny * 0.03, CLOCK_Z), rot=axis_rot))
        # 12/3/6/9 刻度块（青铜，贴盘面；方形块无需随角度旋转）
        for a in (0.0, math.pi / 2, math.pi, -math.pi / 2):
            mr = CLOCK_R - 0.16
            mx = cx + nx * 0.065 + (math.sin(a) * mr if ny else 0)
            my = cy + ny * 0.065 + (math.sin(a) * mr if nx else 0)
            mz = CLOCK_Z + math.cos(a) * mr
            bronze.append(box(n + f'_Mk{int(math.degrees(a))}',
                              (0.03 if nx else 0.15), (0.03 if ny else 0.15), 0.15,
                              (mx, my, mz)))
        # 时针(10 点方向 300°) + 分针(2 点 8 分方向 42°)—— 在盘面平面内旋转：
        # Y 法向面：绕 Y 轴转 (a−π/2) 使 +X → (sin a, 0, cos a)；
        # X 法向面：绕 X 轴转 (π/2−a) 使 +Y → (0, sin a, cos a)。
        for ln, a in ((0.40, math.radians(300)), (0.60, math.radians(42))):
            d = ln / 2 - 0.06
            hx = cx + nx * 0.075 + (math.sin(a) * d if ny else 0)
            hy = cy + ny * 0.075 + (math.sin(a) * d if nx else 0)
            hz = CLOCK_Z + math.cos(a) * d
            if ny:
                bronze.append(box(n + f'_H{int(math.degrees(a))}', ln, 0.05, 0.05,
                                  (hx, hy, hz), rot=(0, a - math.pi / 2, 0)))
            else:
                bronze.append(box(n + f'_H{int(math.degrees(a))}', 0.05, ln, 0.05,
                                  (hx, hy, hz), rot=(math.pi / 2 - a, 0, 0)))

    for nx, ny in ((0, -1), (0, 1), (1, 0), (-1, 0)):
        add_clock(nx, ny)
    # 角尖塔 ×4 + 方锥顶 + 风向杆（总高 20.0）
    for sx in (1, -1):
        for sy in (1, -1):
            stone_dark.append(make_rect_pyramid(
                f'Pinnacle_{sx}{sy}', 0.55, 0.55, 1.15, (sx * 1.7, sy * 1.7, SPIRE_BASE_Z)))
    roof.append(make_rect_pyramid('Spire', 3.2, 3.2, SPIRE_TOP - SPIRE_BASE_Z,
                                  (0, 0, SPIRE_BASE_Z)))
    bronze.append(cyl('FinialRod', 0.05, 0.05, 0.75, 8, (0, 0, SPIRE_TOP + 0.375)))
    bronze.append(make_sphere('FinialOrb', 0.14, 12, (0, 0, SPIRE_TOP + 0.62)))
    bronze.append(box('WeatherVane', 0.04, 0.55, 0.20, (0, 0, SPIRE_TOP + 0.89)))

    # ── ⑩ 旗杆 + 旗（塔侧屋面甲板，杆顶 19.55 < 风向杆 20.0）────────────
    bronze.append(cyl('FlagPole', 0.05, 0.05, POLE_TOP - CORONA_TOP, 8,
                      (POLE_X, POLE_Y, CORONA_TOP + (POLE_TOP - CORONA_TOP) / 2)))
    bronze.append(make_sphere('FlagBall', 0.09, 10, (POLE_X, POLE_Y, POLE_TOP)))
    flag.append(box('Flag', 1.5, 0.03, 0.95, (POLE_X + 0.78, POLE_Y, POLE_TOP - 0.62)))

    # ── 材质：3 桶烘焙（Stone / StoneDark / Roof）+ 6 桶平色 ────────────
    bake_buckets = [
        ('CityHall_Stone', stone, CITY_PALETTE['stone_cream'], 0.82, 0.0, 0.30, 2.5),
        ('CityHall_StoneDark', stone_dark, CITY_PALETTE['stone_cream_dark'], 0.85, 0.0, 0.42, 4.0),
        ('CityHall_Roof', roof, CITY_PALETTE['roof_tile_slate'], 0.70, 0.05, 0.24, 5.0),
    ]
    flat_buckets = [
        ('CityHall_Glass', glass, CITY_PALETTE['glass_dark'], 0.15, 0.0, None, None),
        ('CityHall_Clock', clock, CITY_PALETTE['clock_face'], 0.40, 0.0,
         CITY_PALETTE['clock_face'], 0.45),
        ('CityHall_Door', door, CITY_PALETTE['door_oak'], 0.72, 0.0, None, None),
        ('CityHall_Bronze', bronze, CITY_PALETTE['bronze'], 0.45, 0.85, None, None),
        ('CityHall_Lantern', lantern, CITY_PALETTE['sign_lightbox'], 0.35, 0.0,
         CITY_PALETTE['sign_lightbox'], 0.5),
        ('CityHall_Flag', flag, CITY_PALETTE['flag_red'], 0.80, 0.0, None, None),
    ]

    joined = []
    for name, objs, col, rough, metal, emis, ei in flat_buckets:
        if not objs:
            raise RuntimeError(f'{name}: 空桶（接线断了）')
        mat = make_material(name + '_Mat', col, rough, metal,
                            emissive=emis, emissive_intensity=ei or 0.0)
        j = join_objects(objs, name)
        assign_material(j, mat)
        joined.append(j)

    for name, objs, col, rough, metal, wear, scale in bake_buckets:
        if not objs:
            raise RuntimeError(f'{name}: 空桶（接线断了）')
        from __common__ import shared_weathered
        shared_weathered(objs, col, rough, metal, name=name + '_Mat',
                         wear=wear, scale=scale)
        mat = objs[0].data.materials[0]
        smart_uv(objs)
        img = bake_bucket(mat, objs, size=256)
        wire_baked(mat, img)
        j = join_objects(objs, name)
        assign_material(j, mat)
        joined.append(j)

    # ── 米 → 世界单位（×0.1；先 flatten 再统一缩放，见 bake_transforms）──
    for obj in joined:
        bpy.ops.object.select_all(action='DESELECT')
        obj.select_set(True)
        bpy.context.view_layer.objects.active = obj
        bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
        obj.scale = (0.1, 0.1, 0.1)
    for obj in joined:
        min_z = min(bb[2] for bb in obj.bound_box)
        if min_z < 0:
            obj.location.z = -0.1 * min_z
    center_content_xz(joined)

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv
                else '/tmp/city_hall.glb')
    export_glb(out_path, image_format='JPEG')
    size = os.path.getsize(out_path)
    print(f'✅ city_hall.glb exported: {out_path} ({size} bytes)', flush=True)
    assert size <= 500 * 1024, f'超 500 KB 上限（§27.3-1）: {size}'


if __name__ == '__main__':
    main()
