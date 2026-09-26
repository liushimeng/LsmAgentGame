#!/usr/bin/env python3
"""
build_cactus — 柱状仙人掌（cactus.glb）Blender headless 导出脚本
（批次 26「城市坐标系统与四缘环境带」§2.3，西·沙漠带 instanced 用）。

⚠ 单位口径：**世界单位**（1 单位 = 10 m，见前端
  ClientWeb/src/components/virtualCity/cityScale.ts::METERS_PER_UNIT）。
  与既有 GLB（civic/city_hall、road/trash_can 等）一致。
  米制尺寸常量必须 ×U（U=0.1）换算，否则前端 scale 全错。

几何（米制 → 世界单位，Blender 原生 Z-up，pivot 在地面中心 (0,0,0)，总高 2.92m = 0.29u）：
  - 主干：圆柱 r=0.32m h=2.6m + 顶部半球
  - 双臂：水平短节（沿 ±x 横放）+ 竖直上扬节 + 顶端半球，左右不对称

朝向规约同 build_trash_can.py：Blender Z-up → glTF/three.js 直立贴地，
前端 <Model> / InstancedMesh 零旋转直挂。

调用：
  cd 3d_script && blender --background --python build_cactus.py -- <输出.glb>
"""
import bpy
import sys
import os

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (  # noqa: E402
    reset_scene, set_unit_meters,
    make_cylinder, make_sphere, apply_pbr, join_objects, export_glb,
)

# 米 → 世界单位（1 世界单位 = 10 m，cityScale.METERS_PER_UNIT）
U = 0.1

GREEN = '#3e7d44'
GREEN_DARK = '#357039'


def build_cactus() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    objs = []

    # 主干（z ∈ [0, 0.26u]）+ 顶部半球 → 总高 ~0.292u
    trunk = make_cylinder('Trunk', 0.32 * U, 0.34 * U, 2.6 * U, 10, (0, 0, 1.3 * U))
    apply_pbr(trunk, GREEN, rough=0.85, metal=0.0)
    objs.append(trunk)
    top = make_sphere('TrunkTop', 0.32 * U, 10, (0, 0, 2.6 * U))
    apply_pbr(top, GREEN, rough=0.85, metal=0.0)
    objs.append(top)

    # 右臂：水平节（沿 +x 横放）+ 竖直节 + 半球
    arm_r_h = make_cylinder('ArmR_H', 0.17 * U, 0.17 * U, 0.55 * U, 8,
                            (0.45 * U, 0, 1.7 * U), rot=(0, 1.5708, 0))
    apply_pbr(arm_r_h, GREEN_DARK, rough=0.85, metal=0.0)
    objs.append(arm_r_h)
    arm_r_v = make_cylinder('ArmR_V', 0.16 * U, 0.17 * U, 1.0 * U, 8, (0.72 * U, 0, 2.2 * U))
    apply_pbr(arm_r_v, GREEN_DARK, rough=0.85, metal=0.0)
    objs.append(arm_r_v)
    arm_r_top = make_sphere('ArmR_Top', 0.16 * U, 8, (0.72 * U, 0, 2.7 * U))
    apply_pbr(arm_r_top, GREEN_DARK, rough=0.85, metal=0.0)
    objs.append(arm_r_top)

    # 左臂（略低、略短，不对称）：水平节 + 竖直节 + 半球
    arm_l_h = make_cylinder('ArmL_H', 0.15 * U, 0.15 * U, 0.5 * U, 8,
                            (-0.40 * U, 0, 1.3 * U), rot=(0, 1.5708, 0))
    apply_pbr(arm_l_h, GREEN_DARK, rough=0.85, metal=0.0)
    objs.append(arm_l_h)
    arm_l_v = make_cylinder('ArmL_V', 0.14 * U, 0.15 * U, 0.8 * U, 8, (-0.64 * U, 0, 1.7 * U))
    apply_pbr(arm_l_v, GREEN_DARK, rough=0.85, metal=0.0)
    objs.append(arm_l_v)
    arm_l_top = make_sphere('ArmL_Top', 0.14 * U, 8, (-0.64 * U, 0, 2.1 * U))
    apply_pbr(arm_l_top, GREEN_DARK, rough=0.85, metal=0.0)
    objs.append(arm_l_top)

    joined = join_objects(objs, 'Cactus')
    bpy.ops.object.select_all(action='DESELECT')
    joined.select_set(True)
    bpy.context.view_layer.objects.active = joined
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    return joined


if __name__ == '__main__':
    build_cactus()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/cactus.glb'
    export_glb(out_path)
    print(f'[size] {os.path.getsize(out_path)} bytes', flush=True)
