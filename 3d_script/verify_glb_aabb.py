#!/usr/bin/env python3
"""
verify_glb_aabb — 虚拟城市 GLB 轴向 / 贴地 / 尺度 / 节点 identity 自检（纯 stdlib）。

背景（2026-09-27 批次 19 GLB 轴向+尺度回溯修正）：
  批次 19 的 11 个 GLB 由旧「作者自定 Y-up」脚本导出 —— bpy.ops.export_scene.gltf 的
  Yup 转换 (x,y,z)_b → (x,z,-y)_g 把作者的"上"映射到 glTF -Z ⇒ three.js 里整个模型侧躺；
  同时尺度过小（玩具车 1 m）或过大（楼体 1.5~2.5×）。本脚本是修正后的可复现验收工具。

判据（逐条打印实测 / 目标 / 偏差，任一 FAIL ⇒ 退出码 1）：
  1. 直立   —— 主包围轴 = Y（唯一例外：ocean/cargo_ship 与 ocean/sailboat 长轴 = X 属正确
               摆放；nature/snow_mountain 为地形基准尺度，只报不判）。
  2. 贴地   —— minY = 0（cargo_ship / sailboat 为吃水线，允许 < 0）。
  3. 尺寸   —— 对上表中的 12 个目标件，三条边逐一比对（容差 ±5%）。
  4. 节点   —— 无节点携带非 identity 的 scale / rotation（蒙皮 mesh 顶点受 bind matrix 支配、
               骨骼节点天然带旋转，故 joint 节点与带 skin 的 mesh 节点豁免）；平移只允许出现在
               上述两类节点上。消费端 <Model> 零旋转零 scale 直挂，靠的就是这条。

用法：
  python3 3d_script/verify_glb_aabb.py                # 扫 ClientWeb/src/assets/models/**.glb
  python3 3d_script/verify_glb_aabb.py path/to.glb    # 只看指定文件
"""
import glob
import json
import math
import os
import struct
import sys

# 模型根目录：默认仓库内的 ClientWeb/src/assets/models；可用 GLB_MODELS_DIR 覆盖
# （回归自检用：把 build_*.py 重导到临时目录后仍按 `<类别>/<名>.glb` 的 rel 命中目标表）。
MODELS_DIR = os.environ.get('GLB_MODELS_DIR') or os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
    'ClientWeb', 'src', 'assets', 'models',
)

# ── 目标表（世界单位；1 单位 = 10 m）────────────────────────────────────────
# 来源：前端程序化 fallback 的真实尺寸（civic/*Fallback + props/Vehicle.tsx VEHICLE_DIMS）。
# x/y/z 为 None 表示该轴不参与判据（仅报实测）。
TARGETS = {
    # 车辆：长轴 = X 属正确摆放（真实车长 4.6~12 m 必然大于车高），故 axis='x_flat'
    'vehicles/sedan.glb':     dict(x=0.460, y=0.145, z=0.182, axis='x_flat', pivot='轮底贴地 minY=0，X/Z 居中'),
    'vehicles/taxi.glb':      dict(x=0.470, y=0.150, z=0.185, axis='x_flat', pivot='轮底贴地 minY=0，X/Z 居中'),
    'vehicles/bus.glb':       dict(x=1.200, y=0.320, z=0.255, axis='x_flat', pivot='轮底贴地 minY=0，X/Z 居中'),
    'vehicles/truck.glb':     dict(x=0.850, y=0.340, z=0.250, axis='x_flat', pivot='轮底贴地 minY=0，X/Z 居中'),
    'characters/pedestrian_walk.glb': dict(x=0.055, y=0.167, z=0.035, pivot='身高 1.67 m，脚底 minY=0，X/Z 居中'),
    'civic/city_hall.glb':    dict(x=1.440, y=2.000, z=0.840, pivot='地面中心 minY=0'),
    'civic/comm_tower.glb':   dict(x=1.000, y=2.050, z=1.000, pivot='地面中心 minY=0'),
    'civic/water_tower.glb':  dict(x=0.360, y=1.330, z=0.360, pivot='地面中心 minY=0'),
    'civic/police_station.glb': dict(x=1.210, y=0.660, z=0.700, pivot='地面中心 minY=0'),
    'civic/fire_station.glb': dict(x=1.810, y=0.850, z=0.800, pivot='地面中心 minY=0'),
    # road_props.glb 一文件 3 盏路灯沿 X 排开 ⇒ 整体长轴 = X 属正确；直立性逐件判：
    # 至少 3 个「主包围轴 = Y 且落地」的立体件（= 3 根竖杆），横臂/灯头不计。
    'road/road_props.glb':    dict(x=3.130, y=1.200, z=None, axis='per_part_y', min_parts=3,
                                   pivot='3 路灯沿 X 总跨；每盏杆竖直、地面 minY=0'),
    # trash_can.glb 一个文件含 2 只独立桶（x=0 / +0.12），目标值是**单桶**尺寸，
    # 故用 per_part 模式：对每个节点各求 AABB，取各轴最大值（而非整体包围盒）。
    'road/trash_can.glb':     dict(x=0.050, y=0.100, z=0.050, per_part=True, axis='per_part_y',
                                   min_parts=2, pivot='单桶 桶底 minY=0（两桶各自 minY=0）'),
}

