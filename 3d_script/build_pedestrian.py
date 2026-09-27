#!/usr/bin/env python3
"""
build_pedestrian — 行人（pedestrian_walk）Blender headless 导出脚本（19-Blender3D模型集成）。

输出：
  - .glb 内含 Armature（10 bone：root/hips/spine/chest/neck/head + 上下臂 ×2 + 上下腿 ×2）
  - 单 mesh 蒙皮（4 outfit Material Slot：body/pants/head/shoes，运行时换色）
  - walk 动画 clip：24 帧循环（摆臂摆腿反相，±30°）

坐标与尺度规约（2026-09-27 批次 19 GLB 轴向/尺度回溯修正，全目录统一）：
  - **Blender 原生 Z-up**：x = 左右，y = 前后（**-Y = 行进方向**），z = 高度（脚底 z = 0）。
    导出后 glTF X = 左右、glTF Y = 身高（脚底 minY = 0）、glTF Z = 前后（+Z = 行进方向）。
  - 世界单位：1 单位 = 10 m；身高 0.167 = 真实 1.67 m（旧版 0.86 单位且横躺，
    全城 ~100 个行人渲染成 8.6 m 长的"原木"，本次修正）。

目标包围盒（世界单位）：X 0.055（肩宽 0.55 m）× Z 0.035（进深 0.35 m）× Y 0.167（身高 1.67 m），
脚底 minY = 0，X/Z 居中。

注意：mesh 未建 vertex group，故导出不含 skin（与旧版一致，动画仅驱动骨骼节点）；
本批次不改这一点，只保证 walk clip 仍在（animations 非空、通道数不变）。
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
    make_box, make_sphere, make_material, join_objects, export_glb,
)

BODY = '#3a6a8a'
PANTS = '#2a3a4a'
HEAD = '#e8c8a8'
SHOES = '#1a1a1a'

# ── 人体关键高度（世界单位；身高 0.167 = 1.67 m）─────────────────────────
FOOT_Z = 0.012        # 鞋面高
KNEE_Z = 0.045
HIP_Z = 0.085
WAIST_Z = 0.090
CHEST_Z = 0.120
SHOULDER_Z = 0.145
NECK_Z = 0.150
HEAD_R = 0.014
HEAD_Z = 0.153        # 头顶 = 0.167 = 目标身高
ARM_X = 0.0205        # 上臂中心（外缘 0.0275 ⇒ 肩宽 X = 0.055）
LEG_X = 0.014
TORSO_D = 0.035       # 躯干进深 ⇒ 目标 Z


def _bone(eb, name, head, tail, parent=None):
    b = eb.new(name)
    b.head = head
    b.tail = tail
    if parent is not None:
        b.parent = parent
        b.use_connect = False
    return b


def _build_armature():
    """10 bone 骨架（Blender Z-up：骨骼"向下"= 沿 -Z，躯干沿 +Z 生长）。"""
    bpy.ops.object.armature_add(enter_editmode=False, location=(0, 0, 0))
    arm_obj = bpy.context.active_object
    arm_obj.name = 'PedestrianArmature'
    bpy.ops.object.mode_set(mode='EDIT')

    eb = arm_obj.data.edit_bones
    for b in list(eb):
        eb.remove(b)

    root = _bone(eb, 'root', (0, 0, 0), (0, 0, 0.020))
    hips = _bone(eb, 'hips', (0, 0, HIP_Z), (0, 0, HIP_Z + 0.010), root)
    spine = _bone(eb, 'spine', (0, 0, WAIST_Z), (0, 0, CHEST_Z), hips)
    chest = _bone(eb, 'chest', (0, 0, CHEST_Z), (0, 0, SHOULDER_Z), spine)
    neck = _bone(eb, 'neck', (0, 0, SHOULDER_Z), (0, 0, NECK_Z), chest)
    _bone(eb, 'head', (0, 0, NECK_Z), (0, 0, HEAD_Z + HEAD_R), neck)

    for side, sx in (('L', -1), ('R', 1)):
        ua = _bone(eb, f'upper_arm.{side}', (sx * ARM_X, 0, SHOULDER_Z - 0.005),
                   (sx * ARM_X, 0, 0.095), chest)
        _bone(eb, f'lower_arm.{side}', (sx * ARM_X, 0, 0.095),
              (sx * ARM_X, 0, 0.055), ua)
        ul = _bone(eb, f'upper_leg.{side}', (sx * LEG_X, 0, HIP_Z),
                   (sx * LEG_X, 0, KNEE_Z), hips)
        _bone(eb, f'lower_leg.{side}', (sx * LEG_X, 0, KNEE_Z),
              (sx * LEG_X, 0, 0.005), ul)

    bpy.ops.object.mode_set(mode='OBJECT')
    return arm_obj


def _build_mesh():
    """单 mesh（头 + 躯干 + 4 limb + 鞋），全部合计后 join 成 PedestrianMesh。"""
    parts = []
    # 头（球心抬到 0.153 ⇒ 头顶 = 0.167）
    parts.append(make_sphere('HeadMesh', HEAD_R, 12, (0, 0, HEAD_Z)))
    # 躯干（垂向 WAIST_Z..SHOULDER_Z，进深 = 目标 Z）
    parts.append(make_box('BodyMesh', (0.040, TORSO_D, SHOULDER_Z - WAIST_Z),
                          (0, 0, (WAIST_Z + SHOULDER_Z) / 2)))
    # 颈（藏于头/躯干之间）
    parts.append(make_box('NeckMesh', (0.014, 0.014, 0.014), (0, 0, NECK_Z - 0.005)))
    for side, sx in (('L', -1), ('R', 1)):
        parts.append(make_box(f'UpperArm{side}Mesh', (0.014, 0.014, 0.045),
                              (sx * ARM_X, 0, 0.1175)))
        parts.append(make_box(f'LowerArm{side}Mesh', (0.012, 0.012, 0.040),
                              (sx * ARM_X, 0, 0.075)))
        parts.append(make_box(f'UpperLeg{side}Mesh', (0.026, 0.026, 0.040),
                              (sx * LEG_X, 0, 0.065)))
        parts.append(make_box(f'LowerLeg{side}Mesh', (0.022, 0.022, 0.035),
                              (sx * LEG_X, 0, 0.0275)))
        # 鞋（脚尖朝 -Y = 行进方向）
        parts.append(make_box(f'Shoe{side}', (0.026, 0.030, FOOT_Z),
                              (sx * LEG_X, -0.0025, FOOT_Z / 2)))
    return join_objects(parts, 'PedestrianMesh')


def build_pedestrian() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    arm_obj = _build_armature()
    body_joined = _build_mesh()

    # 4 outfit material slot（body/pants/head/shoes）—— join 后按旧版口径统一 index 0，
    # 实际 outfit 差异由前端 uniforms 控制（v19.5 增强），本批次不改。
    for name, color in (('PedestrianBody', BODY), ('PedestrianPants', PANTS),
                        ('PedestrianHead', HEAD), ('PedestrianShoes', SHOES)):
        body_joined.data.materials.append(make_material(name, color, rough=0.8, metal=0.0))
    for poly in body_joined.data.polygons:
        poly.material_index = 0

    # Armature modifier（旧版无 vertex group ⇒ 导出不含 skin；保持原样不动）
    body_joined.parent = arm_obj
    mod = body_joined.modifiers.new(name='Armature', type='ARMATURE')
    mod.object = arm_obj

    # walk clip（24 帧循环，摆臂摆腿反相 ±30°，绕骨骼局部 X）
    scene = bpy.context.scene
    scene.frame_start = 1
    scene.frame_end = 24
    bpy.context.view_layer.objects.active = arm_obj
    bpy.ops.object.mode_set(mode='POSE')
    pbones = arm_obj.pose.bones

    def key(bone_name, axis, frames_degrees):
        pb = pbones[bone_name]
        pb.rotation_mode = 'XYZ'
        for f, deg in frames_degrees:
            scene.frame_set(f)
            pb.rotation_euler[axis] = math.radians(deg)
            pb.keyframe_insert(data_path='rotation_euler', index=axis, frame=f)

    key('upper_leg.L', 0, [(1, -30), (13, 30), (24, -30)])
    key('upper_leg.R', 0, [(1, 30), (13, -30), (24, 30)])
    key('upper_arm.L', 0, [(1, 30), (13, -30), (24, 30)])
    key('upper_arm.R', 0, [(1, -30), (13, 30), (24, -30)])

    bpy.ops.object.mode_set(mode='OBJECT')
    scene.frame_set(1)
    return arm_obj


if __name__ == '__main__':
    arm_obj = build_pedestrian()
    # 烘焙 mesh 的 object transform（join 残留首件的 location，如头球心 0.153）
    # ⇒ 导出节点 identity；armature 本身建在原点，保持 identity。
    for ob in bpy.context.scene.objects:
        if ob.type != 'MESH':
            continue
        bpy.ops.object.select_all(action='DESELECT')
        ob.select_set(True)
        bpy.context.view_layer.objects.active = ob
        bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/pedestrian_walk.glb'
    export_glb(out_path)
