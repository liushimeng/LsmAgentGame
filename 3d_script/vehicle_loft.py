"""
vehicle_loft — 车身截面放样共享库（批次 41 · 车辆真实感 A0）。

核心思路：沿车长方向布「截面站」，每站是一个**超椭圆**截面，from_pydata 桥接
成连续车身面 —— 替代 v19 的「两盒叠加」积木车身，产出有弧线车顶 / 坡度引擎盖 /
内收座舱（tumblehome）的真实轮廓。

超椭圆（superellipse）：
    |y/a|^n + |(z-zc)/b|^n = 1
  · n = 2   → 椭圆（座舱玻璃体的弧顶）
  · n ≥ 4   → 圆角矩形（下车身：侧面竖直、顶底平）
  同一站列的每站 n_pts 相同 ⇒ 桥接面一一对应，无三角化歧义。

坐标规约（与既有 build_vehicle_*.py 一致）：
  · Blender 原生 Z-up：x = 车长（前 +）、y = 车宽（右 +）、z = 高度（地面 z=0）。
  · 世界单位 1 = 10 m（cityScale.METERS_PER_UNIT），0.46 = 真实 4.6 m。
  · 导出后 glTF X=车长 / Y=车高（minY=0）/ Z=车宽（Yup 转换）。

⚠ 本库用 from_pydata 直接构造 mesh（__common__.py「避免直接构造」的指引针对
  简单盒件；放样体没有 primitive 等价物）。法线统一走编辑模式 recalc +
  shade_smooth，桥接绕向错误由 recalc 兜底。

公开 API:
  super_loop(n_pts, a, b, zc, n)       — 超椭圆截面点列 [(y, z)]（CCW，+y 起）
  loft_stations(stations, n_pts, name) — 桥接截面站 → bpy.types.Object（含端帽）
  wheel_well_cut(body, cx, wheel_z, r, width) — boolean 减出轮拱开口（贯穿两侧）
  shade_smooth(obj)                    — 平滑着色 + 法线朝外
  build_wheel(prefix, x, y, z, r, tire_w) — 胎 + 毂盘 + 5 辐 + 内衬盘（单轮，未赋材质）
"""
import math

import bpy

from __common__ import make_box, make_cylinder


def _spow(v: float, p: float) -> float:
    """带符号幂（superellipse 参数化用）。"""
    return math.copysign(abs(v) ** p, v)


def super_loop(n_pts: int, a: float, b: float, zc: float, n: float):
    """超椭圆截面点列 [(y, z)]，t 从 0 到 2π 均布，CCW（+y 起逆时针）。

    a = 半宽（y 向）、b = 半高（z 向）、zc = 高度中心、n = 方次（2=椭圆 / ≥4=圆角矩形）。
    """
    pts = []
    p = 2.0 / n
    for i in range(n_pts):
        t = 2.0 * math.pi * i / n_pts
        y = a * _spow(math.cos(t), p)
        z = zc + b * _spow(math.sin(t), p)
        pts.append((y, z))
    return pts


def shade_smooth(obj) -> None:
    """平滑着色 + 法线统一朝外（桥接绕向错误由 recalc 兜底）。"""
    bpy.ops.object.select_all(action='DESELECT')
    obj.select_set(True)
    bpy.context.view_layer.objects.active = obj
    bpy.ops.object.mode_set(mode='EDIT')
    bpy.ops.mesh.select_all(action='SELECT')
    bpy.ops.mesh.normals_make_consistent(inside=False)
    bpy.ops.object.mode_set(mode='OBJECT')
    bpy.ops.object.shade_smooth()


