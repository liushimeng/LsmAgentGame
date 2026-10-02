/**
 * 加油站（18-AA · §5.2 GasStation）。布点 (5.8, 6.7)。
 *
 * 批次 46「城市公用设施真实感」：程序化玩具 → Blender GLB（`civic/gas_station.glb`
 * ＋ `3d_script/build_gas_station.py`）。程序化几何保留为 fallback 并按新尺寸重建
 * （§27.3 硬约束 7：GLB ≡ fallback 同尺寸）。
 *
 * ── 尺寸为什么是这些数（真实依据，详见设计 46 §2.2）──────────────────────
 *   罩棚投影 22×14 = 308 ㎡、檐口下**有效净高 4.70 m**（GB 50156-2012 强条：
 *   罩棚下有效高度不应小于 4.5 m）；加油岛 11.0×1.40×0.15（高出场地
 *   0.15~0.20 m、宽不小于 1.2 m）；罩棚柱配 ⌀0.20×0.80 防撞柱（柱应有防撞措施、
 *   栏高 ≥0.5 m）；便利店 12.0×4.50 = 54 ㎡ 单层、层高 3.60 m（常见 70~365 ㎡、
 *   作业区内不宜超 300 ㎡）；形象牌杆 9.0 m + 灯箱 2.60 m。
 *   批次 18-AA 的罩棚净高与投影**本来就合规**，缺的是网架层次/加油岛/防撞柱/
 *   雨棚/幕墙/女儿墙等全部细节。
 *
 * ── 坐标系（与 `build_gas_station.py` 对齐）─────────────────────────────
 *   Blender Z-up + 导出 Yup ⇒ three y = Blender z，**three z = -Blender y**。
 *   `center_content_xz()` 把「罩棚 + 侧挂形象牌 + 后置站房」这个三向不对称的
 *   组合居中：内容盒 X 中心 -1.875、Y 中心 +2.78 ⇒ three x = Blender x + 1.875、
 *   three z = -Blender y + 2.78。下文直接写 three 坐标（已含翻转与居中）。
 *
 * ── 降级链（§27.3-3）──────────────────────────────────────────────────
 *   `blenderModelsEnabled()` → `modelUrl` → GLB 载入 → 否则本文件的程序化几何。
 *   尺寸守卫挂在 `sizeTargetFor('gasStation')`。
 */
import { useEffect, useMemo } from 'react';
import { boxPart, cylPart, mergeParts, type MergePart } from '@/engine3d';
import { u } from '../cityScale';
import { CivicGlbPiece } from './CivicGlb';

const CONCRETE = '#b6b3ad';
const CONCRETE_DARK = '#8d8a85';
const STEEL = '#8d949c';
const STEEL_DARK = '#4e545c';
const CANOPY_RED = '#a83b32';
const SHOP_WHITE = '#e2e0da';
const GLASS = '#9fc4cf';
const SIGN_AMBER = '#ffb43a';   // 加油机屏（与 3d_script CITY_PALETTE.sign_amber 同值）
const LIGHTBOX = '#e8e2d0';      // 形象牌 / 门头灯箱乳白亚克力

/** 纯色件统一材质参数（沿用批次 28 二轮的全件取值）。 */
const FLAT_ROUGH = 0.62;
const FLAT_METAL = 0.25;

/** 夜间自发光：形象牌灯箱 2.4 / 便利店门头 2.4 / 加油机屏 1.2。 */
const LIT = {
  GasStation_SIGN_Panel: 2.4,
  GasStation_PUMP_Screen: 1.2,
};

/** 罩棚（米）。 */
const CAN_X = 22.0, CAN_Y = 14.0;
const CLEAR_H = 4.70, RIDGE_Z = 5.58, EAVE_Z = 5.23, PURLIN_Z = 5.10, MAINBEAM_Z = 4.98;
const COL_X = 9.6, COL_Y = 5.6, COL_R = 0.18;
/** 加油岛 / 加油机。 */
const ISL_L = 11.0, ISL_W = 1.4, ISL_H = 0.15, ISL_Y = 3.2, PUMP_X = 5.0;
/** 便利店。 */
const SHOP_L = 12.0, SHOP_D = 4.5, SHOP_H = 3.6, SHOP_Y = 10.0, SHOP_PLINTH = 0.3;
/** 形象牌。 */
const TOTEM_X = -12.6, TOTEM_Y = 0.0, TOTEM_POLE_R = 0.15, TOTEM_POLE_H = 9.0;
const TOTEM_PANEL_L = 4.2, TOTEM_PANEL_H = 2.6, TOTEM_PANEL_T = 0.35, TOTEM_PANEL_Z = 8.0;
/** center_content_xz 的居中偏移（Blender x / Blender y）。 */
const CX = 1.875, CY = 2.78;

