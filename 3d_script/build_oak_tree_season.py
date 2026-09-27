#!/usr/bin/env python3
"""
build_oak_tree_season — 季节橡树三变体（批次 27「时间比例与昼夜季节天气」§6.2）
Blender headless 导出脚本。一个脚本三个变体，按输出文件名的 stem 自动选择：

  nature/oak_tree_spring.glb   春：嫩绿三球冠 #8fd08a/#7cc47f/#a2dc9c + 粉白花球点缀
  nature/oak_tree_autumn.glb   秋：橙 #d98f3f / 深红 #b85c38 / 黄 #d9b13f 三球冠
  nature/oak_tree_winter.glb   冬：冠球缩 40% 深褐 #6b5240 + 白色雪壳 #eef3f7 覆枝
  （stem 为 oak_tree 时导出「夏」= 原版绿色，可用于修正批次 19 的 oak_tree.glb）

⚠️ 朝向规约（与批次 26 build_pine_tree.py 一致，**不要**照抄 build_oak_tree.py）：
  Blender 原生 Z-up 建模 → glTF/three.js Y-up 直立贴地，前端 <Model> /
  GlbInstanced 零旋转直挂。批次 19 build_oak_tree.py 按 Blender +Y 当「上」建模，
  经 glTF 导出 (x,y,z)b→(x,z,-y)g 映射后树体横躺在 -Z 上（已实测其 GLB AABB
  y∈[-0.25,0.25] / z∈[-1.01,-0.2]），本脚本改为 Z-up 修正该缺陷。

⚠️ 单位口径：**世界单位**（1 单位 = 10 m，cityScale.METERS_PER_UNIT）。几何数字
  沿用 build_oak_tree.py 原始量级（整树 ~1.0u ≈ 10m，EastForest 逐实例再乘
  scale 0.9~1.7），花球/雪壳等新增件按同量级配。

硬约束（CLAUDE.md §27）：pivot 地面中心 (0,0,0)、z≥0 贴地、单 GLB ≤500KB、
  .blend 不入库（本脚本即唯一事实来源）。

调用（对三个输出各跑一次；stem 决定变体，默认 /tmp 兜底路径为 spring）：
  cd 3d_script && blender --background --python build_oak_tree_season.py -- \
    /abs/path/ClientWeb/src/assets/models/nature/oak_tree_spring.glb
"""
import bpy
import os
import sys

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (  # noqa: E402
    reset_scene, set_unit_meters,
    make_box, make_cylinder, apply_pbr, join_objects, export_glb,
)

MAX_GLB_BYTES = 500 * 1024  # §27.3 硬上限

# ── 四季配色（stem → 变体名）─────────────────────────────────────────
VARIANTS = {
    'oak_tree_spring': 'spring',
    'oak_tree_autumn': 'autumn',
    'oak_tree_winter': 'winter',
    'oak_tree': 'summer',   # 原版绿；用于（可选）修正批次 19 的横躺 oak_tree.glb
}

TRUNK = '#5a4634'

# 夏（= build_oak_tree.py 原版绿，仅作对照/修正导出用）
SUMMER_CROWNS = ['#3a8a45', '#2f7a3a', '#4a9a55']
# 春：嫩绿三球 + 花球
SPRING_CROWNS = ['#8fd08a', '#7cc47f', '#a2dc9c']
SPRING_FLOWERS = ['#f4dbe2', '#fceef2', '#ffffff']
# 秋：橙 / 深红 / 黄
AUTUMN_CROWNS = ['#d98f3f', '#b85c38', '#d9b13f']
# 冬：冠缩 40% 深褐 + 雪壳
WINTER_CROWN = '#6b5240'
SNOW_SHELL = '#eef3f7'