# 直立判据的例外：长轴落在水平轴属正确摆放。
DOMINANT_AXIS_EXEMPT = {'ocean/cargo_ship.glb': 'X', 'ocean/sailboat.glb': 'X'}
# 地形基准：只报实测，不参与直立 / 贴地判据。
TERRAIN_BASELINE = {'nature/snow_mountain.glb'}
# 吃水线：minY < 0 属正确。
WATERLINE = {'ocean/cargo_ship.glb', 'ocean/sailboat.glb'}

SIZE_TOL = 0.05        # ±5%
EPS = 1e-4

_COMPONENT = {5120: ('b', 1), 5121: ('B', 1), 5122: ('h', 2),
              5123: ('H', 2), 5125: ('I', 4), 5126: ('f', 4)}
_NCOMP = {'SCALAR': 1, 'VEC2': 2, 'VEC3': 3, 'VEC4': 4, 'MAT4': 16}


# ── GLB 解析 ──────────────────────────────────────────────────────────────
def read_glb(path):
    with open(path, 'rb') as fh:
        data = fh.read()
    magic, _ver, length = struct.unpack('<III', data[:12])
    if magic != 0x46546C67:
        raise ValueError('not a GLB: ' + path)
    off, js, blobs = 12, None, []
    while off < length:
        clen, ctype = struct.unpack('<II', data[off:off + 8])
        chunk = data[off + 8:off + 8 + clen]
        if ctype == 0x4E4F534A:
            js = json.loads(chunk.decode('utf-8'))
        elif ctype == 0x004E4942:
            blobs.append(chunk)
        off += 8 + clen
    return js, (blobs[0] if blobs else b'')


def read_accessor(js, blob, idx):
    """返回 [(x,y,z), ...]（仅 POSITION，VEC3 float）。"""
    acc = js['accessors'][idx]
    bv = js['bufferViews'][acc['bufferView']]
    fmt, size = _COMPONENT[acc['componentType']]
    n = _NCOMP[acc['type']]
    start = bv.get('byteOffset', 0) + acc.get('byteOffset', 0)
    stride = bv.get('byteStride') or (size * n)
    out = []
    for i in range(acc['count']):
        o = start + i * stride
        vals = struct.unpack_from('<' + fmt * n, blob, o)
        out.append(vals[:3] if n >= 3 else (vals[0], 0.0, 0.0))
    return out


def _mat_mul(a, b):
    return [[sum(a[i][k] * b[k][j] for k in range(4)) for j in range(4)] for i in range(4)]


def _mat_apply(m, v):
    x, y, z = v
    return (m[0][0] * x + m[0][1] * y + m[0][2] * z + m[0][3],
            m[1][0] * x + m[1][1] * y + m[1][2] * z + m[1][3],
            m[2][0] * x + m[2][1] * y + m[2][2] * z + m[2][3])


IDENT = [[1.0 if i == j else 0.0 for j in range(4)] for i in range(4)]


