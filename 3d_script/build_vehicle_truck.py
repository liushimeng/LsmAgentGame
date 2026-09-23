#!/usr/bin/env python3
"""
build_vehicle_truck — 卡车（truck）Blender headless 导出脚本（19-Blender3D模型集成）。

约定：x=车长方向, y=上, z=车宽方向；y=0 在车轮下沿。
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

CAB = '#c83a2c'
CAB_DARK = '#8a2618'
CARGO = '#dadada'
WINDOW = '#1a2a3a'
TIRE = '#1a1a1a'
HUB = '#888888'
LAMP_FRONT = '#fff8e0'


def build_vehicle_truck() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 货厢（长 0.16, 高 0.06, 宽 0.05；x=0..0.16，y=0.02..0.08）
    cargo = make_box('Cargo', (0.16, 0.06, 0.05), (0.04, 0.02 + 0.06 / 2, 0))
    apply_pbr(cargo, CARGO, rough=0.7, metal=0.1)

    # 车头（驾驶室，长 0.05, 高 0.05, 宽 0.045；x=-0.05..0）
    cab = make_box('Cab', (0.05, 0.05, 0.045),
                   (-0.05 - 0.025, 0.02 + 0.05 / 2, 0))
    apply_pbr(cab, CAB, rough=0.4, metal=0.5)

    # 车头驾驶室玻璃
    cab_glass = make_box('CabGlass', (0.005, 0.03, 0.04),
                         (-0.05 - 0.045, 0.02 + 0.04, 0), rot=(0, 0, 0.5))
    apply_pbr(cab_glass, WINDOW, rough=0.2, metal=0.0)

    # 大灯 ×2
    front_lamps = []
    for sz in (1, -1):
        fl = make_box(f'FrontLamp_{sz}', (0.004, 0.008, 0.012),
                      (-0.05 - 0.049, 0.02 + 0.015, sz * 0.018))
        apply_pbr(fl, LAMP_FRONT, rough=0.3, metal=0.0,
                  emissive=LAMP_FRONT, emissive_intensity=1.0)
        front_lamps.append(fl)

    # 车轮 ×6（双后轴）
    wheels = []
    wheel_positions = [
        (0.04, 0.015, 0.025),    # 前轴右
        (0.04, 0.015, -0.025),   # 前轴左
        (-0.025, 0.015, 0.025),  # 中轴右
        (-0.025, 0.015, -0.025), # 中轴左
        (-0.06, 0.015, 0.025),   # 后轴右
        (-0.06, 0.015, -0.025),  # 后轴左
    ]
    for i, (x, y, z) in enumerate(wheel_positions):
        tire = make_cylinder(f'Tire_{i}', 0.013, 0.013, 0.008, 12,
                             (x, y, z), rot=(1.5708, 0, 0))
        apply_pbr(tire, TIRE, rough=0.85, metal=0.0)
        wheels.append(tire)

    all_objs = [cargo, cab, cab_glass] + front_lamps + wheels
    return join_objects(all_objs, 'VehicleTruck')


if __name__ == '__main__':
    build_vehicle_truck()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/vehicle_truck.glb'
    export_glb(out_path)
