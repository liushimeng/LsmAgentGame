/**
 * 变电站（18-AA · §5.2 Substation）—— 城区 10/35kV 配电站。布点 (-18.5, -11)。
 *
 * 批次 46「城市公用设施真实感」：程序化玩具 → Blender GLB（`civic/substation.glb`
 * ＋ `3d_script/build_substation.py`）。本文件保留程序化几何作 fallback，
 * **并按新尺寸整体重建**（§27.3 硬约束 7：GLB ≡ fallback 同尺寸）。
 *
 * ── 尺寸为什么是这些数（真实依据，详见设计 46 §2.1）──────────────────────
 *   围墙 18.0×14.0 = 252 ㎡（35kV 户外变电站常规 400~600 ㎡、紧凑 200~300 ㎡）；
 *   围墙全高 2.45 m（GB 50059-2011 §2.0.5：屋外变电站实体围墙不低于 2.2 m）；
 *   出线电杆 ⌀0.30 渐收、高 12.0 m（10kV 配网常用杆型）；主变外廓距围栏 1.6 m、
 *   底部距地 0.35 m（GB 50053-2013 §4.2.2 的 ≥0.8 m / ≥0.3 m）。
 *   批次 18-AA 原状是 4×3 m、墙高 1.2 m 的「围栏 + 2 个圆筒」，体量差约 30 倍。
 *
 * ── 坐标系（与 `build_substation.py` 对齐）──────────────────────────────
 *   Blender Z-up + 导出 Yup ⇒ three y = Blender z，**three z = -Blender y**。
 *   建模脚本末尾调 `center_content_xz()`，把「围墙方院 + 外挑电杆」这个天然
 *   不对称的组合在水平两轴居中 ⇒ 院区在 three 里**不居中于原点**：
 *   围墙 z ∈ [-9.675, +4.325]（中心 -2.675），出线电杆在 z ≈ +6.8。
 *   下文一律直接写 **three 坐标**（已含翻转与居中），改 GLB 尺寸时须同步。
 *
 * ── 降级链（§27.3-3）──────────────────────────────────────────────────
 *   `blenderModelsEnabled()` → `modelUrl` → GLB 载入 → 否则本文件的程序化几何。
 *   尺寸守卫挂在 `sizeTargetFor('substation')`（dev 态量 Box3 比对 ±5%）。
 */
import { useEffect, useMemo } from 'react';
import { boxPart, cylPart, mergeParts, type MergePart } from '@/engine3d';
import { u } from '../cityScale';
import { CivicGlbPiece } from './CivicGlb';

const WALL_LIGHT = '#b6b3ad';    // concrete：装配式墙板
const WALL_DARK = '#8d8a85';     // concrete_dark：墙裙 / 压顶 / 基础
const STEEL = '#8d949c';
const STEEL_DARK = '#4e545c';
const VENT = '#3f454c';
const WARN = '#e0a020';
const RUST = '#8a4a2c';

/** 纯色件统一材质参数（沿用批次 28 二轮的全件取值，避免两条路径观感分叉）。 */
const FLAT_ROUGH = 0.62;
const FLAT_METAL = 0.45;

/** 站内警示灯箱：材质名匹配 GLB 的 `Substation_Sign_Mat`，夜间 1.6。 */
const LIT = { Substation_Sign: 1.6 };

/** 站点常量（米）—— 与 `build_substation.py` 一一对应（已换算到 three 坐标）。 */
const YARD_HX = 9.0;             // 围墙 X 半跨
const WALL_Y = 14.0;             // 围墙 Y 跨度（Blender）
const WALL_H = 2.45;             // 围墙全高
const GATE_W = 4.0;
const Z_N = -9.675;              // 北墙（带门，Blender y=+7）
const Z_S = 4.325;               // 南墙（Blender y=-7）
const Z_CABIN = -5.675;          // 箱变舱（Blender y=+3）
const Z_XFMR = -0.675;           // 主变（Blender y=-2）
const Z_SW = 2.325;              // 隔离开关构架（Blender y=-5）
const Z_POLE = 6.825;            // 出线电杆（Blender y=-9.5）
const CABIN_X = 4.6, CABIN_L = 6.0, CABIN_D = 2.5, CABIN_H = 3.0, CABIN_BASE = 0.3;
const XFMR_L = 2.4, XFMR_D = 1.9, XFMR_H = 2.2, XFMR_BASE = 0.35;
const POLE_X = 5.0, POLE_H = 12.0, WIRE_LEN = 3.0;
const SW_X = 1.2, SW_STAND_H = 4.5;
const POLE_ARM_Z = [9.0, 10.2, 11.4];
const SKIRT_H = 0.45, CAP_H = 0.2;
const PANEL_H = WALL_H - SKIRT_H - CAP_H;
const WALL_T = 0.2, CAP_T = 0.34;

