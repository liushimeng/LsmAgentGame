/**
 * 物流港码头（18-AA · §5.2 PortTerminal）—— 内河支线集装箱码头。
 *
 * 批次 48「港口码头真实感」重制。三处硬伤（详见设计 48 §1.1）：
 *
 * ── D1 尺度与落位双错 ────────────────────────────────────────────────
 *   旧：`PlaneGeometry(u(80), u(25))` @ `(-30, 0, -9.5)` `rotation=[0, π, 0]`
 *   ⇒ 世界 **x ∈ [-70, +10]、800 m 长的码头面**，伸出城界（x=-60）**100 m**，
 *   另一端一路铺到 CBD 脚下。
 *   新：**80 m 泊位**，落点 `(-32, -8.7)`，陆域 8×4 u（保留地锁定）。
 *
 * ── D2 集装箱尺寸错一半 ──────────────────────────────────────────────
 *   旧：`boxPart(u(6), u(2.6), u(2.4))` —— 40 ft 国际标准箱是 **12.19 × 2.44 × 2.59 m**
 *   （20 ft 为 6.06 × 2.44 × 2.59）。旧的既不是 40 ft 也不是 20 ft（宽 2.6 > 2.44）。
 *   新：40 只 40 ft 箱，4 排 × 5 列 × 2 层，**5 种航运箱色**。
 *
 * ── D3 岸吊是门架玩具 ────────────────────────────────────────────────
 *   旧：4 根 ⌀0.5 m 圆柱 + 10 m 悬臂 + 22 m 横梁 + 1 个吊具。
 *   新：真 STS 岸桥 —— **轨距 16 m**（DB36/T 1833-2023）、门架净空 10 m、
 *   轨下起升 **26 m**、外伸 **26 m 罩住水面**、后伸 12 m、A 字架 + 前后拉杆
 *   + 桁架腹杆 + 机器房 + 司机室 + 吊具（跨 40 ft = 12.5 m）+ 航空障碍灯。
 *
 * ── 坐标系（与 `build_port_terminal.py` 对齐）──────────────────────────
 *   建模脚本在 Blender 的 **XY 平面**铺场（X = 泊长、Y = 陆海进深、Z = 高），
 *   **海侧是 Blender +Y**；导出自动 Yup ⇒ three y = Blender z、**three z = -Blender y**。
 *   于是「海侧」落在 three 的**负 z** 侧 —— 而港池 `PORT_POOL_WATER` 在世界
 *   z ∈ [-8, 0]（也是负 z）⇒ **不需要额外旋转**，`position=[-32, 0, -8.7]` 即让
 *   岸壁线（局部 z = -0.70）落在 z = -8（池南岸）、悬臂端（局部 z = -3.0）落在
 *   z = -5.7（**罩在港池上方** ✓）。
 *
 * ── 降级链（§27.3-3）────────────────────────────────────────────────
 *   `blenderModelsEnabled()` → `modelUrl` → GLB 载入 → 否则本文件的程序化几何
 *   （按新尺寸重建）。尺寸守卫 `sizeTargetFor('portTerminal')`。
 */
import { useEffect, useMemo } from 'react';
import { boxPart, cylPart, mergeParts, type MergePart } from '@/engine3d';
import { u } from '../cityScale';
import { CivicGlbPiece } from './CivicGlb';

const CONCRETE = '#b6b3ad';
const CONCRETE_DARK = '#8d8a85';
const STEEL = '#8d949c';
const CRANE = '#9aa3ab';
const CRANE_FRAME = '#5c646c';
const WARN = '#d4622a';
const BOX_COLORS = ['#8f4a38', '#6f3a2c', '#3f6b8a', '#7a7f52', '#8a6a3a'];
const LINE = '#f4f0e2';   // 航空障碍灯 / 照明灯头发光面
const RMG_SPAN = 20.0, RMG_H = 18.0;

/** 纯色件统一材质参数。 */
const FLAT_ROUGH = 0.72;
const FLAT_METAL = 0.35;

/** 夜间自发光：岸桥航空障碍灯 + 堆场照明塔灯头（材质名匹配 GLB 的 `Port_NavLight_Mat`）。 */
const LIT = { Port_NavLight: 2.0 };

