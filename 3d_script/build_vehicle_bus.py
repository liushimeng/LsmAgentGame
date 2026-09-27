#!/usr/bin/env python3
"""
build_vehicle_bus — 公交（bus）Blender headless 导出脚本（19-Blender3D模型集成）。

对应组件 ClientWeb/src/components/virtualCity/props/Vehicle.tsx（变体 'bus'）。
真实公交尺度 12 m × 2.55 m × 3.2 m（世界单位 1.200 × 0.255 × 0.320）。

坐标与尺度规约（2026-09-27 批次 19 GLB 轴向/尺度回溯修正，全目录统一）：
  - **Blender 原生 Z-up**：x = 车长，y = 车宽，z = 高度（地面 z = 0）；
    导出后 glTF X = 车长、glTF Y = 车高（minY = 0）、glTF Z = 车宽。
  - 世界单位：1 单位 = 10 m。旧版长仅 2.14 m（应 12 m）+ 侧躺 + 半埋，本次一并修正。
  - 车轮：cylinder 绕 X 轴 90° ⇒ 轮轴 = Y（车宽），轮面竖直。

目标包围盒（世界单位）：X 1.200 × Z 0.255 × Y 0.320，轮底 minY = 0，X/Z 居中。
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

BODY = '#2a8c4a'
BODY_DARK = '#1a5a30'
WINDOW = '#1a2a3a'
TIRE = '#1a1a1a'
LAMP_FRONT = '#fff8e0'
LAMP_REAR = '#cc0000'
DOOR = '#dadada'

# ── 尺寸（世界单位）──────────────────────────────────────────────────────
L, W, H = 1.200, 0.255, 0.320
WHEEL_R, WHEEL_W = 0.050, 0.028
WHEEL_X = 0.32 * L                     # ±0.384（前后轴）
BODY_L, BODY_W, BODY_Z0, BODY_Z1 = 1.170, 0.215, 0.085, 0.300
ROOF_L, ROOF_W = 1.160, 0.205
WIN_Z, WIN_L, WIN_H = 0.215, 0.085, 0.070


def build_vehicle_bus() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 车轮 ×4（轴沿车宽 Y，轮底 z = 0）
    wheels = []
    for i, (x, sy) in enumerate([(WHEEL_X, 1), (WHEEL_X, -1),
                                 (-WHEEL_X, 1), (-WHEEL_X, -1)]):
        tire = make_cylinder(f'Tire_{i}', WHEEL_R, WHEEL_R, WHEEL_W, 16,
                             (x, sy * (W / 2 - WHEEL_W / 2), WHEEL_R),
                             rot=(math.pi / 2, 0, 0))
        apply_pbr(tire, TIRE, rough=0.85, metal=0.0)
        wheels.append(tire)
        hub = make_cylinder(f'Hub_{i}', 0.016, 0.016, 0.008, 12,
                            (x, sy * (W / 2 - 0.004), WHEEL_R),
                            rot=(math.pi / 2, 0, 0))
        apply_pbr(hub, '#888888', rough=0.3, metal=0.7)
        wheels.append(hub)

    # 车身
    body = make_box('Body', (BODY_L, BODY_W, BODY_Z1 - BODY_Z0),
                    (0, 0, (BODY_Z0 + BODY_Z1) / 2))
    apply_pbr(body, BODY, rough=0.5, metal=0.3)

    # 车顶盖（顶面 = H）
    roof = make_box('Roof', (ROOF_L, ROOF_W, H - BODY_Z1),
                    (0, 0, (BODY_Z1 + H) / 2))
    apply_pbr(roof, BODY_DARK, rough=0.5, metal=0.3)

    # 8 扇侧窗（沿车长均布，两侧对称）
    windows = []
    for i, x in enumerate([-0.42, -0.30, -0.18, -0.06, 0.06, 0.18, 0.30, 0.42]):
        for sy in (1, -1):
            win = make_box(f'Win_{i}_{sy}', (WIN_L, 0.006, WIN_H),
                           (x, sy * (BODY_W / 2 + 0.003), WIN_Z))
            apply_pbr(win, WINDOW, rough=0.2, metal=0.0)
            windows.append(win)

    # 前门 / 后门（同一侧）
    doors = []
    for i, x in enumerate((0.50, -0.50)):
        door = make_box(f'Door_{i}', (0.022, 0.006, 0.130),
                        (x, BODY_W / 2 + 0.003, 0.150))
        apply_pbr(door, DOOR, rough=0.5, metal=0.3)
        doors.append(door)

    # 大灯 ×2 / 尾灯 ×2（外端贴 x = ±L/2）
    lamps = []
    for name, tx, color, inten in (('FrontLamp', 1, LAMP_FRONT, 1.0),
                                   ('RearLamp', -1, LAMP_REAR, 0.8)):
        for sy in (1, -1):
            lp = make_box(f'{name}_{sy}', (0.006, 0.030, 0.020),
                          (tx * (L / 2 - 0.003), sy * 0.045, 0.110))
            apply_pbr(lp, color, rough=0.3, metal=0.0,
                      emissive=color, emissive_intensity=inten)
            lamps.append(lp)

    all_objs = [body, roof] + windows + doors + lamps + wheels
    return join_objects(all_objs, 'VehicleBus')


if __name__ == '__main__':
    obj = build_vehicle_bus()
    # 烘焙 join 残留 object transform ⇒ 导出节点 identity（<Model> 零旋转零 scale 直挂）
    # 注：__common__.export_glb 自 2026-09-27 起亦统一 bake_transforms()（同一目的）；
    # 此处显式保留，既让脚本自证，也防 __common__ 行为回退时静默复发。
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/vehicle_bus.glb'
    export_glb(out_path)
