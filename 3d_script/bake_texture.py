#!/usr/bin/env python3
"""
bake_texture — GLB 贴图烘焙通路（批次 90 首次落地；规范 §7 遗留候选 #1 结案）。

## 背景
`weathered_pbr()` 的程序噪声磨损**不经 glTF 导出**（Noise Texture 无 wire 对应物，
`__common__.py` 文档早已注明）—— 全城 73 个 GLB 因此全是纯色积木。前端链路经
2026-11-12 Explore 全量核实：GLTFLoader 原生解码内嵌贴图 → `modelCache` 材质签名
归并（签名含贴图 uuid）→ **零剥离代码**。本模块把「噪声磨损」烘成 baseColor 贴图
嵌进 GLB，是磨损进游戏的唯一通路。

## 工序（每材质桶）
    ① shared_weathered(objs, palette, rough, metal, wear=…, scale=…)   ← 建噪声链
    ② smart_uv(objs)                                                   ← Smart UV Project
    ③ img = bake_bucket(mat, objs, size=256)                           ← Cycles CPU 烘
    ④ wire_baked(mat, img)                                             ← 图接线 Base Color
    ⑤ join → ×0.1 → export_glb(image_format='JPEG')                    ← 内嵌出仓

## 色彩空间（批次 51 同族风险，贴图版）
glTF 2.0 规定 `baseColorTexture` 按 **sRGB** 存储。本通路：目标图 8-bit、
colorspace='sRGB' —— Cycles 把线性渲染值编码进 8-bit 缓冲时自动走 sRGB OETF。
**数值验收**在 `audit_glb_textures.py`：解码内嵌图 → sRGB→linear 反变换 →
与 CITY_PALETTE 期望线性值比对（±8%），不过即返工，不靠肉眼。

## 体积预算
程序噪声 PNG 压缩不动（256² ≈ 60-100 KB），JPEG（export_image_format='JPEG'）
约 1/4。单 GLB ≤ 500 KB（§27.3-1）由调用方在导出后自检。
"""
import math
import os
import sys

import bpy

_THIS_DIR = os.path.dirname(os.path.abspath(__file__))
if _THIS_DIR not in sys.path:
    sys.path.insert(0, _THIS_DIR)


def setup_cycles_for_bake() -> None:
    """烘 baseColor 用不着光照积分：CPU + 低采样 + 无降噪，llvmpipe 分钟级可完成。"""
    scene = bpy.context.scene
    scene.render.engine = 'CYCLES'
    scene.cycles.device = 'CPU'
    scene.cycles.samples = 4
    scene.cycles.use_denoising = False


def smart_uv(objs, angle_deg: float = 66.0, island_margin: float = 0.02) -> None:
    """Smart UV Project（逐对象，Edit Mode 上下文 —— headless 下多选一键展开的
    poll 不通过，必须逐个进编辑模式）。

    ⚠ 各对象独立装箱 ⇒ 同桶对象的 UV 互相重叠。对**纯程序噪声**烘焙无碍
    （每个对象读到的都是同一张噪声图的不同区域特征，无跨对象语义）；
    若未来烘**语义贴图**（字样/图案），必须改为整桶联合装箱。
    """
    if not objs:
        raise ValueError('smart_uv: empty bucket')
    for o in objs:
        bpy.ops.object.select_all(action='DESELECT')
        o.select_set(True)
        bpy.context.view_layer.objects.active = o
        bpy.ops.object.mode_set(mode='EDIT')
        bpy.ops.mesh.select_all(action='SELECT')
        bpy.ops.uv.smart_project(angle_limit=math.radians(angle_deg),
                                 island_margin=island_margin,
                                 correct_aspect=True, scale_to_bounds=True)
        bpy.ops.object.mode_set(mode='OBJECT')


def bake_bucket(mat, objs, size: int = 256):
    """把 mat 当前 Base Color 节点链（含 weathered 噪声）烘进一张新图，返回图像。

    关键细节：
    · 目标图挂进材质的 **active Image Texture 节点** —— bake 只写 active 节点的图；
    · `pass_filter={'COLOR'}` ⇒ 纯 Base Color（含噪声 Mix），不含任何光照；
    · ⚠ **金属度临时置 0**（批次 90 实测踩坑）：Cycles 的 DIFFUSE 颜色 pass 会把
      基色乘 (1−metallic) —— 金属面几乎没有漫反射。rail_mast（metal 0.75）烘出
      0.323×0.25≈0.081 的暗图。glTF 的 baseColorTexture 语义是**全量基色**
      （金属度走 metallicFactor 标量），故烘前置 0、烘后还原；
    · margin=12 + EXTEND ⇒ 接缝不出现黑边（island 外扩边）。
    """
    img = bpy.data.images.new(f'{mat.name}_Bake', width=size, height=size,
                              alpha=False, float_buffer=False)
    img.colorspace_settings.name = 'sRGB'
    nt = mat.node_tree
    tex = nt.nodes.new('ShaderNodeTexImage')
    tex.image = img
    tex.label = 'BakeTarget'
    tex.select = True
    nt.nodes.active = tex          # bake 写 active 节点的图
    bsdf = nt.nodes.get('Principled BSDF')
    metal_in = bsdf.inputs['Metallic']
    metal_backup = metal_in.default_value
    metal_in.default_value = 0.0   # 见 docstring：消除 (1−metallic) 衰减
    setup_cycles_for_bake()
    bpy.ops.object.select_all(action='DESELECT')
    for o in objs:
        o.select_set(True)
    bpy.context.view_layer.objects.active = objs[0]
    try:
        bpy.ops.object.bake(
            type='DIFFUSE',
            pass_filter={'COLOR'},      # 纯 Base Color（含噪声链），无任何光照
            use_clear=True, margin=12, margin_type='EXTEND',
        )
    finally:
        metal_in.default_value = metal_backup
    return img


def wire_baked(mat, img) -> None:
    """烘焙图接线 Base Color（替换 weathered 节点链）。

    烘完不接线的话：材质仍是节点链（导出被丢 ⇒ 白模，批次 38 事故同款），
    必须把 Image Texture → Base Color 连上，让导出器读到贴图。
    """
    nt = mat.node_tree
    bsdf = nt.nodes.get('Principled BSDF')
    if bsdf is None:
        raise RuntimeError(f'{mat.name}: Principled BSDF missing')
    tex = next((n for n in nt.nodes
                if n.type == 'TEX_IMAGE' and n.image == img), None)
    if tex is None:
        raise RuntimeError(f'{mat.name}: bake target node lost')
    nt.links.new(tex.outputs['Color'], bsdf.inputs['Base Color'])