/** 一段装配式围墙：`axis='x'` 沿 X 延展（centerZ 固定），`'z'` 沿 Z 延展（centerX 固定）。 */
function wallRun(parts: MergePart[], len: number, centerX: number, centerZ: number, axis: 'x' | 'z', gate: boolean) {
  const put = (w: number, h: number, d: number, x: number, y: number, z: number, color: string) =>
    parts.push(boxPart(u(w), u(h), u(d), u(x), u(y), u(z), color));

  // gate=true 时把这一段在 X 向让出 GATE_W 的大门，拆成左右两截
  const runs: Array<{ len: number; cx: number }> = gate
    ? [-1, 1].map((s) => ({ len: (len - GATE_W) / 2, cx: centerX + s * (GATE_W / 2 + (len - GATE_W) / 4) }))
    : [{ len, cx: centerX }];

  for (const { len: segLen, cx } of runs) {
    put(segLen, SKIRT_H, WALL_T + 0.06, cx, SKIRT_H / 2, centerZ, WALL_DARK);
    put(segLen, PANEL_H, WALL_T, cx, SKIRT_H + PANEL_H / 2, centerZ, WALL_LIGHT);
    put(segLen + 0.14, CAP_H, CAP_T, cx, WALL_H - CAP_H / 2, centerZ, WALL_DARK);
    // 壁柱（兼作预制墙板分格缝）
    for (let i = 0; i <= Math.floor(segLen / 3); i++) {
      const o = -segLen / 2 + 3 * i;
      const px = axis === 'x' ? cx + o : cx + (WALL_T - 0.27) / 2;
      const pz = axis === 'x' ? centerZ - 0.035 : centerZ + o;
      put(axis === 'x' ? 0.34 : 0.27, WALL_H, axis === 'x' ? 0.27 : 0.34, px, WALL_H / 2, pz, WALL_LIGHT);
    }
  }
}

