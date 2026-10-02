/**
 * ConstructionSite — 施工工地 + 格构塔式起重机（16-3D城市WebGL质感与城市补全 · 阶段 T）。
 *
 * 批次 50「施工工地真实感」重制为 `civic/construction_site.glb`。
 *
 * ── 重制理由（详见 `lag_docs/虚拟城市/已实现/50-施工工地真实感/01-方案设计.md`）──
 *   批次 18-AA 的程序化版本有两类硬伤：
 *
 *   1) **★ 围挡压在方格骨干主路里 4.0 m**：`SITE_X=28, SITE_W=3.4` ⇒ 西侧围挡在
 *      x=26.30，而 `arterial-z26` 路幅是 x∈[25.3, 26.7] —— 一段工程黄围挡站在
 *      主路中央。**几何没错，是落位错了**，tsc / 构建 / 出图全部无感。
 *      新落位由 `scripts/ci/check_site_clearance.mjs` 的净距数值解给出。
 *
 *   2) **塔吊差了一个数量级**：旧版是「1.6 m 见方 × 9 m 高的一根实心柱」+
 *      「一根 7.2 m 的 10 cm 见方细杆当起重臂」+「1 块 0.15 t 的方块当配重」，
 *      且无基础、无塔帽、无拉索、无吊钩组。而方圆 QTZ80(TC6010) 的实际参数是：
 *      标准节 1.8×1.8×2.5 m **格构**、14~18 节（起升高度 40.5~46.2 m、总高约
 *      55.9 m）、起重臂 50/55/60 m **三角形桁架**、平衡臂 12.4~13.4 m、
 *      配重 11.75~18 t（5~7 块）、承台 4.0×4.0×1.2 m。
 *
 * ── 落位（净距数值解，不是拍脑袋）─────────────────────────────────────
 *   判据 `净距 = 点到路段中心线距离 − 路半宽`，场坪按 25×25 网格采样：
 *     旧 (28, −1) 34×26 m  ⇒  **−4.0 m**（围挡压在 arterial-z26 上）
 *     新 (30.5, −1.5) 50×44 m ⇒ **13.0 m**（离 arterial-z26 13 m，
 *                                        离最近区底板 cultural_creative 60.4 m）
 *   塔吊起重臂会**越出场坪**探到东侧相邻街区 —— 这是它的本职工作：
 *   臂根标高 41.8 m，远高于路面与沿线 15~25 m 建筑，规范允许越路回转。
 *   护栏 `check_site_clearance.mjs` **不检查回转扫掠**，只检查落地场坪。
 *
 * ── 坐标系 ────────────────────────────────────────────────────────────
 *   建模脚本在 Blender 的 XY 平面铺场（X = 东西 50 m、Y = 南北 44 m、Z = 高），
 *   导出 Yup 后 `three.y = Blender z`（高）、`three.z = -Blender y`（南北翻转）。
 *   场坪是 X/Z 对称的 50×44 矩形，**零旋转**直挂即可。
 *
 * ── 降级链（§27.3-3）────────────────────────────────────────────────
 *   `blenderModelsEnabled()` → `modelUrl` → GLB 载入 → 否则本文件的程序化几何
 *   （**按新尺寸重建**，§27.3-7 双路径同尺寸）。尺寸守卫 `sizeTargetFor`。
 */
import { useEffect, useMemo } from 'react';
import { boxPart, cylPart, mergeParts, type MergePart } from '@/engine3d';
import { u } from '../cityScale';
import { CivicGlbPiece } from '../civic/CivicGlb';

/** 场坪中心与尺寸（米 → 世界单位由 `u()` 换算；护栏 `check_site_clearance.mjs`
 *  从**本文件源码**取这四个常量，改这里脚本就会红）。 */
const SITE_X = 30.5;
const SITE_Z = -1.5;
const SITE_W = 5.0;
const SITE_D = 4.4;

