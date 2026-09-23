"""
__common__ — Blender headless 导出工具集（19-Blender3D模型集成）。

被 build_*.py 脚本统一 import；不直接执行。

公开 API:
  reset_scene()                 — 清空场景（删全部 mesh/lamp/material，但保留 world）
  set_unit_meters(scale=1.0)    — 把场景单位改成 meters（默认 1 blender unit = 1 m）
  make_box(name, size, pos, rot=None)        — 返回 bpy.types.Object
  make_cylinder(name, r_top, r_bot, h, segs, pos, rot=None)
  make_cone(name, r, h, segs, pos, rot=None)
  make_sphere(name, r, segs, pos)
  apply_pbr(obj, base_color, rough, metal, emissive=None, emissive_intensity=0.0)
  make_material(name, base_color, rough, metal, emissive=None, emissive_intensity=0.0) — 返回 mat
  assign_material(obj, mat)
  join_objects(objs, name)      — join 多个 object 到一个，删除中间产物
  export_glb(out_path, apply=True, animations=True) — 走 bpy.ops.export_scene.gltf

约束:
  - 全部用 bpy.ops.primitive_*,避免直接构造 mesh.vertices / faces
  - PBR 颜色用 hex "#rrggbb",bpy 4.x 支持
  - 单位:全部 Blender 单位 = 米,导出后 GLB 自动以 meter 为基准
"""
import bpy
import math


def reset_scene() -> None:
    """删除所有 mesh / light / camera / material（保留 world 与 render settings）。"""
    # 删 object
    bpy.ops.object.select_all(action='SELECT')
    bpy.ops.object.delete(use_global=False)
    # 删 orphan data
    for col in (bpy.data.meshes, bpy.data.materials, bpy.data.images,
                bpy.data.cameras, bpy.data.lights, bpy.data.armatures,
                bpy.data.actions):
        for it in list(col):
            col.remove(it)


def set_unit_meters(scale: float = 1.0) -> None:
    """1 Blender unit = 1 meter（scale 参数对应 Blender Scene Properties → Unit Scale）。"""
    scene = bpy.context.scene
    scene.unit_settings.system = 'METRIC'
    scene.unit_settings.scale_length = scale
    scene.unit_settings.length_unit = 'METERS'


def _new_object(name: str, data, pos, rot=None):
    obj = bpy.data.objects.new(name, data)
    bpy.context.collection.objects.link(obj)
    obj.location = (pos[0], pos[1], pos[2])
    if rot is not None:
        obj.rotation_euler = (rot[0], rot[1], rot[2])
    return obj


def make_box(name: str, size, pos, rot=None):
    """size = (x, y, z) meters。"""
    bpy.ops.mesh.primitive_cube_add(size=1, location=(0, 0, 0))
    obj = bpy.context.active_object
    obj.name = name
    obj.scale = (size[0], size[1], size[2])
    obj.location = (pos[0], pos[1], pos[2])
    if rot is not None:
        obj.rotation_euler = (rot[0], rot[1], rot[2])
    return obj


def make_cylinder(name: str, r_top: float, r_bot: float, h: float, segs: int, pos, rot=None):
    """圆柱（r_top == r_bot == h 为细管，r_top 0 / r_bot r 为圆锥）。"""
    bpy.ops.mesh.primitive_cylinder_add(vertices=max(segs, 8), radius=1, depth=1,
                                        location=(0, 0, 0))
    obj = bpy.context.active_object
    obj.name = name
    # Blender cylinder 默认沿 Z 轴,radius=1 depth=1 → 我们缩放到目标
    obj.scale = (r_bot if r_bot > 0 else r_top,
                 r_bot if r_bot > 0 else r_top,
                 h)
    obj.location = (pos[0], pos[1], pos[2])
    if rot is not None:
        obj.rotation_euler = (rot[0], rot[1], rot[2])
    return obj


