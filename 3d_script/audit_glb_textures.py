#!/usr/bin/env python3
"""
audit_glb_textures — 批次 90 贴图烘焙通路数值门禁（非零退出 = 失败）。

对 GLB 做两类检查：
  A. 结构检查：目标材质存在、且挂了 baseColorTexture（防「烘焙了但没接线」白模）；
  B. 色彩空间检查：解码内嵌图 → sRGB→linear 反变换 → 与 CITY_PALETTE 期望线性值
     比对（中位数口径 ±8%）—— 防批次 51 同族缺陷复发（sRGB/linear 弄反 ⇒ 整件偏亮/偏暗）。

用法：
  python3 audit_glb_textures.py ClientWeb/src/assets/models/civic/city_hall.glb ...

期望值表 BAKED_EXPECT 直接写死 CITY_PALETTE 语义键的 hex —— 与
`__common__.py::CITY_PALETTE` 同源；改色板须同步本表（两处都在 3d_script/，同 PR 审）。
"""
import io
import json
import struct
import sys

import numpy as np
from PIL import Image

sys.path.insert(0, __file__.rsplit('/', 1)[0])
# CITY_PALETTE 在 __common__ 里，但该模块 import bpy —— 审计跑在纯 python 下，
# 不能整模块导入，直接 exec 抽取字典（表是纯字面量，零副作用）。
_PALETTE_SRC = open(__file__.rsplit('/', 1)[0] + '/__common__.py', encoding='utf-8').read()
_start = _PALETTE_SRC.index('CITY_PALETTE = {')
_end = _PALETTE_SRC.index('\n}', _start) + 2
CITY_PALETTE = eval(_PALETTE_SRC[_start:_end].split('=', 1)[1].strip())

# 模型 → {材质名子串: palette 键}（只列**烘焙槽**；平色槽不查）
BAKED_EXPECT = {
    'city_hall.glb': {
        'CityHall_Stone': 'stone_cream',
        'CityHall_StoneDark': 'stone_cream_dark',
        'CityHall_Roof': 'roof_tile_slate',
    },
    'comm_tower.glb': {
        'CommTower_Galv': 'rail_mast',
        'CommTower_GalvDark': 'steel_dark',
    },
    'water_tower.glb': {
        'WaterTower_Tank': 'tank_paint',
        'WaterTower_TankDark': 'tank_band',
    },
}


def srgb_to_linear(c: np.ndarray) -> np.ndarray:
    return np.where(c <= 0.04045, c / 12.92, ((c + 0.055) / 1.055) ** 2.4)


def hex_linear(hex_str: str) -> np.ndarray:
    s = hex_str.lstrip('#')
    rgb = np.array([int(s[i:i + 2], 16) for i in (0, 2, 4)], dtype=np.float64) / 255.0
    return srgb_to_linear(rgb)


def parse_glb(path: str) -> dict:
    """解析 GLB → (json_dict, {buffer_view_idx: bytes})。"""
    raw = open(path, 'rb').read()
    magic, version, length = struct.unpack_from('<III', raw, 0)
    assert magic == 0x46546C67, f'{path}: not a GLB'
    off, chunks = 12, {}
    while off < length:
        clen, ctype = struct.unpack_from('<II', raw, off)
        chunks.setdefault(ctype, []).append(raw[off + 8: off + 8 + clen])
        off += 8 + clen
    return json.loads(chunks[0x4E4F534A][0])  # JSON chunk


def glb_images(gltf: dict, bin_chunk: bytes) -> dict:
    """{image name/idx: PIL Image}。bufferView 解引用 + mimeType 分派。"""
    views = gltf.get('bufferViews', [])
    out = {}
    for i, img in enumerate(gltf.get('images', [])):
        data = None
        if 'bufferView' in img:
            v = views[img['bufferView']]
            data = bin_chunk[v.get('byteOffset', 0): v.get('byteOffset', 0) + v['byteLength']]
        elif 'uri' in img and img['uri'].startswith('data:'):
            import base64
            data = base64.b64decode(img['uri'].split(',', 1)[1])
        if data is not None:
            out[i] = Image.open(io.BytesIO(data)).convert('RGB')
    return out


def audit(path: str) -> list:
    fails = []
    fname = path.rsplit('/', 1)[-1]
    expect = BAKED_EXPECT.get(fname)
    if not expect:
        return [f'{fname}: 不在 BAKED_EXPECT 表（新增烘焙资产须登记）']
    gltf = parse_glb(path)
    # GLB 的 BIN chunk
    raw = open(path, 'rb').read()
    off = 12
    bin_chunk = b''
    while off < len(raw):
        clen, ctype = struct.unpack_from('<II', raw, off)
        if ctype == 0x004E4942:
            bin_chunk = raw[off + 8: off + 8 + clen]
        off += 8 + clen
    images = glb_images(gltf, bin_chunk)
    if not images:
        return [f'{fname}: 零内嵌图像 —— 烘焙通路未生效']

    for mat in gltf.get('materials', []):
        # 最长子串匹配：'CityHall_Stone' 是 'CityHall_StoneDark' 的前缀，
        # 必须取命中的最长键，否则暗槽会被拿去跟亮色基值比。
        hits = sorted((k for k in expect if k in mat['name']), key=len)
        if not hits:
            continue
        key = hits[-1]
        pal = expect[key]
        tex_info = (mat.get('pbrMetallicRoughness') or {}).get('baseColorTexture')
        if not tex_info:
            fails.append(f"{fname}/{mat['name']}: 无 baseColorTexture（白模风险）")
            continue
        img = images.get(gltf['textures'][tex_info['index']]['source'])
        if img is None:
            fails.append(f"{fname}/{mat['name']}: texture 解码失败")
            continue
        arr = np.asarray(img, dtype=np.float64) / 255.0
        lin = srgb_to_linear(arr)
        # 污渍只把像素拉向更暗的 grime ⇒ 干净基色分布在**亮侧**：取 80 分位。
        p80 = np.percentile(lin.reshape(-1, 3), 80, axis=0)
        want = hex_linear(CITY_PALETTE[pal])
        for ch, (m, w) in enumerate(zip(p80, want)):
            if abs(m - w) > 0.08 * max(w, 1e-4) and abs(m - w) > 0.012:
                fails.append(
                    f"{fname}/{mat['name']} ch{ch}: p80 linear={m:.3f} "
                    f"期望={w:.3f}（{pal}={CITY_PALETTE[pal]}）偏差超 8%")
    # 期望表里每个键至少命中一个材质（防改名后静默漏检）
    mat_names = [m['name'] for m in gltf.get('materials', [])]
    for key in expect:
        if not any(key in n for n in mat_names):
            fails.append(f'{fname}: 期望槽 {key} 未在材质表中出现（改名/丢失）')
    return fails


def main() -> int:
    if len(sys.argv) < 2:
        print('用法: audit_glb_textures.py <glb>...', file=sys.stderr)
        return 2
    all_fails = []
    for p in sys.argv[1:]:
        all_fails += audit(p)
    if all_fails:
        print('❌ 贴图审计失败：')
        for f in all_fails:
            print('  -', f)
        return 1
    print('✅ 贴图审计通过：%d 件' % len(sys.argv[1:]))
    return 0


if __name__ == '__main__':
    sys.exit(main())
