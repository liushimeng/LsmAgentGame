#!/usr/bin/env python3
"""
build_snow_mountain — 雪山（snow_mountain.glb）Blender headless 导出脚本
（批次 26「城市坐标系统与四缘环境带」§2.3，北·雪山带脊线用）。

⚠ 单位口径：**世界单位**（1 单位 = 10 m，见前端
  ClientWeb/src/components/virtualCity/cityScale.ts::METERS_PER_UNIT）。
  与既有 GLB（civic/city_hall、road/trash_can 等）一致。
  本脚本米制尺寸常量必须 ×U（U=0.1）换算，否则前端 scale 全错
  （批次 26 首版曾按真实米导出，25.5 单位高的山被前端再 ×6~10 →
  1500~2500m 巨山，相机被包进山体整屏单色）。

几何（米制 → 世界单位，Blender 原生 Z-up，pivot 在地面中心 (0,0,0)）：
  - 主峰锥体（灰岩 #7a7d84，8 棱低多边形），底 r=14m，高 25m → 1.4u / 2.5u
  - 次峰肩锥（同色略深），底 r=8m，高 12m，偏移 x=+9m
  - 山腰绿带（暗绿 #4a7a3f，截锥环带，微凸出岩面防 z-fighting）
  - 雪顶（白 #f0f4f8，顶部锥套）

朝向规约同 build_trash_can.py（批次 24 实测定稿）：Blender Z-up 摆放，
glTF Yup 转换后 three.js 中直立、底面贴 y=0，前端 <Model> 零旋转直挂。
前端以 scale 拉出 150~250m 高变化（基准高 2.5u = 25m）。

调用：
  cd 3d_script && blender --background --python build_snow_mountain.py -- <输出.glb>
"""
import bpy
import sys
import os

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (  # noqa: E402
    reset_scene, set_unit_meters,
    apply_pbr, join_objects, export_glb,
)

# 米 → 世界单位（1 世界单位 = 10 m，cityScale.METERS_PER_UNIT）
U = 0.1

ROCK = '#7a7d84'
ROCK_DARK = '#6a6d74'
BELT = '#4a7a3f'
SNOW = '#f0f4f8'

MAIN_R_M, MAIN_H_M = 14.0, 25.0   # 米


def _cone(name: str, r_bot: float, r_top: float, h: float, segs: int, pos):
    """真圆锥/截锥（__common__.make_cone 是历史遗留直柱实现，本脚本不依赖）。

    直接走 bpy.ops.mesh.primitive_cone_add，radius1=底 radius2=顶 depth=高，
    精确尺寸、无 object scale（join 前无需 transform_apply 也有干净顶点）。
    入参单位 = 世界单位。
    """
    bpy.ops.mesh.primitive_cone_add(vertices=max(segs, 8), radius1=r_bot,
                                    radius2=r_top, depth=h, location=pos)
    obj = bpy.context.active_object
    obj.name = name
    return obj


def build_snow_mountain() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    main_r = MAIN_R_M * U          # 1.4u
    main_h = MAIN_H_M * U          # 2.5u

    objs = []

    # 主峰（灰岩锥，8 棱低多边形，z ∈ [0, 2.5u]）
    main = _cone('MainPeak', main_r, 0.0, main_h, 8, (0, 0, main_h / 2))
    apply_pbr(main, ROCK, rough=0.95, metal=0.0)
    objs.append(main)

    # 次峰肩（偏移 x=+0.9u，半倚主峰，z ∈ [0, 1.2u]）
    shoulder = _cone('Shoulder', 8.0 * U, 0.0, 12.0 * U, 8, (9.0 * U, 1.5 * U, 6.0 * U))
    apply_pbr(shoulder, ROCK_DARK, rough=0.95, metal=0.0)
    objs.append(shoulder)

    # 山腰绿带（截锥环带 z ∈ [0.1u, 0.7u]，半径微凸出岩面防 z-fighting）
    # 主峰在高度 z 处半径 = main_r * (1 - z/main_h)
    belt = _cone('GreenBelt', 13.9 * U, 10.6 * U, 6.0 * U, 8, (0, 0, 4.0 * U))
    apply_pbr(belt, BELT, rough=0.9, metal=0.0)
    objs.append(belt)

    # 雪顶（z ∈ [1.2u, 2.55u] 锥套：底半径 0.82u 明显凸出岩面 0.728u，
    # 顶略高于岩尖 2.5u，保证雪顶完全包裹峰顶）
    snow = _cone('SnowCap', 8.2 * U, 0.0, 13.5 * U, 8, (0, 0, 18.75 * U))
    apply_pbr(snow, SNOW, rough=0.6, metal=0.0)
    objs.append(snow)

    joined = join_objects(objs, 'SnowMountain')
    # 烘焙全部 transform → 顶点世界坐标，节点保持 identity（pivot=地面中心）
    bpy.ops.object.select_all(action='DESELECT')
    joined.select_set(True)
    bpy.context.view_layer.objects.active = joined
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    return joined


if __name__ == '__main__':
    build_snow_mountain()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/snow_mountain.glb'
    export_glb(out_path)
    print(f'[size] {os.path.getsize(out_path)} bytes', flush=True)
