#!/usr/bin/env python3
"""
build_trash_can — 路侧分类垃圾桶（trash_can.glb）Blender 导出脚本（批次 24）。

GLB 内合并 2 个独立命名对象：
  - TrashCan_Green（厨余绿 #2e8b57 系）置于 x=0
  - TrashCan_Blue（可回收蓝 #1668b3 系）置于 x=+0.12（排开，调用方按对象名取节点）

几何（单位=米，1u 世界 = 1m 模型）：
  - 桶身：圆柱 r_top=0.035 / r_bot=0.0375，h=0.09（重心低、俯视为主，底部不封细节）
  - 桶盖：略大短圆柱 r=0.039 h=0.012 + 顶部微凸扁球（总高 ≈ 0.11）
  - 投放口：深色暗槽 box 嵌于盖前侧（glTF +Z 朝向 = Blender -Y 侧）
每变体 3 材质：桶身 / 桶盖（含顶凸，略深）/ 暗槽。

⚠ 朝向规约（2026-09-26 批次 24 实测定稿，与本目录 19 批次部分旧脚本不同）：
  本脚本按 **Blender 原生 Z-up** 摆放（圆柱默认轴 = Z，桶底 z=0）。经
  bpy.ops.export_scene.gltf 的 Yup 转换（(x,y,z)_b → (x,z,-y)_g）后，glTF/three.js
  中垃圾桶**直立、底面贴 y=0、投放口朝 +Z**，前端无需任何旋转补偿即可
  instancedMesh 直挂。旧脚本（build_road_props/city_hall 等）按 Y-up 注释摆放，
  导出后长轴落在 glTF Z 上（侧躺），本批次不扩散该写法。
  验证：python 解析 glb JSON+BIN chunk 计算节点世界包围盒（见批次 24 实施记录）。

调用（同 build_road_props.py 模式）：
  cd 3d_script && blender --background --python build_trash_can.py -- <输出.glb>
"""
import bpy
import sys
import os

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (  # noqa: E402
    reset_scene, set_unit_meters,
    make_box, make_cylinder, make_sphere, apply_pbr, assign_material,
    make_material, join_objects, export_glb,
)

# ── 桶径/桶高（2026-09-27 修正：真实分类桶 ⌀0.5 m × 1.0 m）──────────────
#   旧值 BODY_R 0.035 / BODY_H 0.09 / LID_R 0.039 ⇒ 单桶 ⌀0.78 m × 高 1.11 m，
#   比旁边 1 m 长的玩具车还大（用户反馈"垃圾桶与汽车比例不正常"）。
#   现按 ⌀0.050 × 高 0.100（世界单位）收敛：径向 ×0.64、垂向 ×0.90。
BODY_R, BODY_H = 0.0225, 0.0825    # 桶身（r_top；r_bot 见下 = 0.024，微锥度）
LID_R, LID_H = 0.025, 0.012        # 桶盖半径 = 单桶 ⌀/2 = 0.025 ⇒ ⌀ 0.050
DOME_SQUASH = 0.22                 # 盖顶扁球压扁比（总高 = BODY_H+LID_H+DOME_SQUASH·LID_R = 0.100）
SLOT = '#161616'   # 投放口暗槽近黑


def build_one_trash_can(name: str, body_hex: str, lid_hex: str, x_offset: float):
    """单个垃圾桶（Blender Z-up，桶底 z=0）；返回 list[obj]（未 join）。"""
    objs = []
    base = f'{name}_'

    # 桶身（微锥度：上略收，重心稳；圆柱默认轴 = Z = 直立）
    body = make_cylinder(f'{base}Body', BODY_R, 0.024, BODY_H, 20,
                         (x_offset, 0, BODY_H / 2))
    apply_pbr(body, body_hex, rough=0.55, metal=0.05)
    objs.append(body)

    # 桶盖（略大短圆柱）+ 盖顶微凸（扁球）共用同一盖材质（每变体 ≤3 材质）
    lid_mat = make_material(f'{name}_Lid_Mat', lid_hex, rough=0.50, metal=0.05)
    lid = make_cylinder(f'{base}Lid', LID_R, LID_R, LID_H, 20,
                        (x_offset, 0, BODY_H + LID_H / 2))
    assign_material(lid, lid_mat)
    objs.append(lid)

    dome = make_sphere(f'{base}Dome', LID_R, 20, (x_offset, 0, BODY_H + LID_H))
    dome.scale = (1.0, 1.0, DOME_SQUASH)   # 沿 Z 压扁 → 顶部微凸
    assign_material(dome, lid_mat)
    objs.append(dome)

    # 投放口暗槽（深色 box 嵌于盖前侧；glTF +Z 前 = Blender -Y 侧，内缩呈凹槽观感）
    # 尺寸随桶径等比收缩（径向 ×0.64 / 垂向 ×0.90），保持"槽宽 ≈ 1/3 盖径"的观感
    slot = make_box(f'{base}Slot', (0.0167, 0.0083, 0.0054),
                    (x_offset, -(LID_R - 0.0042), BODY_H + LID_H / 2 + 0.0014))
    apply_pbr(slot, SLOT, rough=0.9, metal=0.0)
    objs.append(slot)

    return objs


def build_trash_can():
    reset_scene()
    set_unit_meters()

    green = build_one_trash_can('TrashCan_Green', '#2e8b57', '#24704a', 0.0)
    join_objects(green, 'TrashCan_Green')
    blue = build_one_trash_can('TrashCan_Blue', '#1668b3', '#11568f', 0.12)
    join_objects(blue, 'TrashCan_Blue')


if __name__ == '__main__':
    build_trash_can()
    # 烘焙 join 残留 object transform（__common__.make_* 用 obj.scale 表达尺寸）
    # ⇒ 导出节点 identity（<Model>/instancedMesh 零旋转零 scale 直挂）。几何/尺寸不变。
    # 注：__common__.export_glb 自 2026-09-27 起亦统一 bake_transforms()（同一目的）；
    # 此处显式保留，既让脚本自证，也防 __common__ 行为回退时静默复发。
    for ob in bpy.context.scene.objects:
        bpy.ops.object.select_all(action='DESELECT')
        ob.select_set(True)
        bpy.context.view_layer.objects.active = ob
        bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/trash_can.glb'
    export_glb(out_path)
