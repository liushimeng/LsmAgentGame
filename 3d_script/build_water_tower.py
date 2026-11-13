#!/usr/bin/env python3
"""
build_water_tower — 水塔（WaterTower）批次 90「地标真实感重制」。

── 重制理由：批次 19/29 首版缺核心构件──────────────────────────────────
  无中心立管（水塔功能核心）、无环梁（支腿与罐体悬空分离，规范 §6 判读记录）、
  无腿间斜撑、无走道栏杆、无安全笼、无避雷针。本批按球罐式高架水塔真实构造
  全量重制，并挂贴图烘焙通路（Tank / TankDark / Steel 三桶烘 256² baseColor）。

── §27.0-1 真实形态调研（球罐式高架水塔，美中西部 spheres-on-legs 一族）──
  | 部件     | 真实规格                              | 本件取值                  |
  |----------|---------------------------------------|---------------------------|
  | 罐体     | 球罐 ⌀3~8 m，焊接钢板分瓣带           | ⌀3.16 m（r 1.58）         |
  | 赤道走道 | 环形格栅检修走道 + 双层栏杆           | 16 段拼环（r 1.70/1.78）  |
  | 支承     | 4 支腿收分 + 腿间水平撑/X 撑 ×2 层    | 底 ±1.35 → 顶 ±0.95       |
  | 环梁     | 支腿与罐壁焊接过渡（贴球面接头）      | 支腿上端落在球面 z 9.76   |
  | 立管     | 中心进出水竖管 ⌀0.3~0.5 m + 法兰/阀组 | ⌀0.36 + 法兰×2 + 底阀     |
  | 爬梯     | 直爬梯 + 安全护圈（>3 m 规范）        | 梯 + 护圈环盘 ×5          |
  | 附件     | 罐顶人孔 + 避雷/障碍灯（=总高）       | 人孔 + 灯顶 13.3 m        |
  | 名带     | 罐体城市名色带（水塔涂装惯例）        | navy 带 h 0.5 @ z 11.0    |

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  · Z-up、建模即米制（导出前统一 ×0.1）。
  · 尺寸唯一事实来源 REAL_DIMS_M.waterTower = 3.6 × 3.6 × 13.3
    （x/z 由**走道栏杆外缘 ±1.80** 定义、y 由罐顶障碍灯定义），本批不变。
  · 节点 identity；minY = 0；X/Z 居中。

── 材质槽（6 ⇒ 6 primitive/DC；三桶烘焙，Obstruction 夜间 lit）──────────
  WaterTower_Tank(烘) / _TankDark(烘) / _Steel(烘) / _Riser / _Ladder /
  _Obstruction

用法：
  blender --background --python build_water_tower.py -- \
    ClientWeb/src/assets/models/civic/water_tower.glb
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
    make_box_wdh as box, make_cylinder as cyl, make_sphere,
    make_strut, make_material, assign_material, join_objects, export_glb,
    center_content_xz, shared_weathered, CITY_PALETTE,
)
from bake_texture import smart_uv, bake_bucket, wire_baked  # noqa: E402

# ── 几何（米）───────────────────────────────────────────────────────────
H_CONTRACT = 13.3                      # 总高（罐顶障碍灯顶面）
TANK_R, TANK_Z = 1.58, 10.6            # 球罐半径 / 球心高（span 9.02..12.18）
WALK_R, RAIL_R = 1.69, 1.78            # 走道半径 / 栏杆半径（外缘=±1.80 包线）
LEG_BOT, LEG_TOP = 1.35, 0.95          # 支腿半宽（地面/罐底）
LEG_Z0, LEG_Z1 = 0.40, 9.80            # 支腿底/顶标高（顶落在球面 z≈9.76）
BRACE_Z = (3.2, 6.4)                   # 腿间撑标高
CORNERS = ((-1, -1), (1, -1), (1, 1), (-1, 1))


def leg_xy(z: float):
    """支腿中心线 (x=y) 随高度线性内收。"""
    t = (z - LEG_Z0) / (LEG_Z1 - LEG_Z0)
    return LEG_BOT + (LEG_TOP - LEG_BOT) * t


def main() -> None:
    reset_scene()
    set_unit_meters(1.0)

    tank, tank_dark, steel = [], [], []
    riser, ladder, warn = [], [], []

    # ── ① 基础：4 垫块 + 立管基墩 ──────────────────────────────────────
    for sx, sy in CORNERS:
        steel.append(box(f'Pad_{sx}{sy}', 0.70, 0.70, 0.40,
                         (sx * LEG_BOT, sy * LEG_BOT, 0.20)))
    riser.append(cyl('RiserPlinth', 0.40, 0.40, 0.30, 12, (0, 0, 0.15)))

    # ── ② 4 支腿（收分杆，上端贴球面）──────────────────────────────────
    for sx, sy in CORNERS:
        steel.append(make_strut(f'Leg_{sx}{sy}',
                                (sx * LEG_BOT, sy * LEG_BOT, LEG_Z0),
                                (sx * LEG_TOP, sy * LEG_TOP, LEG_Z1), 0.16))

    # ── ③ 腿间水平撑 + X 撑 ×2 层 ──────────────────────────────────────
    for bz in BRACE_Z:
        s = leg_xy(bz)
        for ci in range(4):
            a, b = CORNERS[ci], CORNERS[(ci + 1) % 4]
            p0 = (a[0] * s, a[1] * s, bz)
            p1 = (b[0] * s, b[1] * s, bz)
            steel.append(make_strut(f'H_{bz:.0f}_{ci}', p0, p1, 0.08))
            # X：层内对角 + 层间对角（下一层顶到本层底）
            bz2 = BRACE_Z[1] if bz == BRACE_Z[0] else LEG_Z1
            for flip in (0, 1):
                q0 = (a[0] * s, a[1] * s, bz if not flip else bz2)
                q1 = (b[0] * leg_xy(bz2), b[1] * leg_xy(bz2), bz2 if not flip else bz)
                ladder.append(make_strut(f'X_{bz:.0f}_{ci}{flip}', q0, q1, 0.06))

    # ── ④ 中心立管（功能核心）+ 法兰 ×2 + 底部阀组 ─────────────────────
    riser.append(cyl('RiserPipe', 0.18, 0.18, TANK_Z - TANK_R - 0.30, 12,
                     (0, 0, (TANK_Z - TANK_R + 0.30) / 2)))
    for fz in (2.6, 5.6):
        riser.append(cyl(f'Flange_{fz:.0f}', 0.25, 0.25, 0.10, 12, (0, 0, fz)))
    riser.append(box('ValveBody', 0.52, 0.34, 0.38, (0, 0, 0.62)))
    riser.append(box('ValveHand', 0.30, 0.08, 0.08, (0, 0, 0.90)))

    # ── ⑤ 球罐 + 分瓣带（环向凸棱 ×2）+ 名带 ───────────────────────────
    tank.append(make_sphere('Tank', TANK_R, 36, (0, 0, TANK_Z)))
    for dz in (-0.62, 0.62):
        tank.append(cyl(f'Seam{dz:+.1f}', TANK_R + 0.008, TANK_R + 0.008, 0.06, 36,
                        (0, 0, TANK_Z + dz)))
    tank_dark.append(cyl('NameBand', TANK_R + 0.006, TANK_R + 0.006, 0.50, 36,
                         (0, 0, TANK_Z + 0.45)))

    # ── ⑥ 赤道检修走道（16 段格栅 + 立柱 + 双层扶手）───────────────────
    N = 16
    for i in range(N):
        a = 2 * math.pi * i / N
        ca, sa = math.cos(a), math.sin(a)
        # 格栅段（切向 0.62 × 径向 0.22：内缘贴球面 1.58、外缘恰 1.80 包线）
        steel.append(box(f'Walk_{i}', 0.62, 0.22, 0.05,
                         (ca * WALK_R, sa * WALK_R, TANK_Z),
                         rot=(0, 0, a + math.pi / 2)))
        # 栏杆立柱 + 上下扶手段
        steel.append(box(f'Post_{i}', 0.05, 0.05, 1.02,
                         (ca * RAIL_R, sa * RAIL_R, TANK_Z + 0.51)))
        for rz, rn in ((1.00, 'RailT'), (0.55, 'RailM')):
            a2 = 2 * math.pi * (i + 0.5) / N
            ca2, sa2 = math.cos(a2), math.sin(a2)
            steel.append(make_strut(
                f'{rn}_{i}',
                (ca * RAIL_R, sa * RAIL_R, TANK_Z + rz),
                (ca2 * RAIL_R, sa2 * RAIL_R, TANK_Z + rz), 0.045))

    # ── ⑦ 罐顶人孔 + 避雷/障碍灯（顶 = 13.30 契约值）───────────────────
    apex = TANK_Z + TANK_R
    tank_dark.append(cyl('Manhole', 0.22, 0.22, 0.15, 16, (0, 0, apex + 0.075)))
    tank.append(cyl('WTPole', 0.03, 0.03, 0.85, 8, (0, 0, apex + 0.15 + 0.425)))
    warn.append(make_sphere('ObstrLight', 0.09, 10, (0, 0, H_CONTRACT - 0.09)))

    # ── ⑧ 直爬梯 + 安全护圈 ×5（-Y 侧，贴腿面外侧）────────────────────
    LAD_Y = -1.46
    for rx in (-0.22, 0.22):
        ladder.append(make_strut(f'LadderRail_{rx:+.2f}',
                                 (rx, LAD_Y, 0.55), (rx, LAD_Y, TANK_Z - 0.05), 0.04))
    z = 0.9
    while z < TANK_Z - 0.1:
        ladder.append(make_strut(f'Rung_{z:.1f}', (-0.20, LAD_Y, z), (0.20, LAD_Y, z), 0.035))
        z += 0.42
    for hz in (2.4, 4.4, 6.4, 8.4, 10.0):
        ladder.append(cyl(f'Hoop_{hz:.0f}', 0.33, 0.33, 0.03, 14, (0.0, LAD_Y + 0.04, hz)))

    # ── 材质：3 桶烘焙 + 3 桶平色 ──────────────────────────────────────
    bake_buckets = [
        ('WaterTower_Tank', tank, CITY_PALETTE['tank_paint'], 0.55, 0.25, 0.30, 2.5),
        ('WaterTower_TankDark', tank_dark, CITY_PALETTE['tank_band'], 0.60, 0.15, 0.22, 3.0),
        ('WaterTower_Steel', steel, CITY_PALETTE['rail_mast'], 0.45, 0.75, 0.40, 4.0),
    ]
    flat_buckets = [
        ('WaterTower_Riser', riser, CITY_PALETTE['steel'], 0.50, 0.70, None, None),
        ('WaterTower_Ladder', ladder, CITY_PALETTE['steel_dark'], 0.55, 0.60, None, None),
        ('WaterTower_Obstruction', warn, CITY_PALETTE['signal_red'], 0.35, 0.0,
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
                else '/tmp/water_tower.glb')
    export_glb(out_path, image_format='JPEG')
    size = os.path.getsize(out_path)
    print(f'✅ water_tower.glb exported: {out_path} ({size} bytes)', flush=True)
    assert size <= 500 * 1024, f'超 500 KB 上限（§27.3-1）: {size}'


if __name__ == '__main__':
    main()
