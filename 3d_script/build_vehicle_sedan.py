#!/usr/bin/env python3
"""
build_vehicle_sedan — 轿车（sedan）Blender headless 导出脚本（19-Blender3D模型集成）。

对应组件 ClientWeb/src/components/virtualCity/props/Vehicle.tsx（变体 'sedan'，
fallback 程序化几何 + 车轮/灯组附件），车身尺寸对齐 VEHICLE_DIMS.sedan。

坐标与尺度规约（2026-09-27 批次 19 GLB 轴向/尺度回溯修正，全目录统一）：
  - **Blender 原生 Z-up**：x = 车长，y = 车宽，z = 高度（地面 z = 0）。
  - export_scene.gltf 的 Yup 转换 (x,y,z)_b → (x,z,-y)_g ⇒ glTF/three.js 中
    glTF X = 车长、glTF Y = 车高（minY = 0）、glTF Z = 车宽，
    与 Vehicle.tsx 的 boxGeometry args=[l, h, w] 逐轴一致，<Model> 零旋转零 scale 直挂。
  - 世界单位：1 单位 = 10 m（cityScale.METERS_PER_UNIT），故 0.46 = 真实 4.6 m 车长。
  - 历史坑（本脚本已修正）：旧版按"x=车长, y=上, z=车宽"的作者自定 Y-up 摆放，导出后
    高度落到 glTF -Z ⇒ 整车侧躺且半埋；且几何按 1 m 玩具车尺寸，比 fallback 小 4.4×。
  - 车轮：cylinder 默认轴 = Z，绕 X 转 90° 后轴 = Y（车宽）⇒ 轮面竖直；旧版漏了这层
    推导（旧脚本给的 rot 反而把轮子放平）。

目标包围盒（世界单位）：X 0.460（车长）× Z 0.182（车宽）× Y 0.145（车高），轮底 minY = 0。
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

# 颜色（与原组件 default variant 同款）
SEDAN_BODY = '#3a5a8a'
SEDAN_ROOF = '#2c4a72'
WINDOW = '#1a2a3a'
LAMP_FRONT = '#fff8e0'
LAMP_REAR = '#cc0000'
TIRE = '#1a1a1a'
HUB = '#888888'

# ── 尺寸（世界单位；1 单位 = 10 m）────────────────────────────────────────
L, W, H = 0.460, 0.182, 0.145          # 目标包围盒：车长 / 车宽 / 车高
WHEEL_R, WHEEL_W = 0.035, 0.020        # 与 fallback WHEEL_R=u(0.35) / WHEEL_W=u(0.2) 同值
WHEEL_X = 0.32 * L                     # 与 fallback wheelPositions 的 ±l*0.32 同值
BODY_L, BODY_W, BODY_Z0, BODY_Z1 = 0.440, 0.150, 0.008, 0.088
CABIN_L, CABIN_W = 0.240, 0.130
WINDSHIELD_TILT = math.radians(25)     # 前挡风后倾角（与 fallback WINDSHIELD_TILT 同值）


def build_vehicle_sedan() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 车轮 ×4（先建，便于一眼看出贴地基准）：轴沿车宽 Y，轮底 z = 0
    wheels = []
    for i, (x, sy) in enumerate([(WHEEL_X, 1), (WHEEL_X, -1),
                                 (-WHEEL_X, 1), (-WHEEL_X, -1)]):
        tire = make_cylinder(f'Tire_{i}', WHEEL_R, WHEEL_R, WHEEL_W, 16,
                             (x, sy * (W / 2 - WHEEL_W / 2), WHEEL_R),
                             rot=(math.pi / 2, 0, 0))
        apply_pbr(tire, TIRE, rough=0.85, metal=0.0)
        wheels.append(tire)
        # 轮毂（轮外侧，与轮胎外缘齐平 → 车宽 = ±W/2）
        hub = make_cylinder(f'Hub_{i}', 0.012, 0.012, 0.006, 12,
                            (x, sy * (W / 2 - 0.003), WHEEL_R),
                            rot=(math.pi / 2, 0, 0))
        apply_pbr(hub, HUB, rough=0.3, metal=0.7)
        wheels.append(hub)

    # 车身（车长 0.44，前/后保险杠补到 ±L/2）
    body = make_box('Body', (BODY_L, BODY_W, BODY_Z1 - BODY_Z0),
                    (0, 0, (BODY_Z0 + BODY_Z1) / 2))
    apply_pbr(body, SEDAN_BODY, rough=0.4, metal=0.5)

    # 车顶（前舱，顶面 = H）
    roof = make_box('Roof', (CABIN_L, CABIN_W, H - BODY_Z1),
                    (0, 0, (BODY_Z1 + H) / 2))
    apply_pbr(roof, SEDAN_ROOF, rough=0.4, metal=0.5)

    # 前挡风 / 后挡风（薄板绕 Y 轴后倾 25°；+X 车头端向后倾 = 绕 Y 负向）
    windshields = []
    for tx, ang in ((1, -WINDSHIELD_TILT), (-1, WINDSHIELD_TILT)):
        ws = make_box(f'Windshield_{"FR" if tx > 0 else "RR"}',
                      (0.010, 0.124, 0.072),
                      (tx * 0.108, 0, 0.108), rot=(0, ang, 0))
        apply_pbr(ws, WINDOW, rough=0.2, metal=0.0)
        windshields.append(ws)

    # 侧窗 ×2（贴车顶两侧面）
    side_windows = []
    for sy in (1, -1):
        sw = make_box(f'SideWindow_{sy}', (0.150, 0.006, 0.030),
                      (-0.020, sy * (CABIN_W / 2 + 0.0015), 0.118))
        apply_pbr(sw, WINDOW, rough=0.2, metal=0.0)
        side_windows.append(sw)

    # 大灯 ×2（前保险杠面 x = ±L/2）+ 尾灯 ×2
    lamps = []
    for name, tx, color, inten in (('FrontLamp', 1, LAMP_FRONT, 1.0),
                                   ('RearLamp', -1, LAMP_REAR, 0.8)):
        for sy in (1, -1):
            lp = make_box(f'{name}_{sy}', (0.008, 0.030, 0.016),
                          (tx * (L / 2 - 0.004), sy * 0.045, 0.055))
            apply_pbr(lp, color, rough=0.3, metal=0.0,
                      emissive=color, emissive_intensity=inten)
            lamps.append(lp)

    all_objs = [body, roof] + windshields + side_windows + lamps + wheels
    return join_objects(all_objs, 'VehicleSedan')


if __name__ == '__main__':
    obj = build_vehicle_sedan()
    # 烘焙 join 残留的 object transform（__common__.make_box/make_cylinder 用 obj.scale
    # 表达尺寸，join 会把首件的 scale 留在结果上）⇒ 导出节点为 identity，
    # 消费端 <Model> 零旋转零 scale 直挂（批次 26 坐标契约第 4 条）。
    # 注：__common__.export_glb 自 2026-09-27 起亦统一 bake_transforms()（同一目的）；
    # 此处显式保留，既让脚本自证，也防 __common__ 行为回退时静默复发。
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/vehicle_sedan.glb'
    export_glb(out_path)
