#!/usr/bin/env python3
"""
build_canal_reed — 批次 38 运河岸芦苇丛（canal_reed.glb），沿岸点缀、实例化摆放。

── §27.0-1 真实结构调研 ─────────────────────────────────────────────────
  · 芦苇（Phragmites australis）：株高 1.5~3.0 m（水岸群落取 1.6~2.2 m），
    秆径 4~8 mm（城市可视尺度放大到 ~15 mm 半径，远看仍是草本细秆）；
    叶披针形长 20~50 cm、开展或下垂；顶生圆锥花序（穗）长 15~30 cm，
    花期棕褐色、枯季焦黄 —— 取 CITY_PALETTE['foliage_autumn']。
  · 丛径 ~2 m、每丛 12~16 秆（沿岸带状密植时按 1.5~2 m 间距实例化）。
  · 仓库口径对齐：植被色一律 CITY_PALETTE['foliage'] 系（CLAUDE.md §27.3）。

── 坐标与尺度规约（§27.3）───────────────────────────────────────────────
  · Blender Z-up：X/Y = 丛的水平足迹，Z = 高度（丛底 z = 0）。
  · 1 世界单位 = 10 m ⇒ 全部尺寸 ×U=0.1。禁止按真实米直接导出。
  · 节点变换 identity（export_glb 内置 bake_transforms）；minY = 0、X/Z 居中。
  · 秆全部竖直（不做倾斜）：倾斜后「绕中心旋转会把秆底抬离 z=0」，
    贴地判据会破；轮廓变化由株高差 + 水平散布承担。

── 交付尺寸（世界单位 / 真实米）──────────────────────────────────────────
  丛幅 X/Z ≈ 0.22 u（2.2 m）；丛高 Y ≈ 0.22 u（2.2 m，含最高秆+穗）。
  主导轴 Y ⇒ 直立判据可过（verify_glb_aabb 默认 'y' 模式）。

用法：
  blender --background --python build_canal_reed.py -- \
    ClientWeb/src/assets/models/road/canal_reed.glb
"""
import math
import os
import sys

import bpy

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (  # noqa: E402
    reset_scene, set_unit_meters,
    make_box, make_cylinder, make_cone, join_objects, export_glb,
    weathered_pbr, CITY_PALETTE,
)

U = 0.1  # 米 → 世界单位


def flatten_weathered_materials(obj):
    """weathered_pbr 的 Mix/Noise 链会让 glTF 导出丢掉 Base Color（导出白模，批次 38 实测）。
    导出前解链回常量：基色取 Mix 的 A 输入（CITY_PALETTE 值），粗糙度取 Math 的 addend。
    磨损只存在于 Blender 场景预览（glTF 无 Noise 对应物）。与 build_canal_bank 同文。"""
    for mat in obj.data.materials:
        if not mat or not mat.use_nodes:
            continue
        bsdf = mat.node_tree.nodes.get('Principled BSDF')
        if bsdf is None:
            continue
        bc = bsdf.inputs['Base Color']
        if bc.is_linked:
            node = bc.links[0].from_node
            col = (1.0, 1.0, 1.0, 1.0)
            if node.bl_idname == 'ShaderNodeMix':
                for s in node.inputs:
                    if s.name == 'A' and s.type == 'RGBA':
                        col = tuple(s.default_value)
                        break
            for lk in list(bc.links):
                mat.node_tree.links.remove(lk)
            bc.default_value = col
        rc = bsdf.inputs['Roughness']
        if rc.is_linked:
            node = rc.links[0].from_node
            rough = 0.8
            if node.bl_idname == 'ShaderNodeMath':
                rough = float(node.inputs[2].default_value)
            for lk in list(rc.links):
                mat.node_tree.links.remove(lk)
            rc.default_value = rough


def dedup_materials(obj):
    """同 (基色, 粗糙度, 金属度) 材质合并为共享槽 —— 14 秆×4 件各自 weathered_pbr
    会产出 50+ 份同色材质，GLB 里 51 个材质块白占体积（93 KB）。合并后 ≈3 份。"""
    seen = {}
    for i in range(len(obj.data.materials)):
        mat = obj.data.materials[i]
        if not mat:
            continue
        bsdf = mat.node_tree.nodes.get('Principled BSDF') if mat.use_nodes else None
        if bsdf is None:
            continue
        key = (tuple(bsdf.inputs['Base Color'].default_value),
               round(float(bsdf.inputs['Roughness'].default_value), 4),
               round(float(bsdf.inputs['Metallic'].default_value), 4))
        if key in seen:
            obj.data.materials[i] = seen[key]
        else:
            seen[key] = mat

