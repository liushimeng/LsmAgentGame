#!/usr/bin/env python3
"""
build_vehicle_sedan — 轿车（sedan）Blender headless 导出脚本（19-Blender3D模型集成）。

原组件 ClientWeb/src/components/wealth/props/Vehicle.tsx（行 184-242）：
  - 4 车型之一（sedan/truck/bus/taxi），variant='sedan'
  - 简化几何：车身 + 4 车轮 + 前后挡风 + 灯 + 后视镜 + 雨刮
  - 全城高度 y=0.02 贴路面

GLB pivot 落 (0, 0, 0) 在车轮下沿中点；调用方 <Model position={...} rotation={...}> 即贴位。
约定：x=车长方向, y=上, z=车宽方向（与原组件 Vehicle.tsx 坐标系一致）。
"""
import bpy
import sys
import os
_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (
    reset_scene, set_unit_meters,
    make_box, make_cylinder, make_sphere, apply_pbr, join_objects, export_glb,
)

# 颜色（与原组件 default variant 同款）
SEDAN_BODY = '#3a5a8a'
SEDAN_ROOF = '#2c4a72'
WINDOW = '#1a2a3a'
LAMP_FRONT = '#fff8e0'
LAMP_REAR = '#cc0000'
TIRE = '#1a1a1a'
HUB = '#888888'


def build_vehicle_sedan() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 车身（0.10 长 0.045 高 0.045 宽 ≈ 1m × 0.45m × 0.45m）
    body = make_box('Body', (0.10, 0.025, 0.045), (0, 0.025 + 0.02, 0))
    apply_pbr(body, SEDAN_BODY, rough=0.4, metal=0.5)

    # 车顶（稍小，前后错位）
    roof = make_box('Roof', (0.06, 0.020, 0.04), (0, 0.025 + 0.025 + 0.02, 0))
    apply_pbr(roof, SEDAN_ROOF, rough=0.4, metal=0.5)

    # 前挡风（y = 0.04, x = +0.04, 后倾 25°）
    windshield = make_box('Windshield', (0.005, 0.018, 0.038),
                          (0.045, 0.045, 0), rot=(0, 0, 0.43))
    apply_pbr(windshield, WINDOW, rough=0.2, metal=0.0)

    # 后挡风
    rear_windshield = make_box('RearWindshield', (0.005, 0.018, 0.038),
                               (-0.045, 0.045, 0), rot=(0, 0, -0.43))
    apply_pbr(rear_windshield, WINDOW, rough=0.2, metal=0.0)

    # 侧窗 ×2
    side_windows = []
    for sz in (1, -1):
        sw = make_box(f'SideWindow_{sz}', (0.07, 0.012, 0.003),
                      (0, 0.055, sz * 0.021))
        apply_pbr(sw, WINDOW, rough=0.2, metal=0.0)
        side_windows.append(sw)

    # 大灯 ×2（前）
    front_lamps = []
    for sz in (1, -1):
        fl = make_box(f'FrontLamp_{sz}', (0.004, 0.006, 0.01),
                      (0.05, 0.025 + 0.005, sz * 0.018))
        apply_pbr(fl, LAMP_FRONT, rough=0.3, metal=0.0,
                  emissive=LAMP_FRONT, emissive_intensity=1.0)
        front_lamps.append(fl)

    # 尾灯 ×2（后）
    rear_lamps = []
    for sz in (1, -1):
        rl = make_box(f'RearLamp_{sz}', (0.004, 0.006, 0.01),
                      (-0.05, 0.025 + 0.005, sz * 0.018))
        apply_pbr(rl, LAMP_REAR, rough=0.3, metal=0.0,
                  emissive=LAMP_REAR, emissive_intensity=0.8)
        rear_lamps.append(rl)

    # 车轮 ×4（前后轴）
    wheels = []
    wheel_positions = [
        (0.035, 0.015, 0.025),   # FR
        (0.035, 0.015, -0.025),  # FL
        (-0.035, 0.015, 0.025),  # RR
        (-0.035, 0.015, -0.025), # RL
    ]
    for i, (x, y, z) in enumerate(wheel_positions):
        # 车轮：用 cylinder，axis=z（沿车宽）
        tire = make_cylinder(f'Tire_{i}', 0.012, 0.012, 0.008, 12,
                             (x, y, z), rot=(1.5708, 0, 0))  # 90° 让 cylinder 横放
        apply_pbr(tire, TIRE, rough=0.85, metal=0.0)
        wheels.append(tire)
        # 轮毂（外侧）
        hub = make_cylinder(f'Hub_{i}', 0.005, 0.005, 0.009, 8,
                            (x, y, z + (0.005 if z > 0 else -0.005)),
                            rot=(1.5708, 0, 0))
        apply_pbr(hub, HUB, rough=0.3, metal=0.7)
        wheels.append(hub)

    all_objs = [body, roof, windshield, rear_windshield] + side_windows + front_lamps + rear_lamps + wheels
    return join_objects(all_objs, 'VehicleSedan')


if __name__ == '__main__':
    build_vehicle_sedan()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/vehicle_sedan.glb'
    export_glb(out_path)