/** 塔机关键标高（与 `build_construction_site.py` 对齐，仅供 fallback 复用）。 */
const FOOT_H = 1.2;
const MAST_H = 35.0;
const HEAD_H = 8.0;
const JIB_L = 50.0;
const CRANE_DX = -0.9;    // 回转中心相对场坪中心的 X 偏移（-9.0 m）
const CRANE_DZ = 0.6;     // 回转中心相对场坪中心的 Z 偏移（+6.0 m）

const YELLOW = '#e8b930';
const YELLOW_DARK = '#b8921f';
const HOARD = '#f0c93a';
const STEEL = '#b8bcc2';
const MUD = '#8a7658';
const CONCRETE = '#9e9c96';
const CABIN_BLUE = '#2f6fa8';
const CABIN_WINDOW = '#e0a020';
const NET_GREEN = '#2f7a3e';
const REBAR = '#7a5c40';
const AGG = '#9a9285';
const CONCRETE_DARK = '#8d8a85';

/** 程序化 fallback 的场地件（裸土 + 围挡 + 硬路 + 洗车槽）。 */
function siteFallbackParts(): MergePart[] {
  const p: MergePart[] = [];
  p.push(boxPart(u(SITE_W * 10), u(0.1), u(SITE_D * 10), 0, u(0.05), 0, MUD));
  // 围挡 4 面（南面留 7.0 m 门洞）
  const hw = u(SITE_W * 10) / 2;
  const hd = u(SITE_D * 10) / 2;
  const gate = u(7.0);
  const seg = (u(SITE_W * 10) - gate) / 2;
  const h = u(2.5);
  p.push(boxPart(u(SITE_W * 10), h, u(0.12), 0, h / 2, -hd, HOARD));
  p.push(boxPart(u(SITE_W * 10), h, u(0.12), 0, h / 2, hd, HOARD));
  p.push(boxPart(u(0.12), h, u(SITE_D * 10), -hw, h / 2, 0, HOARD));
  p.push(boxPart(u(0.12), h, u(SITE_D * 10), hw, h / 2, 0, HOARD));
  p.push(boxPart(seg, h, u(0.12), -(gate / 2 + seg / 2), h / 2, -hd, HOARD));
  p.push(boxPart(seg, h, u(0.12), gate / 2 + seg / 2, h / 2, -hd, HOARD));
  for (const sx of [-1, 1]) {
    p.push(boxPart(u(0.3), u(3.05), u(0.3), sx * gate / 2, u(1.525), -hd, YELLOW));
  }
  // 场内硬路 + 洗车槽
  for (const dz of [-hd + u(9), hd - u(8)]) {
    p.push(boxPart(u(SITE_W * 10 - 6), u(0.14), u(5.0), 0, u(0.17), dz, CONCRETE));
  }
  p.push(boxPart(u(4.2), u(0.22), u(7.0), 0, u(0.21), -hd + u(5.0), CONCRETE));
  return p;
}