# ── 丛的确定性布局（固定表，不用随机 ⇒ 同输入必同输出）────────────────────
# (x, y, height_m, has_head)：14 秆，足迹 ±1.05 m，高度 1.55~2.15 m
STALKS = [
    (-0.72, -0.55, 1.70, True), (-0.35, -0.82, 1.95, True), (0.10, -0.70, 1.60, False),
    (0.55, -0.62, 2.05, True), (0.88, -0.22, 1.75, False), (0.70, 0.25, 2.15, True),
    (0.32, 0.62, 1.85, True), (-0.15, 0.80, 1.65, False), (-0.58, 0.68, 2.00, True),
    (-0.90, 0.30, 1.55, False), (-0.55, -0.10, 2.10, True), (-0.05, 0.15, 1.80, True),
    (0.40, 0.05, 1.90, False), (0.05, -0.35, 2.00, True),
]
STALK_R = 0.015          # 秆半径 1.5 cm（可视尺度）
LEAF_L, LEAF_W = 0.42, 0.055   # 叶长/宽
HEAD_L, HEAD_R = 0.26, 0.035   # 穗长/半径
LEAN = 22.0              # 叶片开展角（度）


def _m(v):
    return v * U


def _leaf(name, x, y, z, ang_deg, ang_z_deg):
    """一枚披针叶：细长盒绕 X 倾斜 ang_deg、绕 Z 转向 ang_z_deg，
    根部抵在秆上（盒中心沿叶向偏移 L/2）。

    叶向 = euler XYZ 下局部 +Z 的世界像：
      Rz(az)·Rx(a)·ẑ = (sin a·sin az, −sin a·cos az, cos a)
    """
    a = math.radians(ang_deg)
    az = math.radians(ang_z_deg)
    dx = math.sin(a) * math.sin(az)
    dy = -math.sin(a) * math.cos(az)
    dz = math.cos(a)
    leaf = make_box(name, (_m(LEAF_W), _m(LEAF_W * 0.35), _m(LEAF_L)),
                    (_m(x + dx * LEAF_L / 2), _m(y + dy * LEAF_L / 2),
                     _m(z + dz * LEAF_L / 2)),
                    (a, 0, az))
    weathered_pbr(leaf, CITY_PALETTE['foliage'], rough=0.85, metal=0.0,
                  wear=0.25, scale=8.0)
    return leaf


def build_canal_reed() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()
    objs = []

    for i, (sx, sy, sh, has_head) in enumerate(STALKS):
        # 秆（竖直，底 z=0）
        stalk = make_cylinder(f'Stalk{i}', _m(STALK_R), _m(STALK_R), _m(sh), 6,
                              (_m(sx), _m(sy), _m(sh / 2)))
        weathered_pbr(stalk, CITY_PALETTE['foliage'], rough=0.80, metal=0.0,
                      wear=0.20, scale=10.0)
        objs.append(stalk)

        # 叶 2 枚（中下部，反向开展）
        objs.append(_leaf(f'Leaf{i}a', sx, sy, sh * 0.42, LEAN + (i % 3) * 4,
                          (i * 47) % 360))
        objs.append(_leaf(f'Leaf{i}b', sx, sy, sh * 0.62, LEAN + 14 - (i % 2) * 6,
                          (i * 47 + 150) % 360))

        # 顶穗（棕褐圆锥花序）
        if has_head:
            head = make_cone(f'Head{i}', _m(HEAD_R), _m(HEAD_L), 6,
                             (_m(sx), _m(sy), _m(sh + HEAD_L / 2)))
            weathered_pbr(head, CITY_PALETTE['foliage_autumn'], rough=0.90,
                          metal=0.0, wear=0.35, scale=12.0)
            objs.append(head)

    joined = join_objects(objs, 'CanalReed')
    return joined


if __name__ == '__main__':
    obj = build_canal_reed()
    flatten_weathered_materials(obj)   # 否则 GLB 白模（见函数文档）
    dedup_materials(obj)               # 51 份同色材质 ⇒ ~4 份，缩 GLB
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/canal_reed.glb'
    export_glb(out_path)