def loft_stations(stations, n_pts: int, name: str):
    """桥接截面站 → Object（侧面 quad + 两端 n 边形端帽）。

    stations: [{'x', 'a', 'b', 'zc', 'n'}]，按 x 升序或降序皆可（桥接方向一致即可）。
    顶点布局：station k 的第 i 点 → 索引 k*n_pts + i。
    """
    verts = []
    for s in stations:
        for (y, z) in super_loop(n_pts, s['a'], s['b'], s['zc'], s['n']):
            verts.append((s['x'], y, z))
    faces = []
    for k in range(len(stations) - 1):
        for i in range(n_pts):
            j = (i + 1) % n_pts
            faces.append((k * n_pts + i, k * n_pts + j,
                          (k + 1) * n_pts + j, (k + 1) * n_pts + i))
    # 端帽：首站 / 末站各一个 n 边形（法线由 recalc 统一）
    faces.append(tuple(range(n_pts)))
    last = (len(stations) - 1) * n_pts
    faces.append(tuple(range(last, last + n_pts)))

    mesh = bpy.data.meshes.new(name + '_Mesh')
    mesh.from_pydata(verts, [], faces)
    mesh.validate()
    obj = bpy.data.objects.new(name, mesh)
    bpy.context.collection.objects.link(obj)
    shade_smooth(obj)
    return obj


def wheel_well_cut(body, cx: float, wheel_z: float, r: float, width: float,
                   tag: str = 'Arch') -> None:
    """在 body 上 boolean 减出一个轮拱开口（圆柱轴向 = 车宽 y，贯穿两侧）。

    cx = 轮心 x（车长向）、wheel_z = 轮心高、r = 开口半径（= 胎半径 + 8~12 mm）、
    width = 圆柱长度（须 ≥ 车宽，保证两侧都切开）。
    """
    cyl = make_cylinder(f'_cut_{tag}_{cx:+.3f}', r, r, width, 24, (cx, 0.0, wheel_z),
                        rot=(math.pi / 2, 0.0, 0.0))
    mod = body.modifiers.new(f'cut_{tag}', 'BOOLEAN')
    mod.operation = 'DIFFERENCE'
    mod.solver = 'EXACT'
    mod.object = cyl
    bpy.ops.object.select_all(action='DESELECT')
    body.select_set(True)
    bpy.context.view_layer.objects.active = body
    bpy.ops.object.modifier_apply(modifier=mod.name)
    bpy.data.objects.remove(cyl, do_unlink=True)
    shade_smooth(body)


def build_wheel(prefix: str, x: float, y: float, z: float, r: float,
                tire_w: float):
    """单轮几何组件（**不赋材质**，材质由调用方按合并策略统一处理）。

    返回 [tire, hub, spoke×5, inner] 部件列表：
      tire  — 24 段圆柱胎，轴沿车宽 y，轮心 (x, y, z)；
      hub   — 毂盘（r×0.52，16 段），贴轮外侧；
      spoke — 5 根径向辐条（box），绕毂盘均布，同样贴外侧；
      inner — 胎侧内衬暗盘（r×0.95），遮轴孔（贴外侧，在辐条后）。

    「外侧」= |y| 更大的一侧（sign(y) 决定）。
    """
    parts = []
    out = 1 if y >= 0 else -1
    y_out = y + out * tire_w / 2.0   # 轮外侧面
    # 胎
    parts.append(make_cylinder(f'{prefix}_Tire', r, r, tire_w, 24, (x, y, z),
                               rot=(math.pi / 2, 0.0, 0.0)))
    # 毂盘（贴外侧面）
    hub_r = r * 0.52
    parts.append(make_cylinder(f'{prefix}_Hub', hub_r, hub_r, 0.004, 16,
                               (x, y_out - out * 0.002, z),
                               rot=(math.pi / 2, 0.0, 0.0)))
    # 5 辐条：径向 box，长 = (r*0.82 - hub_r*0.5)，绕 y 轴旋转 (π/2 - ang)
    spoke_len = r * 0.82 - hub_r * 0.5
    rr = (hub_r * 0.5 + r * 0.82) / 2.0
    for i in range(5):
        ang = 2 * math.pi * i / 5
        spoke = make_box(f'{prefix}_Spoke{i}', (0.007, 0.004, spoke_len),
                         (x + rr * math.cos(ang), y_out - out * 0.0035,
                          z + rr * math.sin(ang)))
        spoke.rotation_euler = (0.0, math.pi / 2 - ang, 0.0)
        parts.append(spoke)
    # 胎侧内衬暗盘（最外，遮轴孔）
    parts.append(make_cylinder(f'{prefix}_Inner', r * 0.95, r * 0.95, 0.002, 20,
                               (x, y_out - out * 0.0005, z),
                               rot=(math.pi / 2, 0.0, 0.0)))
    return parts
