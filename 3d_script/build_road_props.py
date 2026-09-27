#!/usr/bin/env python3
"""
build_road_props — 路面道具 StreetLight A/B/C（road_props.glb）Blender 导出脚本。

GLB 内合并 3 种路灯变体，对象名分别为 StreetLight_A / StreetLight_B / StreetLight_C。
调用方 <Model url={...}> 后通过 scene.getObjectByName 选择对应实例
（截止批次 28 尚无组件引用 —— 批次 24 文档登记的 §130「声明了却从不接线」遗留债务，
本批次只保证资产本身正确，不为它接线）。

原组件 ClientWeb/src/components/virtualCity/Road.tsx 的 <StreetLight variant=...>：
  - variant 'a' / 'b' / 'c' 分别对应 StreetLight_A/B/C
  - 3 种灯型排开在 x = -1.5 / 0 / +1.5，调用方按位置挑

坐标与尺度规约（2026-09-27 批次 19 GLB 轴向/尺度回溯修正，全目录统一）：
  - **Blender 原生 Z-up**：x = 沿路方向排布，y = 灯臂伸出方向（-Y = 路心侧），
    z = 高度（地面 z = 0）。导出后 glTF X = 排布方向、glTF Y = 杆高（minY = 0）、
    glTF Z = 灯臂伸出方向。
  - 世界单位：1 单位 = 10 m。旧版把杆高写在位置向量第 1 位（作者自定 Y-up），
    导出后 3 根杆全部横躺且半埋，本次修正。

验收（世界单位）：3 盏沿 X 总跨 3.130（A 球罩 -1.56 ↔ C 方罩 +1.57），
B 型杆 + 横臂顶面 = 1.200（目标总高），每盏杆竖直、minY = 0。
"""
import bpy
import sys
import os
_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (
    reset_scene, set_unit_meters,
    make_box, make_cylinder, make_cone, make_sphere, apply_pbr, join_objects, export_glb,
)

POLE = '#3a3a3a'
LIGHT = '#dcdcdc'
WARM_LAMP = '#ffd9a0'
LENS = '#ffe9b0'

POLE_R = 0.025
ARM_L, ARM_T = 0.400, 0.025
H_MAX = 1.200          # B 型总高 = 目标 Y
X_A, X_B, X_C = -1.500, 0.0, 1.500


def build_one_streetlight(name: str, height: float, head_kind: str, x_offset: float):
    """单盏路灯（Blender Z-up，灯臂朝 -Y，杆底 z = 0）；返回 list[obj]。"""
    objs = []
    # 主杆（顶面 = height）
    pole = make_cylinder(f'{name}_Pole', POLE_R, POLE_R, height, 10,
                         (x_offset, 0, height / 2))
    apply_pbr(pole, POLE, rough=0.7, metal=0.6)
    objs.append(pole)

    # 横臂（沿 -Y 伸出，顶面与杆顶齐平）
    arm_center_y = -ARM_L / 2
    arm = make_box(f'{name}_Arm', (ARM_T, ARM_L, ARM_T),
                   (x_offset, arm_center_y, height - ARM_T / 2))
    apply_pbr(arm, POLE, rough=0.7, metal=0.6)
    objs.append(arm)

    # 灯头（按 head_kind 区分；灯头外端面 = x_offset ± 半宽，A/C 定义整体 X 跨度）
    head_y = -ARM_L
    if head_kind == 'a':
        # A: 球形灯罩（r 0.06 ⇒ 外缘 -1.56）
        head = make_sphere(f'{name}_Head', 0.060, 12, (x_offset, head_y, height - 0.040))
        apply_pbr(head, LIGHT, rough=0.5, metal=0.1)
    elif head_kind == 'b':
        # B: 锥形灯罩（top = height）
        head = make_cone(f'{name}_Head', r=0.080, h=0.120, segs=10,
                         pos=(x_offset, head_y, height - 0.060))
        apply_pbr(head, LIGHT, rough=0.5, metal=0.1)
    else:  # c
        # C: 方形灯罩 + 反光板（半宽 0.07 ⇒ 外缘 +1.57）
        head = make_box(f'{name}_Head', (0.140, 0.040, 0.100),
                        (x_offset, head_y, height - 0.020))
        apply_pbr(head, LIGHT, rough=0.5, metal=0.1)
        reflector = make_box(f'{name}_Reflector', (0.120, 0.040, 0.080),
                             (x_offset, head_y - 0.020, height - 0.040))
        apply_pbr(reflector, LENS, rough=0.3, metal=0.4)
        objs.append(reflector)

    # 灯芯（暖光，emissive）
    lamp = make_sphere(f'{name}_Lamp', 0.030, 10, (x_offset, head_y, height - 0.040))
    apply_pbr(lamp, WARM_LAMP, rough=0.4, metal=0.0,
              emissive=WARM_LAMP, emissive_intensity=2.0)
    objs.append(head)
    objs.append(lamp)
    return objs


def build_road_props() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    all_objs = []
    # 3 种路灯分别放 x = -1.5, 0, +1.5（沿 X 总跨 3.13）
    all_objs += build_one_streetlight('StreetLight_A', height=1.000, head_kind='a', x_offset=X_A)
    all_objs += build_one_streetlight('StreetLight_B', height=H_MAX, head_kind='b', x_offset=X_B)
    all_objs += build_one_streetlight('StreetLight_C', height=0.900, head_kind='c', x_offset=X_C)

    return join_objects(all_objs, 'RoadProps')


if __name__ == '__main__':
    obj = build_road_props()
    # 烘焙 join 残留 object transform ⇒ 导出节点 identity（<Model> 零旋转零 scale 直挂）
    # 注：__common__.export_glb 自 2026-09-27 起亦统一 bake_transforms()（同一目的）；
    # 此处显式保留，既让脚本自证，也防 __common__ 行为回退时静默复发。
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/road_props.glb'
    export_glb(out_path)
