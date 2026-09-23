#!/usr/bin/env python3
"""
build_vehicle_bus — 公交（bus）Blender headless 导出脚本（19-Blender3D模型集成）。

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

BODY = '#2a8c4a'
BODY_DARK = '#1a5a30'
WINDOW = '#1a2a3a'
WINDOW_FRAME = '#dadada'
TIRE = '#1a1a1a'
LAMP_FRONT = '#fff8e0'
LAMP_REAR = '#cc0000'
DOOR = '#dadada'


def build_vehicle_bus() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 车身（长 0.20, 高 0.06, 宽 0.05；y=0.025..0.085）
    body = make_box('Body', (0.20, 0.06, 0.05), (0, 0.025 + 0.06 / 2, 0))
    apply_pbr(body, BODY, rough=0.5, metal=0.3)

    # 8 个窗（沿车长均布，x ∈ -0.08..+0.08 间隔 0.022）
    windows = []
    for i, x in enumerate([-0.07, -0.05, -0.03, -0.01, 0.01, 0.03, 0.05, 0.07]):
        for sz in (1, -1):
            win = make_box(f'Win_{i}_{sz}', (0.018, 0.025, 0.005),
                           (x, 0.055, sz * 0.0255))
            apply_pbr(win, WINDOW, rough=0.2, metal=0.0)
            windows.append(win)

    # 前门（x=+0.09, z=+0.025 朝右）
    front_door = make_box('FrontDoor', (0.008, 0.045, 0.012),
                          (0.09, 0.05, 0.027))
    apply_pbr(front_door, DOOR, rough=0.5, metal=0.3)

    # 后门（x=-0.09）
    rear_door = make_box('RearDoor', (0.008, 0.045, 0.012),
                         (-0.09, 0.05, 0.027))
    apply_pbr(rear_door, DOOR, rough=0.5, metal=0.3)

    # 大灯 ×2
    front_lamps = []
    for sz in (1, -1):
        fl = make_box(f'FrontLamp_{sz}', (0.004, 0.008, 0.012),
                      (0.105, 0.035, sz * 0.020))
        apply_pbr(fl, LAMP_FRONT, rough=0.3, metal=0.0,
                  emissive=LAMP_FRONT, emissive_intensity=1.0)
        front_lamps.append(fl)

    # 尾灯 ×2
    rear_lamps = []
    for sz in (1, -1):
        rl = make_box(f'RearLamp_{sz}', (0.004, 0.008, 0.012),
                      (-0.105, 0.035, sz * 0.020))
        apply_pbr(rl, LAMP_REAR, rough=0.3, metal=0.0,
                  emissive=LAMP_REAR, emissive_intensity=0.8)
        rear_lamps.append(rl)

    # 车轮 ×4（前 + 后）
    wheels = []
    for i, (x, z) in enumerate([(0.075, 0.025), (0.075, -0.025),
                                 (-0.075, 0.025), (-0.075, -0.025)]):
        tire = make_cylinder(f'Tire_{i}', 0.013, 0.013, 0.008, 12,
                             (x, 0.015, z), rot=(1.5708, 0, 0))
        apply_pbr(tire, TIRE, rough=0.85, metal=0.0)
        wheels.append(tire)

    all_objs = [body, front_door, rear_door] + windows + front_lamps + rear_lamps + wheels
    return join_objects(all_objs, 'VehicleBus')


if __name__ == '__main__':
    build_vehicle_bus()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/vehicle_bus.glb'
    export_glb(out_path)
