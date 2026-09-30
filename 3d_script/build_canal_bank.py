#!/usr/bin/env python3
"""
build_canal_bank — 批次 38「真实世界布局与交互修正」运河驳岸段（canal_bank.glb）。

单段可实例化平铺（沿河每 8 m 一段），一文件内含：
  · 驳岸挡墙（retaining wall）
  · 压顶（coping cap，两侧各挑 8 cm）
  · 亲水台阶（5 级 + 底部平台，从压顶层下到水位）

── §27.0-1 真实结构调研（开工前先检索参考；本次 WebSearch 无可用返回，
   改用工程标准值 + 仓库既有资产口径，结论如下）────────────────────────
  · 都市运河驳岸（参考：城市河道整治通用断面、Amsterdam quay、北京亮马河）
    —— 挡墙露出水面 1.0~2.5 m；压顶宽 0.4~0.6 m、厚 0.15~0.25 m；
    —— 亲水台阶：踏面 0.30~0.40 m、踢面 0.15~0.18 m（行人舒适域）。
  · 本件取值（略陡于舒适域，换取 8 m 段内 5 级下到水位的可读性）：
    挡墙高 1.40 m（露出水面 1.40−0.28≈1.12 m）、厚 0.60 m；
    压顶 0.75×0.20 m（两侧各挑 8 cm）；
    台阶踢面 0.24 m、踏面 0.34 m ×5 级 + 0.50 m 平台。
  · 尺度口径与仓库既有运河家具对齐：CanalExtras 系船柱 0.7 m / 护栏柱 1.2 m
    （ClientWeb .../civic/CanalExtras.tsx 经 u() 消费真实米制）。
  · 材质：CITY_PALETTE['concrete_dark']（挡墙）/ ['concrete']（压顶·台阶），
    weathered_pbr 经年磨损（glTF 不导出程序化噪声，仅 Blender 内预览评估）。

── 坐标与尺度规约（CLAUDE.md §27.3，全目录统一）─────────────────────────
  · Blender 原生 **Z-up**：X = 沿河段长（平铺方向），Y = 横河（−Y 水侧 / +Y 陆侧），
    Z = 高度（地面 z = 0）。导出 glTF 后 X = 段长、Y = 高（minY=0）、Z = 横河。
  · 世界单位：**1 单位 = 10 m**（cityScale.METERS_PER_UNIT）⇒ 全部尺寸 ×U=0.1
    换算后写入，禁止按“真实米”直接导出（批次 26 的 10× 巨型雪山即此错）。
  · 节点变换 identity —— 尺寸一律经 bake_transforms 烘进顶点（export_glb 内置）。
  · 原点：minY = 0 贴地、X/Z 居中（X∈[−4,4]；Y 构建后整体平移居中）。

── 交付尺寸（世界单位 / 真实米）──────────────────────────────────────────
  段长 X = 0.800 u（8.0 m）；总高 Y = 0.160 u（1.60 m）；横河深 Z = 0.296 u（2.96 m）。
  亲水台阶位于段中心（X∈[−0.18, 0.18] u），沿河每 8 m 一组台阶（真实城市常见密度）。

用法：
  blender --background --python build_canal_bank.py -- \
    ClientWeb/src/assets/models/road/canal_bank.glb
"""
import os
import sys

import bpy

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)
from __common__ import (  # noqa: E402
    reset_scene, set_unit_meters,
    make_box, join_objects, export_glb, weathered_pbr, CITY_PALETTE,
)

U = 0.1            # 米 → 世界单位（1 u = 10 m）


def flatten_weathered_materials(obj):
    """weathered_pbr 的 Mix/Noise 节点链会让 glTF 导出**丢掉 Base Color**（导出白模）：
    Principled BSDF 的 Base Color 被链路占用后，导出器读不到常量 ⇒ baseColorFactor 缺省
    = 纯白（批次 38 实测：canal_bank/ canal_reed 首版 9/51 个材质全白）。
    导出前解链回常量：基色取 Mix 的 A 输入（weathered_pbr 写入的 CITY_PALETTE 值），
    粗糙度取 Math MULTIPLY_ADD 的 addend（= 原 rough）。磨损只存在于 Blender 场景
    预览（glTF 无 Noise 对应物，weathered_pbr 文档已注明），游戏内即纯色基值。
    """
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
                rough = float(node.inputs[2].default_value)   # MULTIPLY_ADD 的 addend
            for lk in list(rc.links):
                mat.node_tree.links.remove(lk)
            rc.default_value = rough

