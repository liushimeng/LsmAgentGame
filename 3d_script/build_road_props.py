#!/usr/bin/env python3
"""
build_road_props — 路面道具 StreetLight A/B/C（road_props.glb）Blender 导出脚本。

GLB 内合并 3 种路灯变体，对象名分别为 StreetLight_A / StreetLight_B / StreetLight_C。
调用方 <Model url={...}> 后通过 scene.getObjectByName 选择对应实例。

原组件 ClientWeb/src/components/wealth/Road.tsx 已用 <StreetLight variant=...>：
  - variant 'a' / 'b' / 'c' 分别对应原 StreetLight_A/B/C
  - road_props.glb 内 3 种 mesh 排开在 x=0/+1.5/-1.5，调用方按位置挑

3 种灯型差异：高度 + 灯头形状 + 是否带反光板。
"""
import bpy
import sys
import os
import math
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


def build_one_streetlight(name: str, height: float, head_kind: str, x_offset: float):
    """单盏路灯；返回 list[obj]。"""
    objs = []
    # 主杆
    pole = make_cylinder(f'{name}_Pole', 0.025, 0.025, height, 8,
                         (x_offset, height / 2, 0))
    apply_pbr(pole, POLE, rough=0.7, metal=0.6)
    objs.append(pole)

    # 横臂（长 0.4，向 +z）
    arm = make_box(f'{name}_Arm', (0.025, 0.025, 0.4),
                   (x_offset, height, 0.2))
    apply_pbr(arm, POLE, rough=0.7, metal=0.6)
    objs.append(arm)

    # 灯头（按 head_kind 区分）
    if head_kind == 'a':
        # A: 球形灯罩
        head = make_sphere(f'{name}_Head', 0.06, 12, (x_offset, height - 0.04, 0.4))
        apply_pbr(head, LIGHT, rough=0.5, metal=0.1)
    elif head_kind == 'b':
        # B: 锥形灯罩
        head = make_cone(f'{name}_Head', r=0.08, h=0.12, segs=8,
                         pos=(x_offset, height - 0.06, 0.4))
        apply_pbr(head, LIGHT, rough=0.5, metal=0.1)
    else:  # c
        # C: 方形带反光板
        head = make_box(f'{name}_Head', (0.14, 0.04, 0.1),
                        (x_offset, height - 0.02, 0.4))
        apply_pbr(head, LIGHT, rough=0.5, metal=0.1)
        reflector = make_box(f'{name}_Reflector', (0.12, 0.04, 0.08),
                             (x_offset, height - 0.04, 0.42))
        apply_pbr(reflector, LENS, rough=0.3, metal=0.4)
        objs.append(reflector)

    # 灯心（暖光，emissive）
    lamp = make_sphere(f'{name}_Lamp', 0.03, 8, (x_offset, height - 0.04, 0.4))
    apply_pbr(lamp, WARM_LAMP, rough=0.4, metal=0.0,
              emissive=WARM_LAMP, emissive_intensity=2.0)
    objs.append(head)
    objs.append(lamp)

    return objs


def build_road_props() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    all_objs = []
    # 3 种路灯分别放 x=-1.5, 0, +1.5
    all_objs += build_one_streetlight('StreetLight_A', height=1.0, head_kind='a', x_offset=-1.5)
    all_objs += build_one_streetlight('StreetLight_B', height=1.2, head_kind='b', x_offset=0.0)
    all_objs += build_one_streetlight('StreetLight_C', height=0.9, head_kind='c', x_offset=1.5)

    return join_objects(all_objs, 'RoadProps')


if __name__ == '__main__':
    build_road_props()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/road_props.glb'
    export_glb(out_path)