/** 程序化 fallback 的塔机件（格构塔身 + 桁架臂 + 配重 + 吊钩）。 */
function craneFallbackParts(): MergePart[] {
  const p: MergePart[] = [];
  const cx = u(CRANE_DX);
  const cz = u(CRANE_DZ);
  const sec = u(2.5);
  const half = u(0.9);
  // 承台
  p.push(boxPart(u(5.2), u(FOOT_H), u(5.2), cx, u(FOOT_H / 2), cz, CONCRETE_DARK));
  p.push(boxPart(u(5.28), u(0.36), u(5.28), cx, u(0.42), cz, YELLOW_DARK));
  // 塔身：4 根通长主肢 + 逐节横撑 / 斜撑（用细杆近似）
  for (const sx of [-1, 1]) {
    for (const sz of [-1, 1]) {
      p.push(boxPart(u(0.16), u(MAST_H), u(0.16), cx + sx * half,
        u(FOOT_H) + u(MAST_H) / 2, cz + sz * half, YELLOW));
    }
  }
  for (let i = 0; i <= 14; i++) {
    const z = u(FOOT_H) + sec * i;
    p.push(boxPart(u(1.8), u(0.09), u(0.09), cx, z, cz - half, STEEL));
    p.push(boxPart(u(1.8), u(0.09), u(0.09), cx, z, cz + half, STEEL));
    p.push(boxPart(u(0.09), u(0.09), u(1.8), cx - half, z, cz, STEEL));
    p.push(boxPart(u(0.09), u(0.09), u(1.8), cx + half, z, cz, STEEL));
    if (i < 14) {
      p.push(boxPart(u(1.86), u(0.06), u(0.06), cx, z + u(1.25), cz, STEEL));
      p.push(boxPart(u(0.06), u(0.06), u(1.86), cx, z + u(1.25), cz, STEEL));
    }
  }
  const zTop = u(FOOT_H) + u(MAST_H);
  // 塔帽
  p.push(boxPart(u(2.4), u(0.16), u(2.0), cx, zTop + u(HEAD_H) - u(0.1), cz, YELLOW));
  for (const sx of [-1, 1]) {
    p.push(boxPart(u(0.9), u(HEAD_H), u(0.14), cx + sx * u(0.5), zTop + u(HEAD_H / 2), cz, YELLOW));
  }
  // 起重臂（格构近似：2 上弦 + 1 下弦 + 腹杆）
  const jz = zTop + u(5.6);
  const jd = jz - u(1.7);
  for (const sy of [-1, 1]) {
    p.push(boxPart(u(JIB_L), u(0.16), u(0.16), cx + u(JIB_L / 2), jz, cz + sy * u(0.75), YELLOW));
  }
  p.push(boxPart(u(JIB_L), u(0.16), u(0.16), cx + u(JIB_L / 2), jd, cz, YELLOW));
  for (let i = 0; i <= 10; i++) {
    const x = cx + u(i * 5.0);
    p.push(boxPart(u(0.08), u(1.7), u(0.08), x, (jz + jd) / 2, cz, STEEL));
    p.push(boxPart(u(0.07), u(0.07), u(1.5), x, jz, cz, STEEL));
  }
  p.push(boxPart(u(0.6), u(1.9), u(2.0), cx + u(JIB_L), (jz + jd) / 2, cz, YELLOW));
  // 平衡臂 + 配重
  const cz2 = zTop + u(4.2);
  p.push(boxPart(u(13.0), u(0.14), u(1.4), cx - u(6.5), cz2, cz, YELLOW));
  for (let i = 0; i < 4; i++) {
    p.push(boxPart(u(2.4), u(0.55), u(1.9), cx - u(11.9), cz2 - u(1.4 + (i % 2) * 1.94),
      cz + ((i < 3 ? -1 : 0) + (i < 3 ? i : 0)) * 0.6, CONCRETE_DARK));
  }
  // 拉索
  for (const fx of [JIB_L / 3, (JIB_L * 2) / 3]) {
    p.push(boxPart(u(Math.hypot(fx, HEAD_H - 5.3)), u(0.07), u(0.07),
      cx + u(fx / 2), zTop + u((HEAD_H + 5.6) / 2), cz, STEEL));
  }
  // 小车 + 吊钩组
  p.push(boxPart(u(1.6), u(0.55), u(1.8), cx + u(30), jd - u(0.42), cz, STEEL));
  p.push(boxPart(u(0.05), u(24.7), u(0.05), cx + u(30), jd - u(12.7), cz, STEEL));
  p.push(boxPart(u(1.1), u(0.85), u(0.55), cx + u(30), jd - u(25.1), cz, STEEL));
  p.push(boxPart(u(0.46), u(0.55), u(0.3), cx + u(30), jd - u(26.3), cz, STEEL));
  // 操作室
  p.push(boxPart(u(1.6), u(2.0), u(2.2), cx + u(1.9), zTop + u(1.1), cz - u(1.3), CABIN_BLUE));
  return p;
}

