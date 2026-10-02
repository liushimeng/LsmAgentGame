#!/usr/bin/env python3
"""
build_park_fitness — 批次 45「公园设施真实感」健身三件套
（civic/park_fitness.glb，新建：双位太空漫步机 + 扭腰器 + 单杠）。

── 新建理由 ─────────────────────────────────────────────────────────────
  GB 51192-2016《公园设计规范》§8.6「游戏健身设施」+ 现代中国城市公园的
  标配（全民健身工程），全城现状为零。「漫步机 + 扭腰器 + 单杠」是
  社区公园健身角出现频率最高的三件套组合。

── §27.0-1 真实规格调研（GB 19272 室外健身器材的安全通用要求）──────────
  | 器材 | 真实规格                                    | 依据                    | 本件取值 |
  |------|---------------------------------------------|-------------------------|----------|
  | 双位太空漫步机 | 外形 2.58×0.67×1.98 m；立柱 ≥⌀89 mm | GB 19272-2011 / 教育局采购参数 | 柱 ⌀0.089 h1.10 + 顶梁 + 4 摆杆 + 4 踏板 |
  | 扭腰器 | 转盘 ⌀0.6~0.65 × 高 0.10~0.15；扶手高 0.9~1.1 | 室外健身器材产品规格 | 盘 ⌀0.65@0.12 + 立柱 + 扶手杠 |
  | 单杠 | 杜高 1.5~2.4、宽 1.2~1.5、杜径 ⌀28~33 mm     | GB 19272 / 体育器材惯例 | 杜高 2.10、宽 1.30、杜 ⌀0.033 |
  | 布局 | 三件沿步道一字排开，间距 ≥1.2（跌落半径）    | 健身角设计惯例           | 漫步机 +X / 扭腰器中 / 单杠 -X |

  长轴 = X 属正确摆放（axis='x_flat'）。涂装 = 全民健身工程「器材绿」，
  踏板/转盘/杠面为深灰塑料包覆层（材质槽 2）。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  Z-up 米制；节点 identity；minY=0；X/Z 内容盒居中。

用法：
  blender --background --python build_park_fitness.py -- \
    ClientWeb/src/assets/models/civic/park_fitness.glb
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
    make_box, make_taper,
    make_material, assign_material, join_objects,
    export_glb, center_content_xz, CITY_PALETTE,
)

WALKER_X = 1.90      # 漫步机中心
TWIST_X = 0.0        # 扭腰器中心
BAR_X = -1.60        # 单杠中心
POST_R = 0.0445      # ⌀0.089（GB 19272 立柱下限）
BAR_H = 2.10


def main():
    reset_scene()
    set_unit_meters()

    steel, pad = [], []

    # ── ① 双位太空漫步机（2.58 × 0.67 × 1.98）──────────────────────────
    # 立柱 ×2 + 顶梁（沿 Y）+ 摆杆 ×4 + 踏板 ×4 + 扶手杠 ×2
    # 柱脚地脚法兰（GB 19272 地锚语义；预览判读补）
    for x, y in ((WALKER_X, -0.33), (WALKER_X, 0.33),
                 (TWIST_X, 0.0), (BAR_X, -0.65), (BAR_X, 0.65)):
        steel.append(make_taper(f'Flange_{x}_{y}', 0.05, 0.05, 0.04, 8, (x, y, 0.02)))
    for s in (-1, 1):
        steel.append(make_taper(f'WalkerPost_{s}', POST_R, POST_R, 1.10, 8,
                                (WALKER_X, s * 0.33, 0.55)))
    steel.append(make_taper('WalkerTop', POST_R, POST_R, 0.66, 8,
                            (WALKER_X, 0, 1.10), rot=(math.pi / 2, 0, 0)))
    for s in (-1, 1):
        steel.append(make_box(f'WalkerHandle_{s}', (0.35, 0.04, 0.04),
                              (WALKER_X + 0.24, s * 0.33, 1.02)))
    for s in (-1, 1):                       # 每站 2 摆杆（前后各一）
        for f in (-1, 1):
            steel.append(make_taper(
                f'WalkerArm_{s}_{f}', 0.018, 0.014, 0.86, 6,
                (WALKER_X + f * 0.075, s * 0.165, 1.10 - 0.43)))
            pad.append(make_box(
                f'WalkerFoot_{s}_{f}', (0.30, 0.12, 0.04),
                (WALKER_X + f * 0.15, s * 0.165, 0.21)))

    # ── ② 扭腰器（转盘 ⌀0.65 + 立柱 + 扶手杠）──────────────────────────
    steel.append(make_taper('TwistPost', 0.038, 0.034, 1.05, 8, (TWIST_X, 0, 0.525)))
    pad.append(make_taper('TwistDisc', 0.325, 0.30, 0.10, 16, (TWIST_X, 0, 0.10)))
    steel.append(make_box('TwistHandle', (0.05, 0.90, 0.05), (TWIST_X, 0, 1.00)))
    for s in (-1, 1):                       # 扶手弯把（两端小握柱）
        steel.append(make_box(f'TwistGrip_{s}', (0.04, 0.04, 0.16),
                              (TWIST_X, s * 0.45, 0.92)))

    # ── ③ 单杠（杜高 2.10、宽 1.30、杜径 ⌀33 mm）───────────────────────
    for s in (-1, 1):
        steel.append(make_taper(f'BarPost_{s}', 0.038, 0.034, BAR_H, 8,
                                (BAR_X, s * 0.65, BAR_H / 2)))
    pad.append(make_taper('BarCross', 0.0165, 0.0165, 1.30, 8,
                          (BAR_X, 0, BAR_H), rot=(math.pi / 2, 0, 0)))

    # ── 合并（材质槽 2：器材绿钢构 / 深灰塑料包覆）──────────────────────
    mat_steel = make_material('Fitness_Steel_Mat', CITY_PALETTE['park_steel_green'],
                              rough=0.42, metal=0.55)
    mat_pad = make_material('Fitness_Pad_Mat', CITY_PALETTE['fitness_pad'],
                            rough=0.65, metal=0.05)
    groups = [
        (steel, mat_steel, 'Fitness_Steel'),
        (pad, mat_pad, 'Fitness_Pad'),
    ]
    joined = [join_objects(objs, name) for objs, _m, name in groups]
    for (_objs, mat, _name), obj in zip(groups, joined):
        assign_material(obj, mat)

    # ⚠ 全件米 → 世界单位（×0.1）：同 build_palm_tree 的 U=0.1 契约。
    # ⚠ 先 flatten（join 保留首对象变换），再 ×0.1（详见 build_park_pavilion 注释）。
    for obj in joined:
        bpy.ops.object.select_all(action='DESELECT')
        obj.select_set(True)
        bpy.context.view_layer.objects.active = obj
        bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
        obj.scale = (0.1, 0.1, 0.1)

    # 贴地钉扎：倾斜件的柱帽边缘会低于 0（秋千 A 腿 / 倾斜板条端头），
    # 按「本对象 bbox 最低点」整体抬升钉扎到 minY=0（位置由导出烘焙）。
    for obj in joined:
        min_z = min(bb[2] for bb in obj.bound_box)
        if min_z < 0:
            obj.location.z = -0.1 * min_z

    center_content_xz(joined)

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv
                else '/tmp/park_fitness.glb')
    export_glb(out_path)
    print(f'✅ park_fitness.glb exported: {out_path} '
          f'({os.path.getsize(out_path)} bytes)', flush=True)


if __name__ == '__main__':
    main()
