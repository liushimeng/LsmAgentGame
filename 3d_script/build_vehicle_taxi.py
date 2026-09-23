#!/usr/bin/env python3
"""
build_vehicle_taxi — 出租（taxi）Blender headless 导出脚本（19-Blender3D模型集成）。

约定：x=车长方向, y=上, z=车宽方向。
"""
import bpy
import sys
import os
_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (
    reset_scene, set_unit_meters,
    make_box, make_cylinder, apply_pbr, join_objects, export_glb,
)

BODY = '#e8c020'      # 出租车经典黄
BODY_DARK = '#a08818'
ROOF_SIGN = '#222222'
ROOF_SIGN_FACE = '#ffd700'
WINDOW = '#1a2a3a'
TIRE = '#1a1a1a'
LAMP_FRONT = '#fff8e0'
LAMP_REAR = '#cc0000'


def build_vehicle_taxi() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 车身
    body = make_box('Body', (0.10, 0.025, 0.045), (0, 0.025 + 0.02, 0))
    apply_pbr(body, BODY, rough=0.4, metal=0.5)

    # 车顶
    roof = make_box('Roof', (0.06, 0.020, 0.04), (0, 0.025 + 0.025 + 0.02, 0))
    apply_pbr(roof, BODY_DARK, rough=0.4, metal=0.5)

    # 顶灯（黑色基座 + 黄色 TAXI 字面）
    sign_base = make_box('SignBase', (0.025, 0.008, 0.018),
                         (0, 0.025 + 0.025 + 0.045 + 0.004, 0))
    apply_pbr(sign_base, ROOF_SIGN, rough=0.4, metal=0.3)
    sign_face = make_box('SignFace', (0.022, 0.005, 0.015),
                         (0, 0.025 + 0.025 + 0.045 + 0.009, 0))
    apply_pbr(sign_face, ROOF_SIGN_FACE, rough=0.3, metal=0.2,
              emissive=ROOF_SIGN_FACE, emissive_intensity=0.6)

    # 前挡风
    windshield = make_box('Windshield', (0.005, 0.018, 0.038),
                          (0.045, 0.045, 0), rot=(0, 0, 0.43))
    apply_pbr(windshield, WINDOW, rough=0.2, metal=0.0)

    # 大灯 ×2
    front_lamps = []
    for sz in (1, -1):
        fl = make_box(f'FrontLamp_{sz}', (0.004, 0.006, 0.01),
                      (0.05, 0.025 + 0.005, sz * 0.018))
        apply_pbr(fl, LAMP_FRONT, rough=0.3, metal=0.0,
                  emissive=LAMP_FRONT, emissive_intensity=1.0)
        front_lamps.append(fl)

    # 尾灯 ×2
    rear_lamps = []
    for sz in (1, -1):
        rl = make_box(f'RearLamp_{sz}', (0.004, 0.006, 0.01),
                      (-0.05, 0.025 + 0.005, sz * 0.018))
        apply_pbr(rl, LAMP_REAR, rough=0.3, metal=0.0,
                  emissive=LAMP_REAR, emissive_intensity=0.8)
        rear_lamps.append(rl)

    # 车轮 ×4
    wheels = []
    for i, (x, z) in enumerate([(0.035, 0.025), (0.035, -0.025),
                                 (-0.035, 0.025), (-0.035, -0.025)]):
        tire = make_cylinder(f'Tire_{i}', 0.012, 0.012, 0.008, 12,
                             (x, 0.015, z), rot=(1.5708, 0, 0))
        apply_pbr(tire, TIRE, rough=0.85, metal=0.0)
        wheels.append(tire)

    all_objs = [body, roof, sign_base, sign_face, windshield] + front_lamps + rear_lamps + wheels
    return join_objects(all_objs, 'VehicleTaxi')


if __name__ == '__main__':
    build_vehicle_taxi()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/vehicle_taxi.glb'
    export_glb(out_path)