def node_local_matrix(node):
    """nodes[i] 的局部矩阵（matrix 优先，否则 TRS 合成）。"""
    if 'matrix' in node:
        m = node['matrix']          # glTF 列主序
        return [[m[0], m[4], m[8], m[12]],
                [m[1], m[5], m[9], m[13]],
                [m[2], m[6], m[10], m[14]],
                [m[3], m[7], m[11], m[15]]]
    tx, ty, tz = node.get('translation', (0.0, 0.0, 0.0))
    qx, qy, qz, qw = node.get('rotation', (0.0, 0.0, 0.0, 1.0))
    sx, sy, sz = node.get('scale', (1.0, 1.0, 1.0))
    r = [[1 - 2 * (qy * qy + qz * qz), 2 * (qx * qy - qz * qw), 2 * (qx * qz + qy * qw)],
         [2 * (qx * qy + qz * qw), 1 - 2 * (qx * qx + qz * qz), 2 * (qy * qz - qx * qw)],
         [2 * (qx * qz - qy * qw), 2 * (qy * qz + qx * qw), 1 - 2 * (qx * qx + qy * qy)]]
    m = [[r[i][j] * (sx, sy, sz)[j] for j in range(3)] + [0.0] for i in range(3)]
    m.append([0.0, 0.0, 0.0, 1.0])
    m[0][3], m[1][3], m[2][3] = tx, ty, tz
    return m


def is_identity(m):
    for i in range(4):
        for j in range(4):
            if abs(m[i][j] - (1.0 if i == j else 0.0)) > EPS:
                return False
    return True


def prim_local_aabb(js, blob, prim):
    """单个 primitive 的局部 AABB（优先 accessor min/max，缺失则解码顶点）。"""
    idx = prim.get('attributes', {}).get('POSITION')
    if idx is None:
        return None
    acc = js['accessors'][idx]
    if 'min' in acc and 'max' in acc:
        return tuple(acc['min']), tuple(acc['max'])
    verts = read_accessor(js, blob, idx)
    if not verts:
        return None
    lo = [min(v[i] for v in verts) for i in range(3)]
    hi = [max(v[i] for v in verts) for i in range(3)]
    return tuple(lo), tuple(hi)


def world_aabb(js, blob):
    """遍历 scene 节点树，同时求两个口径的包围盒：

      - **世界口径**：accessor AABB 经节点 TRS 链变换后并起来（= 浏览器 Box3 所见）；
      - **几何口径**：直接并 accessor AABB，**不施加任何节点变换**。

    两者相等 ⇔ 节点链为 identity ⇔ 「几何顶点本身就是最终世界尺寸」。
    （HEAD 的 trash_can.glb 就是两者不等的反例：几何 [2,1,2] × 节点 scale 才是 0.078。）
    """
    scene = js['scenes'][js.get('scene', 0)]
    joints = set()
    for sk in js.get('skins', []):
        joints.update(sk.get('joints', []))
    # 动画驱动的节点 = 骨骼节点，天然带旋转/平移，豁免 identity 判据
    anim_targets = set()
    for a in js.get('animations', []):
        for ch in a.get('channels', []):
            if 'node' in ch.get('target', {}):
                anim_targets.add(ch['target']['node'])
    details, node_boxes, geom_node_boxes, bad_nodes, soft_nodes = [], [], [], [], []
    acc_lo, acc_hi = [math.inf] * 3, [-math.inf] * 3
    geo_lo, geo_hi = [math.inf] * 3, [-math.inf] * 3

    def visit(ni, parent):
        node = js['nodes'][ni]
        m = _mat_mul(parent, node_local_matrix(node))
        # 蒙皮 mesh 的顶点受 inverseBindMatrices 支配，其规范里忽略其节点变换
        skinned = 'skin' in node
        name = node.get('name', 'node%d' % ni)
        if ni not in joints and ni not in anim_targets and not skinned:
            s = node.get('scale', (1.0, 1.0, 1.0))
            if any(abs(v - 1.0) > EPS for v in s):
                # 硬判据：节点 scale ≠ 1 ⇒ 尺寸挂在节点变换上
                # （HEAD 的 trash_can.glb 正是 scale=[0.0375,0.09,0.0375] × 单位几何）
                bad_nodes.append('%s(scale=%s)' % (name, [round(v, 4) for v in s]))
            elif not is_identity(node_local_matrix(node)):
                # 软判据：仅 rotation / translation 偏离（骨骼节点已由动画目标集合豁免）
                soft_nodes.append(name)
        if 'mesh' in node:
            nlo, nhi = [math.inf] * 3, [-math.inf] * 3
            gnlo, gnhi = [math.inf] * 3, [-math.inf] * 3
            for prim in js['meshes'][node['mesh']].get('primitives', []):
                box = prim_local_aabb(js, blob, prim)
                if box is None:
                    continue
                lo, hi = box
                for i in range(3):        # 几何（accessor）口径：不施加任何节点变换
                    geo_lo[i] = min(geo_lo[i], lo[i])
                    geo_hi[i] = max(geo_hi[i], hi[i])
                    gnlo[i] = min(gnlo[i], lo[i])
                    gnhi[i] = max(gnhi[i], hi[i])
                corners = [(lo[0] if not (k & 1) else hi[0],
                            lo[1] if not (k & 2) else hi[1],
                            lo[2] if not (k & 4) else hi[2]) for k in range(8)]
                ws = [_mat_apply(m, c) for c in corners]
                plo = [min(w[i] for w in ws) for i in range(3)]
                phi = [max(w[i] for w in ws) for i in range(3)]
                for i in range(3):
                    acc_lo[i] = min(acc_lo[i], plo[i])
                    acc_hi[i] = max(acc_hi[i], phi[i])
                    nlo[i] = min(nlo[i], plo[i])
                    nhi[i] = max(nhi[i], phi[i])
                details.append((name, plo, phi))
            if nlo[0] < math.inf:
                # 节点级并集（单节点多 primitive 时用于"单件"判据，如垃圾桶一个 mesh 分 3 材质）
                node_boxes.append((name, nlo, nhi))
            if gnlo[0] < math.inf:
                geom_node_boxes.append((name, gnlo, gnhi))
        for c in node.get('children', []):
            visit(c, m)

    for ni in scene['nodes']:
        visit(ni, IDENT)
    return dict(lo=acc_lo, hi=acc_hi, details=details, node_boxes=node_boxes,
                bad_nodes=bad_nodes, soft_nodes=soft_nodes,
                exempt=sorted(joints | anim_targets),
                geom_lo=geo_lo, geom_hi=geo_hi, geom_node_boxes=geom_node_boxes)


