#!/usr/bin/env python3
"""
build_vehicle_taxi — 出租车（taxi）Blender headless 导出脚本（批次 41 重制）。

taxi = sedan 家族几何（build_vehicle_sedan.build_sedan_family）+ 黄涂装 + 顶灯，
再按 REAL_DIMS_M 差值做 2% 级非均匀缩放（烘焙进顶点，节点保持 identity）：

    sedan 4.60×1.45×1.82 → taxi 4.70×1.50×1.85
    scale = (0.470/0.460, 0.185/0.182, 0.150/0.145) ≈ (1.0217, 1.0165, 1.0345)

顶灯（'TaxiSign' emissive 暖白）随几何一同缩放；前端运行时按材质名调制昼夜。

坐标与尺度规约与 sedan 一致（Blender Z-up / 1 单位 = 10 m / 导出 Yup）。
目标包围盒（世界单位）：X 0.470 × Z 0.185 × Y 0.150，轮底 minY = 0。
"""
import os
import sys

import bpy

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)

from __common__ import (  # noqa: E402
    CITY_PALETTE, export_glb, reset_scene, set_unit_meters,
)
from build_vehicle_sedan import build_sedan_family  # noqa: E402

# taxi / sedan 包围盒差（REAL_DIMS_M 行值）
SCALE = (0.470 / 0.460, 0.185 / 0.182, 0.150 / 0.145)


def build_vehicle_taxi():
    obj = build_sedan_family(CITY_PALETTE['taxi_yellow'], roof_sign=True)
    obj.scale = SCALE
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    obj.name = 'VehicleTaxi'
    return obj


if __name__ == '__main__':
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv \
        else '/tmp/vehicle_taxi.glb'
    reset_scene()
    set_unit_meters()
    obj = build_vehicle_taxi()
    export_glb(out_path)
    print(f'[build_vehicle_taxi] wrote {out_path}', flush=True)
