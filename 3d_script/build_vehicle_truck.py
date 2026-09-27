#!/usr/bin/env python3
"""
build_vehicle_truck — 卡车（truck）Blender headless 导出脚本（19-Blender3D模型集成）。

对应组件 ClientWeb/src/components/virtualCity/props/Vehicle.tsx（变体 'truck'）。
真实货卡尺度 8.5 m × 2.5 m × 3.4 m（世界单位 0.850 × 0.250 × 0.340）。

坐标与尺度规约（2026-09-27 批次 19 GLB 轴向/尺度回溯修正，全目录统一）：
  - **Blender 原生 Z-up**：x = 车长，y = 车宽，z = 高度（地面 z = 0）；+X = 车头。
    导出后 glTF X = 车长、glTF Y = 车高（minY = 0）、glTF Z = 车宽。
  - 世界单位：1 单位 = 10 m。旧版长仅 2.24 m（应 8.5 m）+ 侧躺 + 半埋，本次一并修正。
  - 车轮：cylinder 绕 X 轴 90° ⇒ 轮轴 = Y（车宽），轮面竖直；6 轮（前轴 + 双后轴）。

目标包围盒（世界单位）：X 0.850 × Z 0.250 × Y 0.340，轮底 minY = 0，X/Z 居中。
"""
import bpy
import math
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
CARGO = '#dadada'
WINDOW = '#1a2a3a'
TIRE = '#1a1a1a'
HUB = '#888888'
LAMP_FRONT = '#fff8e0'
LAMP_REAR = '#cc0000'

# ── 尺寸（世界单位）──────────────────────────────────────────────────────
L, W, H = 0.850, 0.250, 0.340
WHEEL_R, WHEEL_W = 0.0525, 0.028
CARGO_L, CARGO_W, CARGO_Z0, CARGO_Z1 = 0.480, 0.210, 0.090, 0.340   # 货厢（顶面 = H）
CAB_L, CAB_W, CAB_Z1 = 0.330, 0.230, 0.290                         # 驾驶室
# 车头朝 +X：货厢 x ∈ [-0.425, 0.055]，驾驶室 x ∈ [0.055, 0.385]，保险杠补到 +0.425
CARGO_X = -0.185
CAB_X = 0.220
BUMPER_X = 0.405


def build_vehicle_truck() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 车轮 ×6（前轴 + 双后轴；轴沿车宽 Y，轮底 z = 0）
    wheels = []
    for i, (x, sy) in enumerate([(0.280, 1), (0.280, -1),
                                 (-0.200, 1), (-0.200, -1),
                                 (-0.290, 1), (-0.290, -1)]):
        tire = make_cylinder(f'Tire_{i}', WHEEL_R, WHEEL_R, WHEEL_W, 16,
                             (x, sy * (W / 2 - WHEEL_W / 2), WHEEL_R),
                             rot=(math.pi / 2, 0, 0))
        apply_pbr(tire, TIRE, rough=0.85, metal=0.0)
        wheels.append(tire)
        hub = make_cylinder(f'Hub_{i}', 0.016, 0.016, 0.008, 12,
                            (x, sy * (W / 2 - 0.004), WHEEL_R),
                            rot=(math.pi / 2, 0, 0))
        apply_pbr(hub, HUB, rough=0.3, metal=0.7)
        wheels.append(hub)

    # 货厢（顶面 = H）
    cargo = make_box('Cargo', (CARGO_L, CARGO_W, CARGO_Z1 - CARGO_Z0),
                     (CARGO_X, 0, (CARGO_Z0 + CARGO_Z1) / 2))
    apply_pbr(cargo, CARGO, rough=0.7, metal=0.1)

    # 驾驶室
    cab = make_box('Cab', (CAB_L, CAB_W, CAB_Z1 - CARGO_Z0),
                   (CAB_X, 0, (CARGO_Z0 + CAB_Z1) / 2))
    apply_pbr(cab, CAB, rough=0.4, metal=0.5)

    # 驾驶室前风挡（薄板绕 Y 轴后倾 15°）
    cab_glass = make_box('CabGlass', (0.010, 0.200, 0.130),
                         (0.360, 0, 0.240), rot=(0, -math.radians(15), 0))
    apply_pbr(cab_glass, WINDOW, rough=0.2, metal=0.0)

    # 前保险杠（补足车长到 ±L/2）
    bumper = make_box('Bumper', (0.040, 0.220, 0.050), (BUMPER_X, 0, 0.115))
    apply_pbr(bumper, '#3a3a3a', rough=0.7, metal=0.4)

    # 大灯 ×2（保险杠面）+ 尾灯 ×2（货厢后端面）
    lamps = []
    for sy in (1, -1):
        fl = make_box(f'FrontLamp_{sy}', (0.014, 0.030, 0.020),
                      (0.418, sy * 0.075, 0.160))
        apply_pbr(fl, LAMP_FRONT, rough=0.3, metal=0.0,
                  emissive=LAMP_FRONT, emissive_intensity=1.0)
        lamps.append(fl)
        rl = make_box(f'RearLamp_{sy}', (0.006, 0.030, 0.020),
                      (-(L / 2 - 0.003), sy * 0.080, 0.180))
        apply_pbr(rl, LAMP_REAR, rough=0.3, metal=0.0,
                  emissive=LAMP_REAR, emissive_intensity=0.8)
        lamps.append(rl)

    all_objs = [cargo, cab, cab_glass, bumper] + lamps + wheels
    return join_objects(all_objs, 'VehicleTruck')


if __name__ == '__main__':
    obj = build_vehicle_truck()
    # 烘焙 join 残留 object transform ⇒ 导出节点 identity（<Model> 零旋转零 scale 直挂）
    # 注：__common__.export_glb 自 2026-09-27 起亦统一 bake_transforms()（同一目的）；
    # 此处显式保留，既让脚本自证，也防 __common__ 行为回退时静默复发。
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/vehicle_truck.glb'
    export_glb(out_path)