# ── 断面尺寸（米）─────────────────────────────────────────────────────────
SEG_LEN = 8.0          # 段长（平铺周期）
WALL_T = 0.60          # 挡墙厚
WALL_H = 1.40          # 挡墙高（含水下部分，minY=0 为河床基准）
COP_W = 0.78           # 压顶宽（较墙两侧各挑 0.09）
COP_H = 0.20           # 压顶厚
STEP_W = 3.6           # 台阶组宽（沿河）
STEP_TREAD = 0.34      # 踏面
STEP_RISE = 0.24       # 踢面（5 级 × 0.24 = 1.20，从墙顶 1.40 下到 0.20）
N_STEPS = 5
LAND_TREAD = 0.50      # 底部平台深
LAND_TOP = 0.20        # 平台顶高（≈水位 0.28 下方，亲水）

# 陆侧墙背 = Y+0.60（构建原点）；水侧 = Y 负方向。全部建完后 +Y 平移居中。


def _m(v):
    return v * U


def build_canal_bank() -> bpy.types.Object:
    reset_scene()
    set_unit_meters()
    objs = []

    # 挡墙（水侧面向 Y=0；墙厚向 +Y）
    wall = make_box('Wall',
                    (_m(SEG_LEN), _m(WALL_T), _m(WALL_H)),
                    (0, _m(WALL_T / 2), _m(WALL_H / 2)))
    weathered_pbr(wall, CITY_PALETTE['concrete_dark'], rough=0.85, metal=0.0,
                  wear=0.40, scale=3.0)   # 整体风化：低频大块污渍
    objs.append(wall)

    # 压顶（两侧各挑 0.09 m，顶面 = WALL_H + COP_H）。
    # 台阶开口处（X∈[−STEP_W/2, STEP_W/2]）压顶必须断开 —— 否则第 1 级踏面
    # 顶面被压顶底面盖成“天花板”（v1 实渲染判读返工点）。
    cop_y0 = -0.09                      # 水侧挑出
    cop_len = (SEG_LEN - STEP_W) / 2.0  # 每段压顶长（左右各一）
    for side in (-1, 1):
        cx = side * (STEP_W / 2.0 + cop_len / 2.0)
        cop = make_box('Coping_L' if side < 0 else 'Coping_R',
                       (_m(cop_len), _m(COP_W), _m(COP_H)),
                       (_m(cx), _m(cop_y0 + COP_W / 2), _m(WALL_H + COP_H / 2)))
        weathered_pbr(cop, CITY_PALETTE['concrete'], rough=0.75, metal=0.0,
                      wear=0.30, scale=4.0)
        objs.append(cop)

    # 亲水台阶：5 级 + 平台，自压顶水侧缘（Y=−0.09）向 −Y 下行
    y_cursor = -0.09
    top_z = WALL_H                       # 第 1 级顶面与墙顶齐平
    for i in range(N_STEPS):
        y0, y1 = y_cursor - STEP_TREAD, y_cursor
        st = make_box(f'Step{i}',
                      (_m(STEP_W), _m(STEP_TREAD), _m(top_z)),
                      (0, _m((y0 + y1) / 2), _m(top_z / 2)))
        weathered_pbr(st, CITY_PALETTE['concrete'], rough=0.80, metal=0.0,
                      wear=0.45, scale=5.0)   # 近水台阶更脏
        objs.append(st)
        y_cursor = y0
        top_z -= STEP_RISE

    # 底部亲水平台（顶面 LAND_TOP）
    y0, y1 = y_cursor - LAND_TREAD, y_cursor
    landing = make_box('Landing',
                       (_m(STEP_W), _m(LAND_TREAD), _m(LAND_TOP)),
                       (0, _m((y0 + y1) / 2), _m(LAND_TOP / 2)))
    weathered_pbr(landing, CITY_PALETTE['concrete'], rough=0.85, metal=0.0,
                  wear=0.50, scale=6.0)
    objs.append(landing)

    # Y 居中：整体 Y∈[y0, COP_W−0.09]，平移 −(y_min+y_max)/2
    y_min, y_max = y0, COP_W - 0.09
    y_shift = -(y_min + y_max) / 2.0
    for o in objs:
        o.location = (o.location[0], o.location[1] + _m(y_shift), o.location[2])

    joined = join_objects(objs, 'CanalBank')
    return joined


if __name__ == '__main__':
    obj = build_canal_bank()
    # 导出前把 weathered_pbr 节点链压回常量色（否则 GLB 白模，见函数文档）
    flatten_weathered_materials(obj)
    # 显式烘焙（与 export_glb 内置 bake 双保险；防止 __common__ 行为回退时静默复发）
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.transform_apply(location=True, rotation=True, scale=True)
    out_path = sys.argv[sys.argv.index('--') + 1] if '--' in sys.argv else '/tmp/canal_bank.glb'
    export_glb(out_path)
