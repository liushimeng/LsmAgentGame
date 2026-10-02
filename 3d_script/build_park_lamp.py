#!/usr/bin/env python3
"""
build_park_lamp — 批次 45「公园设施真实感」庭院灯
（civic/park_lamp.glb，新建：单头方灯罩庭院灯）。

── 新建理由 ─────────────────────────────────────────────────────────────
  GB 51192-2016《公园设计规范》§10.2：园内照明分功能性 / 景观性 / 装饰性。
  中央公园现状夜间全黑（路灯在园外路缘）；园灯是公园夜景的骨架设施。

── §27.0-1 真实规格调研（庭院灯厂家规格汇总）───────────────────────────
  | 部件 | 真实规格                                     | 依据                     | 本件取值 |
  |------|----------------------------------------------|--------------------------|----------|
  | 灯高 | 庭院步道灯 2.5 / 2.8 / 3.0 / 3.5 / 4.0 m    | 七度照明 / 朗苑照明       | 3.0 m（公园取中低档）|
  | 照明半径 | 2.5~3.5 m 灯高 ⇒ 光池 ⌀5~8 m ⇒ 间距 8~12 m | 同上                    | 布点间距 10 m（前端 C3）|
  | 主杆 | ⌀0.06~0.09、壁厚 ≥2.5 mm                    | 同上                      | ⌀0.070→0.056 收分 + 2 道凸纹箍 |
  | 灯头 | 方柱灯罩 0.2~0.3 m + 四坡小顶 + 顶球         | 庭院灯产品图集            | 罩 0.24×0.30×0.24 + 顶 + 球 |
  | 基座 | 法兰 ⌀0.25~0.35                              | 同上                      | 两层 ⌀0.30/⌀0.22 × 0.18 |

── 夜灯接线（前端 ParkLamps.tsx）───────────────────────────────────────
  灯罩材质名固定 **ParkLamp_Lantern_Mat** —— 前端按材质名 traverse 共享 scene，
  设 emissive 暖白 + emissiveIntensity 随 cityTimeStore.dayFactor01 昼 0.15 → 夜 2.6
  （同批次 44 季节调制 / 批次 41 车灯范式），并配 ⌀6 m additive 地面光斑。

  直立件（y 主轴），常规直立/贴地判据。材质槽 2：灯体深灰钢 / 灯罩暖白。

── 坐标与尺度规约（CLAUDE.md §27.3）────────────────────────────────────
  Z-up 米制；节点 identity；minY=0；X/Z 内容盒居中。

用法：
  blender --background --python build_park_lamp.py -- \
    ClientWeb/src/assets/models/civic/park_lamp.glb
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
    make_box, make_taper, make_sphere,
    make_material, assign_material, join_objects,
    export_glb, CITY_PALETTE,
)


def main():
    reset_scene()
    set_unit_meters()

    body, lantern = [], []

    # ① 基座两层法兰（0.18 高）
    body.append(make_taper('Base_L1', 0.150, 0.130, 0.10, 10, (0, 0, 0.05)))
    body.append(make_taper('Base_L2', 0.110, 0.095, 0.08, 10, (0, 0, 0.14)))
    # ② 主杆（收分 ⌀0.070 → ⌀0.056，2.44 长）+ 2 道凸纹箍 + 灯座颈
    body.append(make_taper('Pole', 0.035, 0.028, 2.44, 10, (0, 0, 0.18 + 1.22)))
    for i, z in enumerate((0.55, 1.95)):
        body.append(make_taper(f'DecoRing_{i}', 0.062, 0.062, 0.03, 10, (0, 0, z)))
    body.append(make_taper('Collar', 0.090, 0.048, 0.07, 8, (0, 0, 2.655)))
    # ③ 灯罩（方柱 0.24×0.30×0.24，暖白发光材质 ParkLamp_Lantern_Mat）
    lantern.append(make_box('Lantern', (0.24, 0.24, 0.30), (0, 0, 2.89)))
    # ④ 四坡小顶（4 段锥旋转 45° 对齐方形）+ 顶球
    body.append(make_taper('Cap', 0.195, 0.020, 0.10, 4, (0, 0, 3.09),
                           rot=(0, 0, math.pi / 4)))
    body.append(make_sphere('Tip', 0.045, 10, (0, 0, 3.16)))

    # ── 合并（材质槽 2）─────────────────────────────────────────────────
    mat_body = make_material('ParkLamp_Body_Mat', CITY_PALETTE['steel_dark'],
                             rough=0.5, metal=0.6)
    mat_lantern = make_material('ParkLamp_Lantern_Mat', '#f2e3c0',
                                rough=0.35, metal=0.0)
    groups = [
        (body, mat_body, 'ParkLamp_Body'),
        (lantern, mat_lantern, 'ParkLamp_Lantern'),
    ]
    joined = [join_objects(objs, name) for objs, _m, name in groups]
    for (_objs, mat, _name), obj in zip(groups, joined):
        assign_material(obj, mat)

    # ⚠ 全件米 → 世界单位（×0.1）：同 build_palm_tree 的 U=0.1 契约。
    # ⚠ 先 flatten（join 保留首对象变换；Lantern 是 make_box 件，scale 也在其中），
    # 再 ×0.1（详见 build_park_pavilion 注释）。
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

    out_path = (sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv
                else '/tmp/park_lamp.glb')
    export_glb(out_path)
    print(f'✅ park_lamp.glb exported: {out_path} '
          f'({os.path.getsize(out_path)} bytes)', flush=True)


if __name__ == '__main__':
    main()