/** 程序化 fallback 的在建结构 + 临建 + 材料堆场。 */
function yardFallbackParts(): MergePart[] {
  const p: MergePart[] = [];
  const bx = u(6.0);
  const bz = u(-6.0);
  const hx = u(9.0);
  const hy = u(6.0);
  const fh = u(3.6);
  // 在建框架：3 层柱 + 梁板
  for (let f = 0; f <= 3; f++) {
    const z = u(0.1) + fh * f;
    for (const cx2 of [-hx, -hx / 3, hx / 3, hx]) {
      for (const cz2 of [-hy, hy]) {
        p.push(boxPart(u(0.45), fh, u(0.6), bx + cx2, z + fh / 2, bz + cz2, CONCRETE));
      }
    }
    for (const cz2 of [-hy, hy]) {
      p.push(boxPart(hx * 2, u(0.24), u(1.3), bx + bx, z + fh - u(0.12), bz + cz2, CONCRETE));
      p.push(boxPart(hx * 2, u(0.45), u(0.35), bx + bx, z + fh - u(0.45), bz + cz2, CONCRETE));
    }
  }
  // 脚手架立杆（两面）
  for (const side of [-1, 1]) {
    for (let i = 0; i <= 10; i++) {
      const x = bx - hx + (i * hx * 2) / 10;
      p.push(cylPart(u(0.055), u(0.055), u(12.4), 5, x, u(6.3), bz + side * (hy + u(0.9)), STEEL));
    }
    p.push(boxPart(hx * 2, u(12.4), u(0.04), bx + bx, u(6.3), bz + side * (hy + u(0.94)), NET_GREEN));
  }
  // 临建集装箱 2 组 × 2 层
  for (let g = 0; g < 2; g++) {
    for (let lv = 0; lv < 2; lv++) {
      const z = u(0.1) + lv * (u(2.59) + u(0.06)) + u(2.59) / 2;
      const x = u(13.0) + g * (u(6.06) + u(1.2));
      p.push(boxPart(u(6.06), u(2.59), u(2.44), x, z, u(9.0), CABIN_BLUE));
      for (const sy of [-1, 1]) {
        p.push(boxPart(u(5.06), u(0.85), u(0.05), x, z + u(0.35), u(9.0) + sy * u(1.24), CABIN_WINDOW));
      }
    }
  }
  // 材料堆场：钢筋捆 + 砂石堆 + 模板垛
  for (let i = 0; i < 2; i++) {
    for (let k = 0; k < 4; k++) {
      p.push(cylPart(u(0.28), u(0.28), u(9.0), 6, u(15.0), u(4.0) + k * u(0.62), u(0.4), REBAR));
    }
  }
  p.push(cylPart(u(1.1), u(3.2), u(2.1), 10, u(15.5), u(-6.0), u(1.15), AGG));
  p.push(cylPart(u(0.9), u(2.6), u(1.7), 10, u(20.5), u(-6.0), u(0.95), AGG));
  for (let i = 0; i < 2; i++) {
    for (let k = 0; k < 6; k++) {
      p.push(boxPart(u(4.0), u(0.09), u(2.4), u(23.0), u(0.16) + k * u(0.11), u(-8.0) + i * u(3.2), AGG));
    }
  }
  return p;
}

/**
 * 施工工地（批次 50）。
 *
 * 走 `CivicGlbPiece` 的标准降级链（批次 46 抽出的可复用件）：GLB 优先，
 * 缺失 / 未启用 / 加载中则渲染本文件的程序化几何（**按新尺寸重建**）。
 * `anchorY` 由 1.0 提到 **8.0** —— 旧值是按「9 m 高的实心柱」定的，
 * 真实塔帽顶在 44.2 m，悬停卡片挂 8 m 正好落在塔身中段的可读位置。
 */
export function ConstructionSite() {
  const geo = useMemo(
    () => mergeParts([...siteFallbackParts(), ...craneFallbackParts(), ...yardFallbackParts()]),
    [],
  );
  useEffect(() => () => geo.dispose(), [geo]);

  return (
    <CivicGlbPiece
      glbName="construction_site"
      dimsKey="constructionSite"
      infoId="landmark.construction-site"
      anchorY={8}
      position={[SITE_X, 0, SITE_Z]}
      fallback={
        <mesh geometry={geo} receiveShadow>
          <meshStandardMaterial vertexColors roughness={0.82} metalness={0.22} />
        </mesh>
      }
    />
  );
}
