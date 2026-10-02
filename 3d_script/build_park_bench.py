#!/usr/bin/env python3
"""
build_park_bench — 批次 45「公园设施真实感」公园长椅
（civic/park_bench.glb，新建：三人位防腐木 + 铸铝弓形脚）。

── 新建理由 ─────────────────────────────────────────────────────────────
  GB 51192-2016《公园设计规范》§3.5 条文说明：座椅应主要分布在游人集中活动
  的场所，「沿园路布置时考虑到老年人行走易疲劳，建议间隔 50~100 m」——
  中央公园园路 2 × 74 m 应有 4~8 处座椅，现状全城公园零座椅。

── §27.0-1 真实规格调研（GB 3326-1997 + 人体工程学 + 市政家具惯例）────
  | 部件 | 真实规格                                   | 依据                  | 本件取值 |
  |------|--------------------------------------------|-----------------------|----------|
  | 座面高| 400~440 mm（户外件常 430~450）             | GB 3326-1997          | 430 mm |
  | 座深 | 带靠背 340~420 mm                          | 人体工程学尺寸汇总    | 5×70 板条 + 4×15 缝 = 410 mm |
  | 座面倾角| 5°~10°                                  | 同上                  | 6°（后倾）|
  | 靠背倾角| 100°~115°（公共排椅常 100~110）          | 同上                  | 103°（对垂线 ~7°）|
  | 长度 | 1.2 / 1.5 / 1.8 m（2/3/4 人位）            | 防腐木长椅产品规格    | 1.80 m（三人位）|
  | 板条 | 座板厚 35~45 mm；座 4~5 条 / 背 2~3 条      | 同上                  | 座 5×40 / 背 3×35，缝 15 mm |
  | 脚架 | 铸铝/铁艺弓形脚 ×2                         | 市政街道家具惯例      | 铸铝弓形脚（脚段+前后撑 3 段折线）|

  座椅长轴 = X 属正确摆放（axis='x_flat'，同 bikeRack/busStop 先例）；
  座面向 +Z（导出后 glTF +Z 面向即「面朝」方向），前端朝前。
  材质槽 2：防腐木板条 / 铸铝脚。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  Z-up 米制；节点 identity；minY=0；X/Z 内容盒居中。

用法：
  blender --background --python build_park_bench.py -- \
    ClientWeb/src/assets/models/civic/park_bench.glb
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
    make_box,
    make_material, assign_material, join_objects,
    export_glb, center_content_xz, CITY_PALETTE,
)

BENCH_L = 1.80
SEAT_H = 0.43
SLAT_D, SLAT_T = 0.07, 0.04        # 板条宽（Y）/ 厚（Z）
SLAT_GAP = 0.015
SEAT_TILT = math.radians(6.0)      # 座面后倾
BACK_TILT = math.radians(7.0)      # 靠背对垂线后倾（座面 6° + 背 7° ≈ 103°）
BACK_TOP = 0.86
LEG_X = 0.70


def main():
    reset_scene()
    set_unit_meters()

    wood, metal = [], []

    # ── ① 座面 5 板条（y=+0.16 前缘 → -0.16 后缘；6° 后倾 ⇒ 前高后低）──
    ys = [-0.16 + i * (SLAT_D + SLAT_GAP) for i in range(5)]   # -0.16 … +0.16
    for i, y in enumerate(ys):
        z = SEAT_H + y * math.tan(SEAT_TILT) - SLAT_T / 2
        wood.append(make_box(f'Slat_{i}', (BENCH_L, SLAT_D, SLAT_T),
                             (0, y, z), rot=(SEAT_TILT, 0, 0)))
    # ── ② 靠背 3 板条（7° 后倾：越高越靠后）─────────────────────────────
    back_ys = [-0.19 - (z - 0.50) * math.tan(BACK_TILT)
               for z in (0.52, 0.68, 0.84)]
    for i, (z, y) in enumerate(zip((0.52, 0.68, 0.84), back_ys)):
        wood.append(make_box(f'BackSlat_{i}', (BENCH_L, SLAT_T, 0.09),
                             (0, y, z), rot=(-BACK_TILT, 0, 0)))
    # ── ③ 铸铝脚 ×2 + 座面横撑（弓形 3 段折线：脚段 + 前后立撑）─────────
    for s in (-1, 1):
        x = s * LEG_X
        # 脚段（贴地沿 Y）
        metal.append(make_box(f'Foot_{s}', (0.05, 0.46, 0.06), (x, -0.01, 0.03)))
        # 前撑（脚前 → 座底，内倾）
        metal.append(make_box(f'StrutF_{s}', (0.05, 0.05, 0.42),
                              (x, 0.155, 0.215), rot=(0.10, 0, 0)))
        # 后撑（脚后 → 靠背中，外倾）
        metal.append(make_box(f'StrutB_{s}', (0.05, 0.05, 0.56),
                              (x, -0.155, 0.28), rot=(-0.12, 0, 0)))
        # 座面横撑
        metal.append(make_box(f'SeatBeam_{s}', (0.05, 0.44, 0.04), (x, 0, 0.375)))

    # ── 合并（材质槽 2）─────────────────────────────────────────────────
    mat_wood = make_material('Bench_Wood_Mat', CITY_PALETTE['wood'],
                             rough=0.78, metal=0.0)
    mat_metal = make_material('Bench_Leg_Mat', CITY_PALETTE['bench_leg'],
                              rough=0.5, metal=0.7)
    groups = [
        (wood, mat_wood, 'Bench_Wood'),
        (metal, mat_metal, 'Bench_Leg'),
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
                else '/tmp/park_bench.glb')
    export_glb(out_path)
    print(f'✅ park_bench.glb exported: {out_path} '
          f'({os.path.getsize(out_path)} bytes)', flush=True)


if __name__ == '__main__':
    main()