# ── 判据 ─────────────────────────────────────────────────────────────────
def check(path, rel):
    js, blob = read_glb(path)
    box = world_aabb(js, blob)
    lo, hi, details, node_boxes = box['lo'], box['hi'], box['details'], box['node_boxes']
    bad, soft_nodes = box['bad_nodes'], box['soft_nodes']
    if not details:
        return ['FAIL  无几何'], None
    size = [hi[i] - lo[i] for i in range(3)]
    g_lo, g_hi = list(box['geom_lo']), list(box['geom_hi'])
    problems, notes = [], []
    target = TARGETS.get(rel)
    exempt_axis = DOMINANT_AXIS_EXEMPT.get(rel)
    terrain = rel in TERRAIN_BASELINE

    # 判据 (b)：几何（accessor）口径 —— 不加任何节点变换直接量顶点。
    # 世界的世界口径必须与它逐轴一致，否则说明「尺寸挂在节点变换上」
    # （HEAD 的 trash_can.glb：几何单位尺度 × node scale 0.0375 → 消费端 collectPairs
    #   抵消 scale 后按 20 m 直径渲染。这就是"垃圾桶比车还大"的真因。）
    size_geo = [g_hi[i] - g_lo[i] for i in range(3)]
    geo_lo = list(g_lo)
    if target and target.get('per_part'):
        # 一文件多件（trash_can 双桶）：按节点（= 单只桶，含其 3 个材质 primitive 的并集）
        # 逐件求 AABB，取各轴最大值 + minY 最小值
        size = [max(b[2][i] - b[1][i] for b in node_boxes) for i in range(3)]
        size_geo = [max(b[2][i] - b[1][i] for b in box['geom_node_boxes']) for i in range(3)]
        lo_fix = list(lo)
        lo_fix[1] = min(b[1][1] for b in node_boxes)
        lo = lo_fix
        geo_lo[1] = min(b[1][1] for b in box['geom_node_boxes'])
        notes.append('per-part 判据：本文件含 %d 个节点（%d 材质 primitive），'
                     '取单节点最大尺寸 / 最低 minY' % (len(node_boxes), len(details)))
    skew = [abs(size[i] - size_geo[i]) for i in range(3)]
    if max(skew) > 1e-6:
        problems.append('FAIL  几何口径与世界口径不等（Δ=%.4f/%.4f/%.4f）—— 尺寸挂在节点变换上'
                        % (skew[0], skew[1], skew[2]))
    else:
        notes.append('几何(accessor)口径 ≡ 世界口径（Δ < 1e-6）⇒ 几何顶点本身即最终世界尺寸')

    # 1. 直立（高度轴落在 glTF Y）
    dom = max(range(3), key=lambda i: size[i])
    dom_name = 'XYZ'[dom]
    mode = 'x_flat' if exempt_axis else (target or {}).get('axis', 'target' if target else 'y')
    # 目标件默认直立性 = 「Y 实测 = 声明高度 ±5%」+「minY = 0」（二者合起来即"站得住"），
    # 由尺寸 / 贴地判据承载，主包围轴只作提示（18 m 宽的消防站长轴本来就是 X）。
    if terrain:
        notes.append('地形基准：直立判据跳过（主包围轴 %s）' % dom_name)
    elif mode == 'target':
        notes.append('直立：高度轴 = Y（实测 %.3f vs 目标高 %.3f，见 Y 判据）；主包围轴 = %s'
                     % (size[1], target['y'], dom_name))
    elif mode == 'x_flat':
        # 长轴 = X 属正确摆放（船体 / 车长）：真实车长 4.6~12 m 必然大于车高，
        # 故直立性改由 Y 尺寸判据保证（Y 实测 = 目标车高才判 ok；侧躺模型的 Y 会落到车宽/车长上）
        if dom_name != 'X':
            problems.append('FAIL  直立：主包围轴 = %s（长轴件应为 X：船体/车长）' % dom_name)
        else:
            notes.append('长轴 X 属正确摆放（船体/车长）；直立性由 Y 尺寸判据保证（Y = 高度轴）')
    elif mode == 'per_part_y':
        # 「立体件」= 主包围轴 = Y、落地（minY=0）、且高度 ≥ 目标高 50% 的件
        h_floor = 0.5 * ((target or {}).get('y') or 0.5)
        upright = [d for d in details
                   if max(range(3), key=lambda i: (d[2][i] - d[1][i])) == 1
                   and abs(d[1][1]) <= 1e-3
                   and (d[2][1] - d[1][1]) >= h_floor]
        need = (target or {}).get('min_parts', 1)
        if len(upright) < need:
            problems.append('FAIL  直立：落地且主包围轴 = Y 的立体件仅 %d 个（需 ≥ %d）'
                            % (len(upright), need))
        else:
            notes.append('直立：%d 个立体件主包围轴 = Y 且落地（需 ≥ %d）：%s'
                         % (len(upright), need, ', '.join(d[0] for d in upright)))
    elif dom_name != 'Y':
        problems.append('FAIL  直立：主包围轴 = %s（应为 Y；X=%.3f Y=%.3f Z=%.3f）'
                        % (dom_name, size[0], size[1], size[2]))

    # 2. 贴地
    if not terrain:
        if rel in WATERLINE:
            if lo[1] >= 0:
                problems.append('FAIL  贴地：吃水线类应 minY < 0，实测 %.4f' % lo[1])
        elif abs(lo[1]) > 1e-3:
            problems.append('FAIL  贴地：minY = %.4f（应为 0）' % lo[1])

    # 3. 尺寸（(a) 世界口径 与 (b) 几何口径 双查 —— 二者一致时数值相同）
    if target:
        for i, key in enumerate('xyz'):
            want = target[key]
            if want is None:
                notes.append('%s 轴不设判据：实测 %.3f' % (key.upper(), size[i]))
                continue
            dev = size[i] - want
            rel_dev = abs(dev) / want
            tag = 'ok  ' if rel_dev <= SIZE_TOL else 'FAIL'
            notes.append('%s 实测 %.4f / 目标 %.3f  偏差 %+.4f (%+.2f%%)  %s'
                         % (key.upper(), size[i], want, dev, rel_dev * 100, tag))
            notes.append('   └ 几何(accessor)口径 %.4f（Δ %+.4f）'
                         % (size_geo[i], size_geo[i] - want))
            if rel_dev > SIZE_TOL:
                problems.append('FAIL  尺寸 %s：实测 %.4f 目标 %.3f（偏差 %+.2f%%）'
                                % (key.upper(), size[i], want, rel_dev * 100))
            if abs(size_geo[i] - want) / want > SIZE_TOL:
                problems.append('FAIL  几何口径尺寸 %s：%.4f 目标 %.3f'
                                % (key.upper(), size_geo[i], want))

    # 4. 节点 identity（硬判据 = scale；软判据 = 非骨骼节点的 rotation/translation）
    if bad:
        problems.append('FAIL  节点 scale 非 1 ⇒ 尺寸挂在节点变换上：%s' % ', '.join(bad))
    else:
        notes.append('节点 scale：全部 1（%d 节点；%d 个骨骼/蒙皮节点按规范豁免）'
                     % (len(js['nodes']), len(box['exempt'])))
    if soft_nodes:
        notes.append('提示（软判据，非 FAIL）：%d 个非骨骼节点带 rotation/translation：%s'
                     % (len(soft_nodes), ', '.join(soft_nodes[:6])))

    anims = js.get('animations', [])
    if anims:
        notes.append('动画 clip：%s（%d channels）'
                     % (', '.join(a.get('name', '?') for a in anims),
                        sum(len(a['channels']) for a in anims)))
    return problems, dict(size=size, lo=lo, hi=hi, notes=notes, details=details,
                          geom_lo=geo_lo, geom_hi=g_hi, size_geo=size_geo,
                          kb=os.path.getsize(path) / 1024.0, target=target, rel=rel)


