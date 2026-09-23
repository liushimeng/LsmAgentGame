#!/usr/bin/env python3
"""
build_oak_tree — 橡树（oak_tree）Blender headless 导出脚本（19-Blender3D模型集成）。

约定：y=上；pivot 在 (0, 0, 0) 地面中心。
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
    make_box, make_cylinder, make_sphere, apply_pbr, join_objects, export_glb,
)

TRUNK = '#5a4634'
CROWN = '#3a8a45'
CROWN_DARK = '#2f7a3a'
CROWN_LIGHT = '#4a9a55'


def build_oak_tree() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # 主干（高 0.5m, 半径 0.04m）
    trunk = make_cylinder('Trunk', 0.04, 0.05, 0.5, 12, (0, 0.25, 0))
    apply_pbr(trunk, TRUNK, rough=0.95, metal=0.0)

    # 2 分枝（box，简化视觉）
    branches = []
    for i, (rot_z, off) in enumerate([(0.5, (0.04, 0.45, 0.0)),
                                       (-0.4, (-0.04, 0.45, 0.02))]):
        branch = make_box(f'Branch_{i}', (0.03, 0.03, 0.18),
                          off, rot=(0, 0, rot_z))
        apply_pbr(branch, TRUNK, rough=0.95, metal=0.0)
        branches.append(branch)

    # 3 球错落树冠
    crowns = []
    crown_specs = [
        ('Crown_0', 0.18, (0.04, 0.65, 0.0), CROWN),
        ('Crown_1', 0.20, (-0.05, 0.78, 0.05), CROWN_DARK),
        ('Crown_2', 0.16, (0.0, 0.85, -0.06), CROWN_LIGHT),
    ]
    for name, r, pos, color in crown_specs:
        # 用 icosahedron 模拟树冠（3 级细分）
        bpy.ops.mesh.primitive_ico_sphere_add(radius=r, subdivisions=2,
                                              location=(0, 0, 0))
        obj = bpy.context.active_object
        obj.name = name
        obj.location = (pos[0], pos[1], pos[2])
        apply_pbr(obj, color, rough=0.85, metal=0.0)
        crowns.append(obj)

    all_objs = [trunk] + branches + crowns
    return join_objects(all_objs, 'OakTree')


if __name__ == '__main__':
    build_oak_tree()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/oak_tree.glb'
    export_glb(out_path)