// ── 场地常量（米）—— 与 `build_port_terminal.py` 一一对应 ─────────────────
/** 岸壁线在**局部 z = -0.70**（Blender y=0 经 Yup 翻转 + center_content_xz 居中）。 */
const QUAY_Z = -0.70;              // 局部 z：岸壁线（陆海分界）
const DECK_BACK = 3.20;            // 局部 z：码头面陆侧后沿（进深 3.9 u = 39 m）
const BERTH_L = 80.0;
const CRANE_X: ReadonlyArray<number> = [-20.0, 20.0];
const CRANE_GAUGE = 16.0;
const CRANE_GIRDER = 26.0;
const CRANE_APEX = 34.0;
const CRANE_SPAN = 12.5;   // 吊具跨 40 ft
const BOX_L = 12.19, BOX_W = 2.44, BOX_H = 2.59;   // 40 ft 箱（ISO 668）
const BOX_GAP = 0.40;
const YARD_ROWS = 4, YARD_COLS = 5, YARD_TIERS = 2;
const DECK_T = 0.30;
const WALL_TOP = 0.70;
const BOLLARD_N = 6;
const FENDER_N = 6;
const LIGHT_MAST_H = 30.0;

function portTerminalParts(): MergePart[] {
  const parts: MergePart[] = [];

  // ① 码头面（80 × 34 m）+ 岸壁胸墙 + 压顶
  // 码头面：岸壁线（局部 z = QUAY_Z）到陆侧后沿（DECK_BACK），进深 3.9 u = 39 m
  const deckDepth = DECK_BACK - QUAY_Z;
  parts.push(boxPart(u(BERTH_L), u(DECK_T), u(deckDepth), 0, u(DECK_T / 2),
    u((QUAY_Z + DECK_BACK) / 2), CONCRETE));
  parts.push(boxPart(u(BERTH_L), u(WALL_TOP - DECK_T), u(0.45), 0,
    u(DECK_T + (WALL_TOP - DECK_T) / 2), u(QUAY_Z - 0.225), CONCRETE));
  parts.push(boxPart(u(BERTH_L), u(0.12), u(0.65), 0, u(WALL_TOP + 0.06), u(QUAY_Z - 0.225), CONCRETE_DARK));

  // ② 系船柱 ×6 + 护舷 ×6（护舷在**水侧** = 局部 z 更负）
  for (let i = 0; i < BOLLARD_N; i++) {
    const bx = -BERTH_L / 2.0 + (BERTH_L / (BOLLARD_N - 1)) * i;
    parts.push(cylPart(u(0.28), u(0.34), u(0.80), 10, u(bx), u(WALL_TOP + 0.40), u(QUAY_Z + 1.10), STEEL));
  }
  for (let i = 0; i < FENDER_N; i++) {
    const fx = -BERTH_L / 2.0 + (BERTH_L / (FENDER_N - 1)) * i;
    parts.push(boxPart(u(1.60), u(0.95), u(0.70), u(fx), u(0.50), u(QUAY_Z - 0.30), WARN));
  }

  // ③ 轨道 ×2（轨距 16 m；轨枕抽稀到 2.4 m，落进 render 不糊）
  for (const y of [3.0, 19.0]) {
    for (let i = 0; i < 34; i++) {
      parts.push(boxPart(u(0.24), u(0.18), u(2.60), u(-39 + i * 2.4), u(0.09), u(y), CONCRETE_DARK));
    }
    for (const sy of [-1, 1]) {
      parts.push(boxPart(u(BERTH_L), u(0.16), u(0.10), 0, u(0.26), u(y + sy * 0.75), STEEL));
    }
  }

  // ④ 岸桥 ×2（简化但保留 STS 轮廓：门架腿 + 门架梁 + 桁架大梁 + A 字架 + 吊具）
  for (const cx of CRANE_X) {
    for (const sx of [-1, 1]) {
      for (const y of [3.0, 19.0]) {
        parts.push(boxPart(u(0.90), u(CRANE_GIRDER - 1.5), u(0.90),
          u(cx + sx * 9.0), u(1.5 + (CRANE_GIRDER - 1.5) / 2), u(y), CRANE));
        parts.push(boxPart(u(1.60), u(0.60), u(1.60), u(cx + sx * 9.0), u(0.30), u(y), WARN));
      }
      parts.push(boxPart(u(1.00), u(1.60), u(CRANE_GAUGE), u(cx + sx * 9.0), u(10.8), u(11.0), CRANE));
    }
    // 桁架大梁：海侧悬臂（局部 z 更负）+ 陆侧后伸
    for (const dz of [0, 2.2]) {
      parts.push(boxPart(u(1.20), u(0.45), u(54.0), u(cx), u(CRANE_GIRDER + 1.0 + dz), u(3.0), CRANE));
    // 桁架腹杆（每 ~4.3 m 一道，兼作「这不是一块光板」的可读性）
    for (let i = 0; i < 13; i++) {
      parts.push(boxPart(u(0.35), u(3.65), u(0.30), u(cx), u(CRANE_GIRDER + 2.1), u(-6.5 + i * 4.3), CRANE_FRAME));
    }
    }
    // A 字架（两腿近似为斜置 box）
    for (const sx of [-1, 1]) {
      for (const y of [0.0, 6.0]) {
        parts.push(boxPart(u(0.55), u(7.4), u(0.55), u(cx + sx * 5.0), u(30.4), u(19.0 - y), CRANE));
      }
    }
    parts.push(boxPart(u(1.40), u(0.90), u(6.60), u(cx), u(CRANE_APEX - 0.45), u(16.0), CRANE));
    // 机器房 + 小车 + 吊具（悬在海侧）
    parts.push(boxPart(u(5.20), u(3.20), u(7.00), u(cx), u(CRANE_GIRDER + 4.6), u(23.5), CRANE));
    parts.push(boxPart(u(2.60), u(1.40), u(3.20), u(cx), u(CRANE_GIRDER + 0.7), u(17.3), CRANE));
    parts.push(boxPart(u(CRANE_SPAN), u(0.90), u(2.70), u(cx), u(CRANE_GIRDER - 8.6), u(17.3), WARN));
    // 航空障碍灯
    parts.push(cylPart(u(0.22), u(0.22), u(0.40), 8, u(cx), u(CRANE_APEX + 0.4), u(16.0), LINE));
  }

  // ⑤ 集装箱堆场：40 ft × 40 只，5 色分桶（材质分组在下面 join 时不再拆）
  const colPitch = BOX_L + BOX_GAP;
  const rowPitch = BOX_W + BOX_GAP;
  const x0 = -(YARD_COLS - 1) * colPitch / 2.0;
  const z0 = 26.0 - (YARD_ROWS - 1) * rowPitch / 2.0;
  for (let r = 0; r < YARD_ROWS; r++) {
    for (let c = 0; c < YARD_COLS; c++) {
      for (let t = 0; t < YARD_TIERS; t++) {
        parts.push(boxPart(u(BOX_L), u(BOX_H), u(BOX_W),
          u(x0 + c * colPitch), u(DECK_T + BOX_H / 2 + t * BOX_H), u(z0 + r * rowPitch),
          BOX_COLORS[(r * YARD_COLS + c + t * 2) % BOX_COLORS.length]));
      }
    }
  }

  // ⑥ 轮胎式龙门吊 RMG ×1（跨堆场）+ 堆场照明塔 ×2 + 闸口办公楼 + 港区道路
  for (const sx of [-1, 1]) {
    for (const sy of [-1, 1]) {
      parts.push(boxPart(u(0.70), u(RMG_H - 2.5), u(0.70), u(sx * 10.0), u(0.30 + (RMG_H - 2.5) / 2), u(26.0 + sy * 4.0), CRANE));
    }
  }
  parts.push(boxPart(u(RMG_SPAN + 3.0), u(1.50), u(2.0), 0, u(RMG_H - 0.75), u(26.0), CRANE));
  parts.push(boxPart(u(BOX_L + 0.4), u(0.80), u(BOX_W + 0.2), u(3.0), u(RMG_H - 6.6), u(26.0), WARN));
  for (const mx of [-34.0, 34.0]) {
    parts.push(boxPart(u(1.60), u(0.50), u(1.60), u(mx), u(0.25), u(33.0), CONCRETE_DARK));
    parts.push(cylPart(u(0.18), u(0.34), u(LIGHT_MAST_H), 8, u(mx), u(0.50 + LIGHT_MAST_H / 2), u(33.0), STEEL));
    parts.push(boxPart(u(3.20), u(0.40), u(1.20), u(mx), u(LIGHT_MAST_H + 0.6), u(33.0), STEEL));
    for (let k = 0; k < 3; k++) {
      parts.push(boxPart(u(0.90), u(0.30), u(0.80), u(mx - 1.1 + k * 1.1), u(LIGHT_MAST_H + 0.35), u(33.0), LINE));
    }
  }
  parts.push(boxPart(u(10.0), u(6.0), u(7.0), u(33.5), u(3.0), u(33.0), CONCRETE));
  parts.push(boxPart(u(10.5), u(0.25), u(7.5), u(33.5), u(6.12), u(33.0), CONCRETE_DARK));
  parts.push(boxPart(u(74.0), u(0.10), u(6.0), 0, u(0.05), u(33.0), CONCRETE));

  return parts;
}

export function PortTerminal() {
  const geo = useMemo(() => mergeParts(portTerminalParts()), []);
  useEffect(() => () => geo.dispose(), [geo]);

  return (
    <CivicGlbPiece
      glbName="port_terminal"
      dimsKey="portTerminal"
      infoId="civic.port-terminal"
      anchorY={6}
      position={[-32, 0, -8.7]}
      litMaterials={LIT}
      fallback={
        <mesh geometry={geo} receiveShadow>
          <meshStandardMaterial vertexColors roughness={FLAT_ROUGH} metalness={FLAT_METAL} />
        </mesh>
      }
    />
  );
}
