#!/usr/bin/env python3
"""
build_lighthouse — 灯塔（lighthouse.glb）Blender headless 导出脚本
（批次 26「城市坐标系统与四缘环境带」§2.3，南·海洋带南港用）。

⚠ 单位口径：**世界单位**（1 单位 = 10 m，见前端
  ClientWeb/src/components/virtualCity/cityScale.ts::METERS_PER_UNIT）。
  与既有 GLB（civic/city_hall、road/trash_can 等）一致。
  米制尺寸常量必须 ×U（U=0.1）换算，否则前端 scale 全错。

几何（米制 → 世界单位，Blender 原生 Z-up，pivot 在基座底面中心 (0,0,0)，
总高 14m = 1.4u）：
  - 基座：灰色石台圆柱 r=2.2m h=1.0m
  - 塔身：4 段红白相间截锥（r 1.5 → 0.95m 收分），每段 h=2.5m
  - 瞭望台：深色圆盘 r=1.25m h=0.3m
  - 灯室：暖黄自发光圆柱 r=0.8m h=1.5m（emissive，夜间可见）
  - 穹顶：红锥 r=1.0m h=1.2m

朝向规约同 build_trash_can.py：Blender Z-up → glTF/three.js 直立贴地，
前端 <Model> 零旋转直挂。

调用：
  cd 3d_script && blender --background --python build_lighthouse.py -- <输出.glb>
"""
import bpy
import sys
import os

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (  # noqa: E402
    reset_scene, set_unit_meters,
    make_cylinder, apply_pbr, join_objects, export_glb,
)

# 米 → 世界单位（1 世界单位 = 10 m，cityScale.METERS_PER_UNIT）
U = 0.1

STONE = '#8a8d92'
RED = '#c0392b'
WHITE = '#f2f0ea'
DARK = '#3a3d42'
LAMP = '#ffe9a8'


def _cone(name: str, r_bot: float, r_top: float, h: float, segs: int, pos):
    """真圆锥/截锥（__common__.make_cone 是历史遗留直柱实现，本脚本不依赖）。入参 = 世界单位。"""
    bpy.ops.mesh.primitive_cone_add(vertices=max(segs, 8), radius1=r_bot,
                                    radius2=r_top, depth=h, location=pos)
    obj = bpy.context.active_object
    obj.name = name
    return obj


def build_lighthouse() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    objs = []

    # 基座（z ∈ [0, 0.1u]）
    base = make_cylinder('Base', 2.2 * U, 2.4 * U, 1.0 * U, 12, (0, 0, 0.5 * U))
    apply_pbr(base, STONE, rough=0.9, metal=0.0)
    objs.append(base)

    # 塔身 4 段红白相间（z ∈ [0.1u, 1.1u]，收分 1.5 → 0.95m）
    band_colors = [RED, WHITE, RED, WHITE]
    for i in range(4):
        z_lo = 1.0 + i * 2.5          # 米
        t0 = i / 4.0
        t1 = (i + 1) / 4.0
        r_bot = 1.5 + (0.95 - 1.5) * t0
        r_top = 1.5 + (0.95 - 1.5) * t1
        band = _cone(f'Band_{i}', r_bot * U, r_top * U, 2.5 * U, 12,
                     (0, 0, (z_lo + 1.25) * U))
        apply_pbr(band, band_colors[i], rough=0.7, metal=0.0)
        objs.append(band)

    # 瞭望台（z ∈ [1.1u, 1.13u]）
    gallery = make_cylinder('Gallery', 1.25 * U, 1.25 * U, 0.3 * U, 12, (0, 0, 11.15 * U))
    apply_pbr(gallery, DARK, rough=0.6, metal=0.2)
    objs.append(gallery)

    # 灯室（z ∈ [1.13u, 1.28u]，暖黄自发光）
    lamp = make_cylinder('LampRoom', 0.8 * U, 0.8 * U, 1.5 * U, 10, (0, 0, 12.05 * U))
    apply_pbr(lamp, LAMP, rough=0.3, metal=0.0,
              emissive=LAMP, emissive_intensity=1.2)
    objs.append(lamp)

    # 穹顶（z ∈ [1.28u, 1.4u]，真圆锥尖顶）
    roof = _cone('Roof', 1.0 * U, 0.0, 1.2 * U, 12, (0, 0, 13.4 * U))
    apply_pbr(roof, RED, rough=0.6, metal=0.1)
    objs.append(roof)

    joined = join_objects(objs, 'Lighthouse')
    bpy.ops.object.select_all(action='DESELECT')
    joined.select_set(True)
    bpy.context.view_layer.objects.active = joined
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    return joined


if __name__ == '__main__':
    build_lighthouse()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/lighthouse.glb'
    export_glb(out_path)
    print(f'[size] {os.path.getsize(out_path)} bytes', flush=True)