/** Blender (x, y) → three (x, z)。 */
const bx = (x: number) => u(x + CX);
const bz = (y: number) => u(-y + CY);

function gasStationParts(): MergePart[] {
  const parts: MergePart[] = [];
  const hx = CAN_X / 2, hy = CAN_Y / 2;

  // ── ① 罩棚钢柱 + 柱脚 + 牛腿 + 防撞柱 ───────────────────────────────
  for (const sx of [-1, 1]) {
    for (const sy of [-1, 1]) {
      const px = sx * COL_X, py = sy * COL_Y;
      parts.push(boxPart(u(1.0), u(0.35), u(1.0), bx(px), u(0.175), bz(py), CONCRETE_DARK));
      parts.push(cylPart(u(COL_R), u(COL_R), u(CLEAR_H), 12, bx(px), u(0.35 + CLEAR_H / 2), bz(py), STEEL));
      parts.push(boxPart(u(0.7), u(0.28), u(0.3), bx(px), u(CLEAR_H + 0.14), bz(py), STEEL_DARK));
      for (const dx of [-1.1, 1.1]) {
        const qy = py + sy * 0.8;
        parts.push(cylPart(u(0.1), u(0.1), u(0.8), 8, bx(px + dx), u(0.4), bz(qy), STEEL_DARK));
        parts.push(cylPart(u(0.11), u(0.11), u(0.12), 8, bx(px + dx), u(0.58), bz(qy), CANOPY_RED));
      }
    }
  }

  // ── ② 罩棚网架：檐口梁 + 主梁 + 次梁 + 檩条 ───────────────────────────
  for (const sy of [-1, 1]) {
    parts.push(boxPart(u(CAN_X), u(0.55), u(0.26), 0, u(CLEAR_H + 0.275), bz(sy * hy), STEEL));
  }
  for (const sx of [-1, 1]) {
    parts.push(boxPart(u(0.26), u(0.55), u(CAN_Y), bx(sx * hx), u(CLEAR_H + 0.275), 0, STEEL));
  }
  for (const x of [-COL_X, -3.2, 3.2, COL_X]) {
    parts.push(boxPart(u(0.24), u(0.62), u(CAN_Y), bx(x), u(MAINBEAM_Z), 0, STEEL));
  }
  for (const y of [-COL_Y, -2.0, 2.0, COL_Y]) {
    parts.push(boxPart(u(CAN_X), u(0.3), u(0.14), 0, u(PURLIN_Z - 0.22), bz(y), STEEL_DARK));
  }
  for (let i = 0; i < 10; i++) {
    const x = -hx + 0.6 + i * (CAN_X - 1.2) / 9;
    parts.push(boxPart(u(0.1), u(0.16), u(CAN_Y), bx(x), u(PURLIN_Z), 0, STEEL_DARK));
  }

  // ── ③ 屋面：缓双坡白板 + 红色立缝 + 红色檐口带 + 屋脊压顶 + 天沟 + 采光带 ─
  for (const sy of [-1, 1]) {
    // 缓坡板（用略高的等效平板近似，fallback 不做倾斜）
    parts.push(boxPart(u(CAN_X), u(0.1), u(hy), 0, u((RIDGE_Z + EAVE_Z) / 2), bz(sy * hy / 2), SHOP_WHITE));
    for (let i = 0; i < 17; i++) {
      const x = -hx + 0.65 + i * (CAN_X - 1.3) / 16;
      parts.push(boxPart(u(0.09), u(0.09), u(hy), bx(x), u((RIDGE_Z + EAVE_Z) / 2 + 0.08), bz(sy * hy / 2), CANOPY_RED));
    }
    parts.push(boxPart(u(CAN_X + 0.1), u(0.3), u(0.1), 0, u(EAVE_Z - 0.1), bz(sy * (hy + 0.06)), CANOPY_RED));
    parts.push(boxPart(u(CAN_X + 0.1), u(0.18), u(0.22), 0, u(EAVE_Z - 0.26), bz(sy * (hy + 0.14)), STEEL_DARK));
    for (const sx of [-1, 1]) {
      parts.push(cylPart(u(0.06), u(0.06), u(EAVE_Z - 0.35), 8, bx(sx * COL_X), u((EAVE_Z - 0.35) / 2), bz(sy * (hy + 0.14)), STEEL_DARK));
    }
    parts.push(boxPart(u(CAN_X - 3.0), u(0.06), u(0.7), 0, u(RIDGE_Z - 0.11), bz(sy * 2.4), GLASS));
  }
  parts.push(boxPart(u(CAN_X + 0.1), u(0.14), u(0.42), 0, u(RIDGE_Z + 0.05), 0, CANOPY_RED));

  // ── ④ 加油岛 ×2 + 加油机 ×4 ────────────────────────────────────────
  for (const sy of [-1, 1]) {
    const iy = sy * ISL_Y;
    parts.push(boxPart(u(ISL_L), u(ISL_H), u(ISL_W), 0, u(ISL_H / 2), bz(iy), CONCRETE));
    for (const s2 of [-1, 1]) {
      parts.push(boxPart(u(1.6), u(ISL_H + 0.05), u(0.17),
        bx(s2 * (ISL_L / 2 - 0.8)), u((ISL_H + 0.05) / 2), bz(iy + s2 * (ISL_W / 2 - 0.08)), STEEL));
    }
    for (const sx of [-1, 1]) {
      const px = sx * PUMP_X;
      parts.push(boxPart(u(1.34), u(0.16), u(0.84), bx(px), u(ISL_H + 0.08), bz(iy), STEEL_DARK));
      parts.push(boxPart(u(1.2), u(1.9), u(0.7), bx(px), u(ISL_H + 1.11), bz(iy), SHOP_WHITE));
      parts.push(boxPart(u(1.24), u(0.55), u(0.74), bx(px), u(ISL_H + 0.435), bz(iy), STEEL_DARK));
      for (const dx of [-0.68, 0.68]) {
        parts.push(boxPart(u(0.09), u(0.3), u(0.09), bx(px + dx), u(ISL_H + 2.21), bz(iy), STEEL_DARK));
      }
      parts.push(boxPart(u(1.6), u(0.14), u(0.9), bx(px), u(ISL_H + 2.43), bz(iy), CANOPY_RED));
      parts.push(boxPart(u(0.6), u(0.36), u(0.04), bx(px), u(ISL_H + 1.62), bz(iy - 0.37), SIGN_AMBER));
      parts.push(boxPart(u(0.7), u(0.46), u(0.05), bx(px), u(ISL_H + 1.62), bz(iy - 0.35), STEEL_DARK));
      for (const dx of [-0.34, 0.34]) {
        parts.push(boxPart(u(0.14), u(0.26), u(0.1), bx(px + dx), u(ISL_H + 1.1), bz(iy - 0.38), STEEL_DARK));
        parts.push(boxPart(u(0.05), u(0.55), u(0.05), bx(px + dx * 0.4), u(ISL_H + 1.48), bz(iy - 0.34), STEEL_DARK));
      }
    }
  }

  // ── ⑤ 便利店：勒脚 + 墙体 + 幕墙 + 门 + 雨棚 + 门头 + 女儿墙 + 屋面 ────
  const pz = SHOP_PLINTH;
  parts.push(boxPart(u(SHOP_L + 0.3), u(SHOP_PLINTH), u(SHOP_D + 0.3), 0, u(SHOP_PLINTH / 2), bz(SHOP_Y), CONCRETE_DARK));
  parts.push(boxPart(u(SHOP_L), u(SHOP_H - SHOP_PLINTH), u(SHOP_D), 0, u(pz + (SHOP_H - SHOP_PLINTH) / 2), bz(SHOP_Y), SHOP_WHITE));
  parts.push(boxPart(u(SHOP_L + 0.06), u(0.45), u(SHOP_D + 0.06), 0, u(pz + 0.22), bz(SHOP_Y), CANOPY_RED));
  parts.push(boxPart(u(SHOP_L - 1.2), u(1.9), u(0.06), 0, u(pz + 1.35), bz(SHOP_Y - SHOP_D / 2 - 0.01), GLASS));
  for (let i = -3; i <= 3; i++) {
    parts.push(boxPart(u(0.08), u(1.9), u(0.1), bx(i * 1.5), u(pz + 1.35), bz(SHOP_Y - SHOP_D / 2 - 0.03), STEEL_DARK));
  }
  parts.push(boxPart(u(1.2), u(2.3), u(0.08), bx(2.2), u(pz + 1.15), bz(SHOP_Y - SHOP_D / 2 - 0.04), STEEL_DARK));
  parts.push(boxPart(u(SHOP_L + 1.0), u(0.25), u(1.8), 0, u(pz + 3.0), bz(SHOP_Y - SHOP_D / 2 - 0.9), CANOPY_RED));
  for (let i = 0; i < 4; i++) {
    parts.push(boxPart(u(0.1), u(0.3), u(1.7), bx(-4.5 + i * 3.0), u(pz + 2.8), bz(SHOP_Y - SHOP_D / 2 - 0.85), STEEL_DARK));
  }
  parts.push(boxPart(u(6.36), u(0.85), u(0.16), 0, u(pz + 3.52), bz(SHOP_Y - SHOP_D / 2 - 0.24), CANOPY_RED));
  parts.push(boxPart(u(6.0), u(0.55), u(0.22), 0, u(pz + 3.52), bz(SHOP_Y - SHOP_D / 2 - 0.32), LIGHTBOX));
  parts.push(boxPart(u(SHOP_L + 0.3), u(0.3), u(SHOP_D + 0.3), 0, u(pz + SHOP_H - SHOP_PLINTH + 0.15), bz(SHOP_Y), CONCRETE_DARK));
  parts.push(boxPart(u(SHOP_L + 0.4), u(0.16), u(SHOP_D + 0.4), 0, u(pz + SHOP_H - SHOP_PLINTH + 0.38), bz(SHOP_Y), STEEL));
  for (const dx of [-4.2, -3.0]) {
    parts.push(boxPart(u(0.95), u(0.75), u(0.42), bx(dx), u(pz + 0.7), bz(SHOP_Y + SHOP_D / 2 + 0.3), STEEL_DARK));
    parts.push(boxPart(u(1.05), u(0.1), u(0.55), bx(dx), u(pz + 0.3), bz(SHOP_Y + SHOP_D / 2 + 0.3), STEEL_DARK));
  }

  // ── ⑥ 高杆形象牌：基础 + 杆 + 灯箱 + 红带 + 边框 + 检修梯 ──────────────
  const tx = bx(TOTEM_X), tz = bz(TOTEM_Y);
  parts.push(boxPart(u(1.3), u(0.3), u(1.3), tx, u(0.15), tz, CONCRETE_DARK));
  parts.push(cylPart(u(TOTEM_POLE_R), u(TOTEM_POLE_R + 0.05), u(TOTEM_POLE_H), 12, tx, u(0.3 + TOTEM_POLE_H / 2), tz, STEEL));
  parts.push(boxPart(u(TOTEM_PANEL_L), u(TOTEM_PANEL_H), u(TOTEM_PANEL_T), tx, u(TOTEM_PANEL_Z), tz, LIGHTBOX));
  for (const s2 of [-1, 1]) {
    parts.push(boxPart(u(TOTEM_PANEL_L - 0.3), u(0.55), u(TOTEM_PANEL_T + 0.03), tx, u(TOTEM_PANEL_Z - 0.55), tz, CANOPY_RED));
    parts.push(boxPart(u(0.1), u(TOTEM_PANEL_H + 0.2), u(TOTEM_PANEL_T + 0.06), tx + u(s2 * (TOTEM_PANEL_L / 2 + 0.05)), u(TOTEM_PANEL_Z), tz, STEEL_DARK));
    parts.push(boxPart(u(TOTEM_PANEL_L + 0.2), u(0.1), u(TOTEM_PANEL_T + 0.06), tx, u(TOTEM_PANEL_Z + s2 * (TOTEM_PANEL_H / 2 + 0.05)), tz, STEEL_DARK));
  }
  for (const s2 of [-1, 1]) {
    parts.push(boxPart(u(0.06), u(6.6), u(0.06), tx + u(0.42), u(3.6), tz + u(s2 * 0.22), STEEL_DARK));
  }
  for (let i = 0; i < 6; i++) {
    parts.push(boxPart(u(0.06), u(0.05), u(0.5), tx + u(0.42), u(0.9 + i * 1.1), tz, STEEL_DARK));
  }
  return parts;
}

export function GasStation() {
  const geo = useMemo(() => mergeParts(gasStationParts()), []);
  useEffect(() => () => geo.dispose(), [geo]);

  return (
    <CivicGlbPiece
      glbName="gas_station"
      dimsKey="gasStation"
      infoId="civic.gas-station"
      anchorY={5.58}
      position={[5.8, 0, 6.7]}
      litMaterials={LIT}
      fallback={
        <mesh geometry={geo}>
          <meshStandardMaterial vertexColors roughness={FLAT_ROUGH} metalness={FLAT_METAL} />
        </mesh>
      }
    />
  );
}
