#!/usr/bin/env python3
"""
build_comm_tower — 通信塔（CommTower）批次 90「地标真实感重制」。

── 重制理由：批次 19/29 首版「结构上不成立」────────────────────────────
  立柱竖直无收分、**无交叉斜撑**（格构塔的主受力构件缺失，规范 §6 判读记录：
  「只有横撑的塔在现实中会倒塌」）、微波天线是平板、无天线阵列、无平台。
  本批按自立式四柱格构塔真实构造全量重制，并挂贴图烘焙通路
  （Galv / GalvDark 两桶烘 256² baseColor —— 镀锌钢经年风化）。

── §27.0-1 真实形态调研（自立式格构通信塔，GB 50135 / YD/T 5231 一族）────
  | 部件      | 真实规格                                | 本件取值                  |
  |-----------|-----------------------------------------|---------------------------|
  | 收分      | 自立塔立柱直线/折线收窄（风载+经济性）    | 底半宽 4.2 → 顶半宽 0.75  |
  | 腹杆      | 每节横杆 + X 交叉斜撑（抗剪主构件）      | 8 节 × 4 面 × X 撑        |
  | 节高      | 底部节高大、顶部收小                     | 2.4 m → 2.1 m 渐变        |
  | 平台      | 天线安装平台带 1.05 m 栏杆              | 2 层（z 12.5 / 16.55）    |
  | 微波天线  | 抛物面 ⌀1.5~3 m + 背架 + 馈源           | 3 面 ⌀3.0 m（±X / +Y）   |
  | 天线阵列  | 玻璃钢护罩柱状天线，三扇区挂抱杆         | 6 组（2 高度 × 3 扇区）   |
  | 防雷      | 塔顶避雷针 + 红色障碍灯（顶/中）          | 针 1.5 m；顶灯=总高 20.5  |
  | 爬梯      | 主材侧面直爬梯（>5 m 规范要求护笼）      | 梯 + 简化护圈（环盘）     |
  | 基础      | 独立混凝土垫块（外拉/重力式）            | 4 块 1.6×1.6×0.5（±5.0） |

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Z-up、建模即米制（导出前统一 ×0.1）。
  · 尺寸唯一事实来源 REAL_DIMS_M.commTower = 10.0 × 10.0 × 20.5
    （x/z 由垫块外缘 ±5.0 定义、y 由塔顶障碍灯定义），本批不变。
  · 节点 identity；minY = 0；X/Z 居中。

── 材质槽（7 ⇒ 7 primitive/DC；Galv/GalvDark 烘焙，Warn 夜间 lit）────────
  CommTower_Galv(烘) / _GalvDark(烘) / _Concrete / _Dish / _Radome /
  _Cabinet / _Warn

用法：
  blender --background --python build_comm_tower.py -- \
    ClientWeb/src/assets/models/civic/comm_tower.glb
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
    make_box_wdh as box, make_cylinder as cyl, make_taper, make_sphere,
    make_strut, make_material, assign_material, join_objects, export_glb,
    center_content_xz, shared_weathered, CITY_PALETTE,
)
from bake_texture import smart_uv, bake_bucket, wire_baked  # noqa: E402

# ── 塔身几何（米）───────────────────────────────────────────────────────
H_CONTRACT = 20.5                      # 总高（塔顶障碍灯顶面）
LEVELS = (0.5, 2.9, 5.3, 7.7, 10.1, 12.5, 14.9, 17.3, 19.4)   # 节点标高
S_BASE, S_TOP = 4.2, 0.75              # 立柱半宽（底/顶）
LEG_W = 0.20                           # 主角钢等效方钢
BRACE_W = 0.085                        # 斜撑
HORIZ_W = 0.11                         # 横杆


def spread(z: float) -> float:
    t = (z - LEVELS[0]) / (LEVELS[-1] - LEVELS[0])
    return S_BASE + (S_TOP - S_BASE) * t


CORNERS = ((-1, -1), (1, -1), (1, 1), (-1, 1))   # 逆时针角点（X 号 / Y 号）


def main() -> None:
    reset_scene()
    set_unit_meters(1.0)

    galv, galv_dark, concrete = [], [], []
    dish, radome, cabinet, warn = [], [], [], []

    # ── ① 基础垫块 ×4（外缘 ±5.0 = x/z 包线）───────────────────────────
    for sx, sy in CORNERS:
        concrete.append(box(f'Pad_{sx}{sy}', 1.6, 1.6, 0.5,
                            (sx * S_BASE, sy * S_BASE, 0.25)))

    # ── ② 主角钢 4 根（连续收分杆）─────────────────────────────────────
    for sx, sy in CORNERS:
        galv.append(make_strut(f'Leg_{sx}{sy}',
                               (sx * S_BASE, sy * S_BASE, 0.5),
                               (sx * S_TOP, sy * S_TOP, LEVELS[-1]), LEG_W))

    # ── ③ 每节：4 面横杆 + X 斜撑 ──────────────────────────────────────
    def cpos(ci, z):
        sx, sy = CORNERS[ci]
        s = spread(z)
        return (sx * s, sy * s, z)

    for pi in range(len(LEVELS) - 1):
        z0, z1 = LEVELS[pi], LEVELS[pi + 1]
        for ci in range(4):
            a, b = cpos(ci, z1), cpos((ci + 1) % 4, z1)
            galv.append(make_strut(f'H{pi}_{ci}', a, b, HORIZ_W))
            # X 斜撑：本节两对角
            for flip in (0, 1):
                p0 = cpos(ci, z0 if not flip else z1)
                p1 = cpos((ci + 1) % 4, z1 if not flip else z0)
                galv_dark.append(make_strut(f'X{pi}_{ci}{flip}', p0, p1, BRACE_W))

    # ── ④ 平台 ×2（z 12.5 / 16.55）：环形走道板 + 栏杆（立柱×8 + 双层扶手）
    def platform(z, tag):
        s = spread(z) + 0.42
        for ci in range(4):                                 # 走道板（4 边）
            w = 2 * s if ci % 2 == 0 else 0.6
            d = 0.6 if ci % 2 == 0 else 2 * s
            cx = 0 if ci % 2 == 0 else CORNERS[ci][0] * (s - 0.3)
            cy = CORNERS[ci][1] * (s - 0.3) if ci % 2 == 0 else 0
            galv_dark.append(box(f'Deck{tag}_{ci}', w, d, 0.06, (cx, cy, z)))
        for sx in (-1, 1):                                  # 栏杆立柱 ×8
            for sy in (-1, 1):
                galv.append(box(f'RailPost{tag}_{sx}{sy}', 0.06, 0.06, 1.05,
                                (sx * (s - 0.12), sy * (s - 0.12), z + 0.525)))
        for rz, name in ((z + 1.02, 'RailTop'), (z + 0.55, 'RailMid')):
            for ci in range(4):
                w = 2 * (s - 0.12) if ci % 2 == 0 else 0.05
                d = 0.05 if ci % 2 == 0 else 2 * (s - 0.12)
                cx = 0 if ci % 2 == 0 else CORNERS[ci][0] * (s - 0.12)
                cy = CORNERS[ci][1] * (s - 0.12) if ci % 2 == 0 else 0
                galv.append(box(f'{name}{tag}_{ci}', w, d, 0.05, (cx, cy, rz)))

    platform(12.5, 'A')
    platform(16.55, 'B')

    # ── ⑤ 抛物面微波天线 ×3（±X / +Y，挂上层平台边缘）──────────────────
    DISH_Z = 15.55

    def add_dish(direction_axis: str, sign: float):
        s = spread(DISH_Z)
        if direction_axis == 'x':
            center = (sign * (s + 0.72), 0.0, DISH_Z)
            # make_taper 大半径(rim)在局部 -Z：rotY(∓π/2) 把 -Z 转到 ±X ⇒ 碗口朝外
            rot = (0.0, -sign * math.pi / 2, 0.0)
        else:
            center = (0.0, sign * (s + 0.72), DISH_Z)
            rot = (sign * math.pi / 2, 0.0, 0.0)
        n = f'Dish_{direction_axis}{sign:+.0f}'
        dish.append(make_taper(n + '_Bowl', 1.5, 0.30, 0.38, 16, center, rot=rot))
        # 馈源杆 + 馈源（碗口前方 0.9 m）
        fx = center[0] + (sign * 0.9 if direction_axis == 'x' else 0)
        fy = center[1] + (sign * 0.9 if direction_axis == 'y' else 0)
        dish.append(make_strut(n + '_FeedRod', center, (fx, fy, DISH_Z), 0.05))
        dish.append(box(n + '_Horn', 0.18, 0.18, 0.26, (fx, fy, DISH_Z)))
        # 背架杆（碗背到塔身）
        bx = (sign * s if direction_axis == 'x' else 0)
        by = (sign * s if direction_axis == 'y' else 0)
        dish.append(make_strut(n + '_BackStay', (bx, by, DISH_Z), center, 0.07))

    add_dish('x', 1.0)
    add_dish('x', -1.0)
    add_dish('y', 1.0)

    # ── ⑥ 柱状天线阵列 ×6（三扇区 × 2 高度，玻璃钢护罩）──────────────────
    for z, tag in ((13.35, 'Lo'), (15.75, 'Hi')):
        for axis, sign in (('x', 1.0), ('x', -1.0), ('y', 1.0)):
            s = spread(z)
            px = (sign * (s + 0.30) if axis == 'x' else 0.0)
            py = (sign * (s + 0.30) if axis == 'y' else 0.0)
            n = f'Ant{tag}_{axis}{sign:+.0f}'
            radome.append(cyl(n + '_Tube', 0.18, 0.18, 2.40, 10, (px, py, z + 1.2)))
            galv_dark.append(make_strut(n + '_Arm',
                                        (sign * s if axis == 'x' else 0.0,
                                         sign * s if axis == 'y' else 0.0, z + 0.4),
                                        (px, py, z + 0.45), 0.06))

    # ── ⑦ 塔顶：小平台 + 机柜 + 避雷针 + 障碍灯（总高 20.5）────────────
    s_top = spread(LEVELS[-1]) + 0.42
    galv_dark.append(box('TopDeck', 2 * s_top - 0.6, 2 * s_top - 0.6, 0.06, (0, 0, 19.43)))
    cabinet.append(box('Cabinet', 0.78, 0.78, 0.55, (0, 0, 19.46 + 0.275)))
    galv.append(cyl('LightningRod', 0.025, 0.025, 1.50, 6, (0, 0, 20.05)))
    warn.append(make_sphere('Beacon', 0.08, 10, (0, 0, H_CONTRACT - 0.08)))
    warn.append(make_sphere('WarnMid', 0.06, 8,
                            (S_BASE * 0.98, S_BASE * 0.98, 12.6)))

    # ── ⑧ 直爬梯（-Y 面外侧，随塔身收分；梯 + 简化护圈环盘）────────────
    rail_off = 0.14
    for seg in range(len(LEVELS) - 1):
        z0, z1 = LEVELS[seg], LEVELS[seg + 1]
        for rx in (-0.22, 0.22):
            galv.append(make_strut(
                f'LadderRail_{seg}_{rx:+.2f}',
                (rx, -(spread(z0) + rail_off), z0 + 0.30),
                (rx, -(spread(z1) + rail_off), z1 + 0.30 if seg < len(LEVELS) - 2 else z1 - 0.6), 0.04))
    z = LEVELS[0] + 0.7
    while z < 18.6:
        y = -(spread(z) + rail_off)
        galv_dark.append(make_strut(f'Rung_{z:.1f}', (-0.20, y, z), (0.20, y, z), 0.035))
        z += 0.42
    for hz in (4.0, 7.5, 11.0, 14.5):                      # 简化护圈（环盘）
        galv_dark.append(cyl(f'Hoop_{hz:.0f}', 0.42, 0.42, 0.03, 12,
                             (0.0, -(spread(hz) + 0.05), hz)))

    # ── 材质：2 桶烘焙 + 5 桶平色 ──────────────────────────────────────
    bake_buckets = [
        ('CommTower_Galv', galv, CITY_PALETTE['rail_mast'], 0.45, 0.75, 0.38, 3.5),
        ('CommTower_GalvDark', galv_dark, CITY_PALETTE['steel_dark'], 0.52, 0.65, 0.45, 5.0),
    ]
    flat_buckets = [
        ('CommTower_Concrete', concrete, CITY_PALETTE['concrete_dark'], 0.88, 0.0, None, None),
        ('CommTower_Dish', dish, CITY_PALETTE['reflect_white'], 0.42, 0.1, None, None),
        ('CommTower_Radome', radome, CITY_PALETTE['plastic'], 0.55, 0.0, None, None),
        ('CommTower_Cabinet', cabinet, CITY_PALETTE['vent_louver'], 0.6, 0.3, None, None),
        ('CommTower_Warn', warn, CITY_PALETTE['signal_red'], 0.35, 0.0,
         CITY_PALETTE['signal_red'], 1.2),
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
        shared_weathered(objs, col, rough, metal, name=name + '_Mat',
                         wear=wear, scale=scale)
        mat = objs[0].data.materials[0]
        smart_uv(objs)
        img = bake_bucket(mat, objs, size=256)
        wire_baked(mat, img)
        j = join_objects(objs, name)
        assign_material(j, mat)
        joined.append(j)

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
                else '/tmp/comm_tower.glb')
    export_glb(out_path, image_format='JPEG')
    size = os.path.getsize(out_path)
    print(f'✅ comm_tower.glb exported: {out_path} ({size} bytes)', flush=True)
    assert size <= 500 * 1024, f'超 500 KB 上限（§27.3-1）: {size}'


if __name__ == '__main__':
    main()
