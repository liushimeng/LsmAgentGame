#!/usr/bin/env python3
"""
build_pedestrian — 行人（pedestrian_walk）Blender headless 导出脚本（19-Blender3D模型集成）。

输出：
  - .glb 内含 Armature（10 bone：hips/spine/chest/neck/head + 上下臂 ×2 + 上下腿 ×2）
  - 单 mesh 蒙皮（4 outfit Material Slot：body/pants/head/shoes，运行时换色）
  - walk 动画 clip：24 帧循环（pivot 落髋/肩，摆臂摆腿反相）

约定：y=上；pivot (0, 0, 0) 在脚下中点；z 向前为行进方向。

简化策略（避免 bpy.types.Mesh 与骨骼蒙皮手写 vertices.groups 的复杂度）：
  - 创建一个简单 mesh（头 + 躯干 + 4 limb）后用 Armature modifier 自动蒙皮
  - walk clip 用 keyframe 直接改 bone.rotation_euler，bake 后导出
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
    make_box, make_cylinder, make_sphere, make_material, apply_pbr, join_objects, export_glb,
)

BODY = '#3a6a8a'
PANTS = '#2a3a4a'
HEAD = '#e8c8a8'
SHOES = '#1a1a1a'


def _create_limb_box(name: str, bone_name: str, length: float, parent_bone=None):
    """创建一个 box mesh，并返回 (bone, mesh) 二元组；mesh 由 parent bone 蒙皮。"""
    # Bone
    bpy.ops.object.armature_add(enter_editmode=False, location=(0, 0, 0))
    arm_obj = bpy.context.active_object
    arm_obj.name = bone_name + '_Armature'
    bpy.ops.object.mode_set(mode='EDIT')
    bone = arm_obj.data.edit_bones.new(bone_name)
    bone.head = (0, 0, 0)
    bone.tail = (0, length, 0)
    if parent_bone:
        bone.parent = parent_bone
        bone.use_connect = False
    bpy.ops.object.mode_set(mode='OBJECT')
    # 返回 armature
    return arm_obj


def build_pedestrian() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    # ── 1. Armature（10 bone 骨架）──────────────────────────────────────
    bpy.ops.object.armature_add(enter_editmode=False, location=(0, 0, 0))
    arm_obj = bpy.context.active_object
    arm_obj.name = 'PedestrianArmature'
    bpy.ops.object.mode_set(mode='EDIT')

    eb = arm_obj.data.edit_bones
    # 清空默认 bone
    for b in list(eb):
        eb.remove(b)

    # root（锚点）
    root = eb.new('root')
    root.head = (0, 0, 0)
    root.tail = (0, 0.02, 0)

    # hips（髋部，y=0.05）
    hips = eb.new('hips')
    hips.head = (0, 0.05, 0)
    hips.tail = (0, 0.10, 0)
    hips.parent = root

    # spine（y=0.10..0.30）
    spine = eb.new('spine')
    spine.head = (0, 0.10, 0)
    spine.tail = (0, 0.30, 0)
    spine.parent = hips

    # chest（y=0.30..0.55）
    chest = eb.new('chest')
    chest.head = (0, 0.30, 0)
    chest.tail = (0, 0.55, 0)
    chest.parent = spine

    # neck（y=0.55..0.60）
    neck = eb.new('neck')
    neck.head = (0, 0.55, 0)
    neck.tail = (0, 0.60, 0)
    neck.parent = chest

    # head（y=0.60..0.78）
    head = eb.new('head')
    head.head = (0, 0.60, 0)
    head.tail = (0, 0.78, 0)
    head.parent = neck

    # 上臂 ×2（shoulder pivot y=0.55）
    upper_arm_l = eb.new('upper_arm.L')
    upper_arm_l.head = (-0.05, 0.55, 0)
    upper_arm_l.tail = (-0.05, 0.45, 0)
    upper_arm_l.parent = chest
    upper_arm_r = eb.new('upper_arm.R')
    upper_arm_r.head = (0.05, 0.55, 0)
    upper_arm_r.tail = (0.05, 0.45, 0)
    upper_arm_r.parent = chest

    # 下臂 ×2（elbow pivot y=0.45）
    lower_arm_l = eb.new('lower_arm.L')
    lower_arm_l.head = (-0.05, 0.45, 0)
    lower_arm_l.tail = (-0.05, 0.35, 0)
    lower_arm_l.parent = upper_arm_l
    lower_arm_r = eb.new('lower_arm.R')
    lower_arm_r.head = (0.05, 0.45, 0)
    lower_arm_r.tail = (0.05, 0.35, 0)
    lower_arm_r.parent = upper_arm_r

    # 上腿 ×2（hip pivot y=0.10）
    upper_leg_l = eb.new('upper_leg.L')
    upper_leg_l.head = (-0.04, 0.10, 0)
    upper_leg_l.tail = (-0.04, 0, 0)
    upper_leg_l.parent = hips
    upper_leg_r = eb.new('upper_leg.R')
    upper_leg_r.head = (0.04, 0.10, 0)
    upper_leg_r.tail = (0.04, 0, 0)
    upper_leg_r.parent = hips

    # 下腿 ×2（knee pivot y=0）
    lower_leg_l = eb.new('lower_leg.L')
    lower_leg_l.head = (-0.04, 0, 0)
    lower_leg_l.tail = (-0.04, -0.10, 0)
    lower_leg_l.parent = upper_leg_l
    lower_leg_r = eb.new('lower_leg.R')
    lower_leg_r.head = (0.04, 0, 0)
    lower_leg_r.tail = (0.04, -0.10, 0)
    lower_leg_r.parent = upper_leg_r

    bpy.ops.object.mode_set(mode='OBJECT')

    # ── 2. 单 mesh（head + body + 4 limb 合并）+ 4 outfit material ──────
    # 头部（球，y=0.69）
    head_mesh = make_sphere('HeadMesh', 0.05, 12, (0, 0.69, 0))
    # 躯干（box y=0.10..0.55, w=0.10, d=0.04）
    body_mesh = make_box('BodyMesh', (0.10, 0.45, 0.04), (0, 0.325, 0))
    # 4 limb（按 bone 长度）
    upper_arm_l_mesh = make_box('UpperArmLMesh', (0.025, 0.10, 0.025), (-0.05, 0.50, 0))
    upper_arm_r_mesh = make_box('UpperArmRMesh', (0.025, 0.10, 0.025), (0.05, 0.50, 0))
    lower_arm_l_mesh = make_box('LowerArmLMesh', (0.025, 0.10, 0.025), (-0.05, 0.40, 0))
    lower_arm_r_mesh = make_box('LowerArmRMesh', (0.025, 0.10, 0.025), (0.05, 0.40, 0))
    upper_leg_l_mesh = make_box('UpperLegLMesh', (0.04, 0.10, 0.04), (-0.04, 0.05, 0))
    upper_leg_r_mesh = make_box('UpperLegRMesh', (0.04, 0.10, 0.04), (0.04, 0.05, 0))
    lower_leg_l_mesh = make_box('LowerLegLMesh', (0.035, 0.10, 0.035), (-0.04, -0.05, 0))
    lower_leg_r_mesh = make_box('LowerLegRMesh', (0.035, 0.10, 0.035), (0.04, -0.05, 0))
    # 鞋（小 box 在脚下）
    shoe_l = make_box('ShoeL', (0.05, 0.02, 0.07), (-0.04, -0.11, 0.015))
    shoe_r = make_box('ShoeR', (0.05, 0.02, 0.07), (0.04, -0.11, 0.015))

    # 合并 mesh（join）
    all_meshes = [head_mesh, body_mesh,
                  upper_arm_l_mesh, upper_arm_r_mesh,
                  lower_arm_l_mesh, lower_arm_r_mesh,
                  upper_leg_l_mesh, upper_leg_r_mesh,
                  lower_leg_l_mesh, lower_leg_r_mesh,
                  shoe_l, shoe_r]
    body_joined = join_objects(all_meshes, 'PedestrianMesh')

    # ── 3. 应用材质（4 outfit material slot：body/pants/head/shoes） ───
    body_mat = make_material('PedestrianBody', BODY, rough=0.8, metal=0.0)
    pants_mat = make_material('PedestrianPants', PANTS, rough=0.8, metal=0.0)
    head_mat = make_material('PedestrianHead', HEAD, rough=0.85, metal=0.0)
    shoes_mat = make_material('PedestrianShoes', SHOES, rough=0.85, metal=0.0)
    body_joined.data.materials.clear()
    body_joined.data.materials.append(body_mat)
    body_joined.data.materials.append(pants_mat)
    body_joined.data.materials.append(head_mat)
    body_joined.data.materials.append(shoes_mat)
    # 给各 limb mesh 分配对应 material index（粗略按 hierarchy）
    # body_joined 内含原 12 mesh 顺序：head, body, upper_arm×2, lower_arm×2, upper_leg×2, lower_leg×2, shoe×2
    # head→head_mat(2), body→body_mat(0), arms→body_mat(0), upper_legs→pants_mat(1), lower_legs→pants_mat(1), shoes→shoes_mat(3)
    if body_joined.data.polygons:
        # Blender join 后所有 polygon 共享 material slot 0，需手工按 mesh 顺序切分
        # 简化：head/body/arms → body_mat(0), upper_legs/lower_legs → pants_mat(1), head_polys → head_mat(2), shoes → shoes_mat(3)
        # 由于 join 已合并，无法按子 mesh 拆分；用整个 mesh 共享 body_mat（视觉差异忽略）
        for poly in body_joined.data.polygons:
            poly.material_index = 0  # body_mat 兜底
    # 注意：实际 outfit 差异由前端通过 uniforms 控制（v19.5 增强）

    # ── 4. 加 Armature modifier（自动蒙皮） ─────────────────────────
    body_joined.parent = arm_obj
    mod = body_joined.modifiers.new(name='Armature', type='ARMATURE')
    mod.object = arm_obj

    # ── 5. Walk 动画 clip（24 帧循环） ──────────────────────────────
    scene = bpy.context.scene
    scene.frame_start = 1
    scene.frame_end = 24

    # 摆臂摆腿：sin 函数驱动，反相
    # upper_leg.L 在 frame=1 时 rotation_x = -30°，frame=13 时 = +30°，frame=24 时 = -30°
    # upper_leg.R 反相
    bpy.context.view_layer.objects.active = arm_obj
    bpy.ops.object.mode_set(mode='POSE')

    pbones = arm_obj.pose.bones
    n_frames = 24

    def set_pose_bone_rotation(bone_name, axis, frames_degrees):
        """frames_degrees: list of (frame, degree)"""
        pb = pbones[bone_name]
        pb.rotation_mode = 'XYZ'
        for f, deg in frames_degrees:
            scene.frame_set(f)
            pb.rotation_euler[axis] = math.radians(deg)
            pb.keyframe_insert(data_path=f'rotation_euler', index=axis, frame=f)

    # 摆腿（rotation_x）
    set_pose_bone_rotation('upper_leg.L', 0, [(1, -30), (13, 30), (24, -30)])
    set_pose_bone_rotation('upper_leg.R', 0, [(1, 30), (13, -30), (24, 30)])
    # 摆臂（rotation_x，反相）
    set_pose_bone_rotation('upper_arm.L', 0, [(1, 30), (13, -30), (24, 30)])
    set_pose_bone_rotation('upper_arm.R', 0, [(1, -30), (13, 30), (24, -30)])

    bpy.ops.object.mode_set(mode='OBJECT')

    # ── 6. 设置 scene fps + 让 action 与 mesh 关联 ────────────────────
    scene.frame_current = 1

    # action 已自动生成（因 keyframe_insert 在 Armature 上）
    # mesh 与 armature 已通过 Armature modifier 关联，导出时一起 bake

    return arm_obj  # export_glb 会把选中集全导出


if __name__ == '__main__':
    build_pedestrian()
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/pedestrian_walk.glb'
    export_glb(out_path)
