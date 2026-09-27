#!/usr/bin/env python3
"""
build_vehicle_taxi — 出租（taxi）Blender headless 导出脚本（19-Blender3D模型集成）。

对应组件 ClientWeb/src/components/virtualCity/props/Vehicle.tsx（变体 'taxi'）。

坐标与尺度规约（2026-09-27 批次 19 GLB 轴向/尺度回溯修正，全目录统一）：
  - **Blender 原生 Z-up**：x = 车长，y = 车宽，z = 高度（地面 z = 0）；
    导出后 glTF X = 车长、glTF Y = 车高（minY = 0）、glTF Z = 车宽。
  - 世界单位：1 单位 = 10 m；0.47 = 真实 4.7 m。旧版按作者自定 Y-up 摆放（导出即侧躺）
    且为 1 m 玩具尺度，本次一并修正（详见 build_vehicle_sedan.py 的长注释）。
  - 车轮：cylinder 绕 X 轴 90° ⇒ 轮轴 = Y（车宽），轮面竖直。

目标包围盒（世界单位）：X 0.470 × Z 0.185 × Y 0.150，轮底 minY = 0，X/Z 居中。
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

BODY = '#e8c020'      # 出租车经典黄
BODY_DARK = '#a08818'
ROOF_SIGN = '#222222'
ROOF_SIGN_FACE = '#ffd700'
WINDOW = '#1a2a3a'
TIRE = '#1a1a1a'
LAMP_FRONT = '#fff8e0'
LAMP_REAR = '#cc0000'

# ── 尺寸（世界单位）──────────────────────────────────────────────────────
L, W, H = 0.470, 0.185, 0.150
WHEEL_R, WHEEL_W = 0.035, 0.020
WHEEL_X = 0.32 * L
BODY_L, BODY_W, BODY_Z0, BODY_Z1 = 0.450, 0.152, 0.008, 0.090
CABIN_L, CABIN_W, CABIN_Z1 = 0.245, 0.132, 0.138
WINDSHIELD_TILT = math.radians(25)


def build_vehicle_taxi() -> bpy.types.Object:
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
        hub = make_cylinder(f'Hub_{i}', 0.012, 0.012, 0.006, 12,
                            (x, sy * (W / 2 - 0.003), WHEEL_R),
                            rot=(math.pi / 2, 0, 0))
        apply_pbr(hub, '#888888', rough=0.3, metal=0.7)
        wheels.append(hub)

    # 车身
    body = make_box('Body', (BODY_L, BODY_W, BODY_Z1 - BODY_Z0),
                    (0, 0, (BODY_Z0 + BODY_Z1) / 2))
    apply_pbr(body, BODY, rough=0.4, metal=0.5)

    # 车顶
    roof = make_box('Roof', (CABIN_L, CABIN_W, CABIN_Z1 - BODY_Z1),
                    (0, 0, (BODY_Z1 + CABIN_Z1) / 2))
    apply_pbr(roof, BODY_DARK, rough=0.4, metal=0.5)

    # 顶灯（黑色基座 + 黄色 TAXI 字面；顶面 = H）
    sign_base = make_box('SignBase', (0.052, 0.030, 0.006),
                         (0, 0, CABIN_Z1 + 0.003))
    apply_pbr(sign_base, ROOF_SIGN, rough=0.4, metal=0.3)
    sign_face = make_box('SignFace', (0.046, 0.024, 0.006),
                         (0, 0, CABIN_Z1 + 0.009))      # 顶面 = H = 0.150
    apply_pbr(sign_face, ROOF_SIGN_FACE, rough=0.3, metal=0.2,
              emissive=ROOF_SIGN_FACE, emissive_intensity=0.6)

    # 前挡风（薄板绕 Y 轴后倾 25°）
    windshield = make_box('Windshield', (0.010, 0.124, 0.072),
                          (0.113, 0, 0.108), rot=(0, -WINDSHIELD_TILT, 0))
    apply_pbr(windshield, WINDOW, rough=0.2, metal=0.0)

    # 侧窗 ×2
    side_windows = []
    for sy in (1, -1):
        sw = make_box(f'SideWindow_{sy}', (0.150, 0.006, 0.030),
                      (-0.020, sy * (CABIN_W / 2 + 0.0015), 0.118))
        apply_pbr(sw, WINDOW, rough=0.2, metal=0.0)
        side_windows.append(sw)

    # 大灯 ×2 / 尾灯 ×2（外端贴 x = ±L/2）
    lamps = []
    for name, tx, color, inten in (('FrontLamp', 1, LAMP_FRONT, 1.0),
                                   ('RearLamp', -1, LAMP_REAR, 0.8)):
        for sy in (1, -1):
            lp = make_box(f'{name}_{sy}', (0.008, 0.030, 0.016),
                          (tx * (L / 2 - 0.004), sy * 0.045, 0.055))
            apply_pbr(lp, color, rough=0.3, metal=0.0,
                      emissive=color, emissive_intensity=inten)
            lamps.append(lp)

    all_objs = [body, roof, sign_base, sign_face, windshield] + side_windows + lamps + wheels
    return join_objects(all_objs, 'VehicleTaxi')


if __name__ == '__main__':
    obj = build_vehicle_taxi()
    # 烘焙 join 残留 object transform ⇒ 导出节点 identity（<Model> 零旋转零 scale 直挂）
    # 注：__common__.export_glb 自 2026-09-27 起亦统一 bake_transforms()（同一目的）；
    # 此处显式保留，既让脚本自证，也防 __common__ 行为回退时静默复发。
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/vehicle_taxi.glb'
    export_glb(out_path)