def main(argv):
    if argv:
        files = sorted(os.path.abspath(p) for p in argv)
    else:
        files = sorted(glob.glob(os.path.join(MODELS_DIR, '*', '*.glb')))
    if not files:
        print('no .glb found')
        return 2
    total_fail = 0
    print('=' * 108)
    print('verify_glb_aabb — GLB 轴线 / 贴地 / 尺度 / 节点 identity 自检（1 世界单位 = 10 m）')
    print('判据：(a) 世界包围盒 = 目标 ±5%  (b) 几何 accessor 包围盒 = 目标（≡(a) 才说明'
          '尺寸没挂在节点变换上）  (c) 节点 scale 全 1（骨骼/蒙皮节点豁免）  (d) 直立 / 贴地')
    print('=' * 108)
    for path in files:
        rel = os.path.relpath(path, MODELS_DIR).replace(os.sep, '/')
        try:
            problems, info = check(path, rel)
        except Exception as exc:                       # noqa: BLE001
            print('\n### %-34s 解析失败：%s' % (rel, exc))
            total_fail += 1
            continue
        mark = 'PASS' if not problems else 'FAIL'
        if info is None:
            print('\n### %-34s %s' % (rel, mark))
            total_fail += len(problems)
            continue
        lo, hi, size = info['lo'], info['hi'], info['size']
        print('\n### %-34s [%s]  %6.1f KB' % (rel, mark, info['kb']))
        print('    包围盒   X %.4f  Y %.4f  Z %.4f     minY %+.4f   (X %.3f..%.3f / Z %.3f..%.3f)'
              % (size[0], size[1], size[2], lo[1], lo[0], hi[0], lo[2], hi[2]))
        if info['target']:
            tz = info['target']['z']
            print('    目标     X %.3f  Y %.3f  Z %s   pivot: %s'
                  % (info['target']['x'], info['target']['y'],
                     ('%.3f' % tz) if tz is not None else '--',
                     info['target']['pivot']))
        for n in info['notes']:
            print('    · %s' % n)
        for p in problems:
            print('    ✗ %s' % p)
        if info['kb'] > 500:
            print('    ✗ FAIL  体积 %.1f KB > 500 KB（CLAUDE.md §27.3）' % info['kb'])
            problems.append('size')
        total_fail += len(problems)
        # 逐件明细（便于定位是哪一件定义包围盒）
        if os.environ.get('VERIFY_VERBOSE'):
            for name, nlo, nhi in info['details']:
                print('      └ %-28s X %.3f..%.3f  Y %.3f..%.3f  Z %.3f..%.3f'
                      % (name, nlo[0], nhi[0], nlo[1], nhi[1], nlo[2], nhi[2]))
    print('\n' + '=' * 108)
    print('扫描 %d 个 GLB，%s' % (len(files), '全部通过 ✓' if not total_fail else '%d 项不符合 ✗' % total_fail))
    return 1 if total_fail else 0


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