def make_cone(name: str, r: float, h: float, segs: int, pos, rot=None):
    """圆锥（底半径 r，高 h），默认底朝下尖朝上。"""
    return make_cylinder(name, r_top=0.0, r_bot=r, h=h, segs=segs, pos=pos, rot=rot)


def make_sphere(name: str, r: float, segs: int, pos):
    bpy.ops.mesh.primitive_uv_sphere_add(radius=r,
                                         segments=max(segs, 8),
                                         ring_count=max(segs // 2, 4),
                                         location=(0, 0, 0))
    obj = bpy.context.active_object
    obj.name = name
    obj.location = (pos[0], pos[1], pos[2])
    return obj


def make_material(name: str, base_color: str, rough: float, metal: float,
                  emissive=None, emissive_intensity: float = 0.0):
    """创建 PBR 材质（Principled BSDF）。base_color 接受 "#rrggbb"。"""
    mat = bpy.data.materials.new(name)
    mat.use_nodes = True
    bsdf = mat.node_tree.nodes.get('Principled BSDF')
    if bsdf is None:
        raise RuntimeError('Principled BSDF node missing')
    # Base Color
    r, g, b = _hex_to_rgb(base_color)
    bsdf.inputs['Base Color'].default_value = (r, g, b, 1.0)
    bsdf.inputs['Roughness'].default_value = rough
    bsdf.inputs['Metallic'].default_value = metal
    if emissive is not None:
        er, eg, eb = _hex_to_rgb(emissive)
        bsdf.inputs['Emission Color'].default_value = (er, eg, eb, 1.0)
        bsdf.inputs['Emission Strength'].default_value = emissive_intensity
    return mat


def assign_material(obj, mat):
    """把 mat 赋给 obj 的所有 face（覆盖默认）。"""
    obj.data.materials.clear()
    obj.data.materials.append(mat)


def apply_pbr(obj, base_color: str, rough: float, metal: float,
              emissive=None, emissive_intensity: float = 0.0):
    """便利方法：make_material + assign_material 一体。"""
    mat = make_material(obj.name + '_Mat', base_color, rough, metal,
                        emissive=emissive, emissive_intensity=emissive_intensity)
    assign_material(obj, mat)
    return obj


def join_objects(objs, name: str):
    """把多个 obj join 成一个，保留 name 作为最终 object 名。"""
    bpy.ops.object.select_all(action='DESELECT')
    for o in objs:
        o.select_set(True)
    bpy.context.view_layer.objects.active = objs[0]
    bpy.ops.object.join()
    joined = bpy.context.active_object
    joined.name = name
    return joined


def _hex_to_rgb(hex_str: str):
    """'#rrggbb' / 'rrggbb' → (r, g, b) 浮点 0..1。"""
    s = hex_str.lstrip('#')
    if len(s) != 6:
        raise ValueError(f'bad hex color: {hex_str}')
    return (int(s[0:2], 16) / 255.0,
            int(s[2:4], 16) / 255.0,
            int(s[4:6], 16) / 255.0)


def export_glb(out_path: str, apply: bool = True, animations: bool = True) -> None:
    """导出当前场景为 .glb。

    apply=True:  导出前 apply 所有 transform（视觉与 gltf-viewer 一致）
    animations=True: 导出当前所有 armature action（行人 walk 动画依赖此）
    """
    import os
    os.makedirs(os.path.dirname(out_path), exist_ok=True)
    # 选中全部
    bpy.ops.object.select_all(action='DESELECT')
    for o in bpy.context.scene.objects:
        if o.type in ('MESH', 'ARMATURE', 'EMPTY'):
            o.select_set(True)
    bpy.context.view_layer.objects.active = bpy.context.scene.objects[0] if bpy.context.scene.objects else None
    bpy.ops.export_scene.gltf(
        filepath=out_path,
        export_format='GLB',
        export_apply=apply,
        export_animations=animations,
        export_materials='EXPORT',
        export_skins=animations,
        export_morph=False,
        export_lights=False,
        export_cameras=False,
        use_selection=True,
    )
    print(f'[export_glb] wrote {out_path}', flush=True)