function substationParts(): MergePart[] {
  const parts: MergePart[] = [];

  // ① 围墙：南墙（整段）+ 东西墙（沿 Z）+ 北墙（让出大门）
  wallRun(parts, WALL_Y * 2, 0, Z_S, 'z', false);
  wallRun(parts, WALL_Y, -YARD_HX, (Z_N + Z_S) / 2, 'z', false);
  wallRun(parts, WALL_Y, YARD_HX, (Z_N + Z_S) / 2, 'z', false);
  wallRun(parts, WALL_Y * 2, 0, Z_N, 'x', true);
  // 大门：门柱 ×2 + 门楣 + 2 扇铁门
  for (const sx of [-1, 1]) {
    parts.push(boxPart(u(0.25), u(WALL_H), u(0.25), u(sx * (GATE_W / 2 + 0.12)), u(WALL_H / 2), u(Z_N), WALL_DARK));
    parts.push(boxPart(u(GATE_W / 2 - 0.06), u(2.2), u(0.05), u(sx * GATE_W / 4), u(1.1), u(Z_N + 0.22), STEEL_DARK));
  }
  parts.push(boxPart(u(GATE_W + 0.7), u(0.25), u(0.3), 0, u(WALL_H - 0.125), u(Z_N), WALL_DARK));

  // ② 预制箱变舱 ×2
  for (const cx of [-CABIN_X, CABIN_X]) {
    parts.push(boxPart(u(CABIN_L + 0.6), u(CABIN_BASE), u(CABIN_D + 0.6), u(cx), u(CABIN_BASE / 2), u(Z_CABIN), WALL_DARK));
    parts.push(boxPart(u(CABIN_L), u(CABIN_H), u(CABIN_D), u(cx), u(CABIN_BASE + CABIN_H / 2), u(Z_CABIN), WALL_LIGHT));
    parts.push(boxPart(u(CABIN_L + 0.3), u(0.16), u(CABIN_D + 0.3), u(cx), u(CABIN_BASE + CABIN_H + 0.06), u(Z_CABIN), STEEL));
    parts.push(boxPart(u(1.0), u(2.1), u(0.06), u(cx - 1.6), u(CABIN_BASE + 1.05), u(Z_CABIN - CABIN_D / 2 - 0.02), STEEL_DARK));
    for (const dx of [0.9, 2.2]) {
      parts.push(boxPart(u(1.0), u(0.7), u(0.05), u(cx + dx), u(CABIN_BASE + 1.95), u(Z_CABIN - CABIN_D / 2 - 0.02), VENT));
    }
    for (const dz of [-0.7, 0, 0.7]) {
      parts.push(cylPart(u(0.085), u(0.085), u(0.45), 8, u(cx + 1.8), u(CABIN_BASE + CABIN_H + 0.34), u(Z_CABIN + dz), RUST));
    }
  }

  // ③ 主变：基座 + 油箱 + 双侧 8 片散热器 + 3 套管
  parts.push(boxPart(u(XFMR_L + 0.6), u(XFMR_BASE), u(XFMR_D + 0.5), 0, u(XFMR_BASE / 2), u(Z_XFMR), WALL_DARK));
  parts.push(boxPart(u(XFMR_L), u(XFMR_H), u(XFMR_D), 0, u(XFMR_BASE + XFMR_H / 2), u(Z_XFMR), STEEL_DARK));
  for (const sz of [-1, 1]) {
    for (let i = 0; i < 8; i++) {
      parts.push(boxPart(u(0.1), u(1.4), u(0.34),
        u(-(XFMR_L - 0.3) / 2 + i * (XFMR_L - 0.3) / 7), u(XFMR_BASE + 1.05),
        u(Z_XFMR + sz * (XFMR_D / 2 + 0.17)), STEEL));
    }
  }
  for (const dx of [-0.7, 0, 0.7]) {
    parts.push(cylPart(u(0.115), u(0.075), u(0.55), 8, u(dx), u(XFMR_BASE + XFMR_H + 0.34), u(Z_XFMR), RUST));
  }

  // ④ 隔离开关构架：2 立杆 + 2 层横担 + 6 绝缘子串
  for (const sx of [-1, 1]) {
    parts.push(cylPart(u(0.06), u(0.06), u(SW_STAND_H), 8, u(sx * SW_X), u(SW_STAND_H / 2), u(Z_SW), STEEL_DARK));
    parts.push(boxPart(u(0.45), u(0.25), u(0.45), u(sx * SW_X), u(0.125), u(Z_SW), WALL_DARK));
  }
  for (const az of [3.0, 3.8]) {
    parts.push(boxPart(u(SW_X * 2 + 0.5), u(0.09), u(0.09), 0, u(az), u(Z_SW), STEEL));
    for (const dx of [-0.75, 0, 0.75]) {
      parts.push(cylPart(u(0.055), u(0.055), u(0.32), 6, u(dx), u(az - 0.2), u(Z_SW), VENT));
    }
  }

  // ⑤ 出线电杆 ×2：三层横担 + 9 绝缘子 + 3 相引下线
  for (const sx of [-1, 1]) {
    const px = sx * POLE_X;
    parts.push(boxPart(u(0.9), u(0.4), u(0.9), u(px), u(0.2), u(Z_POLE), WALL_DARK));
    parts.push(cylPart(u(0.12), u(0.15), u(POLE_H), 10, u(px), u(0.4 + POLE_H / 2), u(Z_POLE), STEEL_DARK));
    for (const az of POLE_ARM_Z) {
      parts.push(boxPart(u(1.8), u(0.09), u(0.09), u(px), u(az), u(Z_POLE), STEEL));
      for (const dx of [-0.6, 0, 0.6]) {
        parts.push(cylPart(u(0.05), u(0.05), u(0.26), 6, u(px + dx), u(az - 0.17), u(Z_POLE), VENT));
        parts.push(boxPart(u(0.05), u(0.05), u(WIRE_LEN), u(px + dx), u(az - 0.4), u(Z_POLE + WIRE_LEN / 2), STEEL_DARK));
      }
    }
  }

  // ⑥ 4.0 m 消防车道（十字）+ 警示灯箱 ×2
  parts.push(boxPart(u(4.0), u(0.1), u(WALL_Y - 0.5), 0, u(0.05), u((Z_N + Z_S) / 2), WALL_LIGHT));
  parts.push(boxPart(u(WALL_Y * 2 - 0.5), u(0.1), u(4.0), 0, u(0.09), u((Z_N + Z_S) / 2), WALL_LIGHT));
  for (const sx of [-1, 1]) {
    parts.push(boxPart(u(0.42), u(0.6), u(0.14), u(sx * 3.2), u(1.55), u(Z_N + 0.22), WARN));
    parts.push(boxPart(u(0.07), u(1.25), u(0.07), u(sx * 3.2), u(0.62), u(Z_N + 0.22), STEEL_DARK));
  }
  return parts;
}

export function Substation() {
  const geo = useMemo(() => mergeParts(substationParts()), []);
  useEffect(() => () => geo.dispose(), [geo]);

  return (
    <CivicGlbPiece
      glbName="substation"
      dimsKey="substation"
      infoId="civic.substation"
      anchorY={2.45}
      position={[-18.5, 0, -11]}
      litMaterials={LIT}
      fallback={
        <mesh geometry={geo}>
          <meshStandardMaterial vertexColors roughness={FLAT_ROUGH} metalness={FLAT_METAL} />
        </mesh>
      }
    />
  );
}