# ── 橡树骨架（世界单位；与 build_oak_tree.py 同量级，改为 Z-up 贴地）────
# 树冠三球：(名, 半径, (x, y, z高度))
CROWN_SPECS = [
    ('Crown_0', 0.18, (0.04, 0.00, 0.65)),
    ('Crown_1', 0.20, (-0.05, 0.05, 0.78)),
    ('Crown_2', 0.16, (0.00, -0.06, 0.85)),
]
# 春季花球：贴在三球冠表面的粉白小点（半径 0.04）
SPRING_FLOWER_SPECS = [
    ('Flower_0', 0.04, (0.17, 0.09, 0.71)),
    ('Flower_1', 0.04, (-0.19, 0.02, 0.84)),
    ('Flower_2', 0.04, (0.03, 0.13, 0.97)),
]
# 冬季雪壳：压扁（scale z 0.5）白色低多面体壳，覆在枝/残冠上方
WINTER_SNOW_SPECS = [
    ('Snow_0', 0.19, (0.04, 0.00, 0.70)),
    ('Snow_1', 0.17, (-0.05, 0.05, 0.80)),
    ('Snow_2', 0.15, (0.00, -0.06, 0.88)),
    ('Snow_3', 0.15, (0.00, 0.00, 0.55)),
]
WINTER_CROWN_SCALE = 0.4   # 冠球缩放比例（任务规格：缩到 40%）


def _ico(name, r, pos, color, rough=0.85, squash_z=1.0, subdivisions=2):
    """icosahedron 球（低多面体感）；squash_z<1 时压扁（雪壳）。"""
    bpy.ops.mesh.primitive_ico_sphere_add(radius=r, subdivisions=subdivisions,
                                          location=(0, 0, 0))
    obj = bpy.context.active_object
    obj.name = name
    obj.location = pos
    if squash_z != 1.0:
        obj.scale = (1.0, 1.0, squash_z)
    apply_pbr(obj, color, rough=rough, metal=0.0)
    return obj


def build_oak_tree_season(variant: str) -> bpy.types.Object:
    reset_scene()
    set_unit_meters()

    objs = []

    # 主干（z ∈ [0, 0.5]，贴地）
    trunk = make_cylinder('Trunk', 0.04, 0.05, 0.5, 12, (0, 0, 0.25))
    apply_pbr(trunk, TRUNK, rough=0.95, metal=0.0)
    objs.append(trunk)

    # 2 分枝（box 长轴竖放后绕 Y 外倾 ±0.5rad，简化视觉）
    for i, (tilt_y, off_x) in enumerate([(0.5, 0.05), (-0.5, -0.05)]):
        branch = make_box(f'Branch_{i}', (0.03, 0.03, 0.18),
                          (off_x, 0.0, 0.45), rot=(0, tilt_y, 0))
        apply_pbr(branch, TRUNK, rough=0.95, metal=0.0)
        objs.append(branch)

    # 3 球错落树冠（季节换色；冬季缩 40% 深褐）
    crown_colors = {
        'summer': SUMMER_CROWNS,
        'spring': SPRING_CROWNS,
        'autumn': AUTUMN_CROWNS,
        'winter': [WINTER_CROWN] * 3,
    }[variant]
    crown_scale = WINTER_CROWN_SCALE if variant == 'winter' else 1.0
    for (name, r, pos), color in zip(CROWN_SPECS, crown_colors):
        objs.append(_ico(name, r * crown_scale, pos, color))

    # 季节附加件
    if variant == 'spring':
        for (name, r, pos), color in zip(SPRING_FLOWER_SPECS, SPRING_FLOWERS):
            objs.append(_ico(name, r, pos, color, rough=0.6))
    elif variant == 'winter':
        for name, r, pos in WINTER_SNOW_SPECS:
            # 低多面体（subdiv 1）+ 压扁雪壳，覆在枝/冠上方
            objs.append(_ico(name, r, pos, SNOW_SHELL, rough=0.55,
                             squash_z=0.5, subdivisions=1))

    joined = join_objects(objs, f'OakTree_{variant.capitalize()}')
    bpy.ops.object.select_all(action='DESELECT')
    joined.select_set(True)
    bpy.context.view_layer.objects.active = joined
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    return joined


def variant_for(out_path: str) -> str:
    stem = os.path.splitext(os.path.basename(out_path))[0]
    if stem not in VARIANTS:
        raise SystemExit(
            f'未知输出文件名 stem: {stem!r}；'
            f"允许 {' / '.join(VARIANTS)}（变体由文件名决定）")
    return VARIANTS[stem]


if __name__ == '__main__':
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv \
        else '/tmp/oak_tree_spring.glb'
    variant = variant_for(out_path)
    build_oak_tree_season(variant)
    export_glb(out_path)
    size = os.path.getsize(out_path)
    print(f'[size] {size} bytes ({variant})', flush=True)
    if size > MAX_GLB_BYTES:
        raise SystemExit(f'!! 超出 §27.3 单 GLB 500KB 硬上限: {size}')
