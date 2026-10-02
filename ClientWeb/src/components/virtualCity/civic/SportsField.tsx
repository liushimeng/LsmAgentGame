/**
 * 体育场（18-AA · §5.2 SportsField）—— 200 m 半圆式田径场。
 *
 * 批次 47「体育场真实感」做了两件事，都不是换皮：
 *
 * ── D1 搬迁：(-16.5, 22) → (10, 38.5) ──────────────────────────────────
 *   原布点落在教育园区（`-8, 22`）/ 医疗城（`8, 22`）之间；而 **(10, 40) 有个叫
 *   「体育新城」的区**（`virtualCity.ts` 的 `sports_new_city`），其官方区简介写的是
 *   「**围绕体育场馆建设的新城板块，赛事与演艺活动带动周边消费**」—— 一个以体育场
 *   命名、简介写着体育场、实际空无一馆的区。真实性包含城市规划的真实性。
 *   新布点经 9 项约束预检（设计 47 §1.3）：体育新城底板 / 高架铁路 z=46 走廊 /
 *   湿地公园 / 金融副中心 / 大学城 / 湾区新城 / 方格骨干 / 一环路 r=20 / 运河 —— 全过。
 *
 * ── D2 重塑：椭圆跑道 + 圆内场 → 标准 200 m 体育场形 ────────────────────
 *   批次 18-AA 是 `TorusGeometry(u(28), u(4))` 压扁成椭圆 + `CircleGeometry(u(24))`
 *   **圆**内场，且分道线半径漏乘了 `u()`（批次 46 登记 L3）。田径场是「两段直道 +
 *   两个半圆」；r=28 m 的**圆**内场套不进任何标准足球场（足球场是矩形）。
 *   现行规格（GB/T 跑道通用参数）：
 *     r = 20.00 m + 6 道 × 1.22 m ⇒ 外半径 27.32 m、单侧直道 37.17 m
 *     闭式 2×37.17 + 2π×20.00 = **200.00 m** ✓、跑道外接 91.8 × 54.6 m
 *     200 m 场放不下 11 人制（105×68 m），内场 74.34×40 m ⇒ **七人制人造草 60×32 m**
 *
 * ── 保留地 ──────────────────────────────────────────────────────────────
 *   田体育场 110×74 m 会被 `DistrictBuildings` 的楼体压上，故在 `cityObstacles`
 *   登记 `SPORTS_FIELD_AREA`（110×74 m @ 中心 (10, 38.5)）并纳入 `isBuildable`。
 *   尺寸口径与 `REAL_DIMS_M.sportsField` 同源，两处须同改。
 *
 * ── 坐标系（与 `build_sports_field.py` 对齐）─────────────────────────────
 *   建模脚本在 Blender 的 **XY 平面**铺场（X = 场长、Y = 场宽、Z = 高），导出自动
 *   Yup ⇒ three 的 **XZ 平面**对应之（x = 场长、z = 场宽、y = 高）。`center_content_xz`
 *   对本件轴对称内容盒幂等，偏移 0。⇒ 主看台（Blender +Y）在 three 的 **-Z 侧**。
 *
 * ── 降级链（§27.3-3）──────────────────────────────────────────────────
 *   `blenderModelsEnabled()` → `modelUrl` → GLB 载入 → 否则本文件的程序化几何
 *   （按新尺寸重建，§27.3-7 双路径同尺寸）。尺寸守卫 `sizeTargetFor('sportsField')`。
 */
import { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { boxPart, cylPart, mergeParts, type MergePart } from '@/engine3d';
import { u } from '../cityScale';
import { CivicGlbPiece } from './CivicGlb';

const TRACK = '#a35a48';        // track_surface：塑胶跑道砖红
const LINE = '#e6e2d6';         // paint_road：标线米白
const TURF = '#54903f';         // turf_sports：七人制人造草
const CONCRETE = '#b6b3ad';
const CONCRETE_DARK = '#8d8a85';
const STEEL = '#8d949c';
const STEEL_DARK = '#4e545c';

/** 纯色件统一材质参数。 */
const FLAT_ROUGH = 0.92;
const FLAT_METAL = 0.05;

/** 夜间自发光：4 根灯杆 × 6 头投光灯（材质名匹配 GLB 的 `Stadium_Floodlight_Mat`）。 */
const LIT = { Stadium_Floodlight: 2.6 };

// ── 跑道几何（米）—— 与 `build_sports_field.py` 一一对应 ──────────────────
const R_IN = 20.0;
const LANE_W = 1.22;
const R_OUT = R_IN + 6 * LANE_W;      // 27.32
const HALF_STRAIGHT = 37.17 / 2;      // 18.585（直道半长：4×18.585 + 2π×20 = 200.00）
const SAFETY = 3.0;
const Z_SLAB = 0.05, Z_SURF = 0.06, Z_RUNWAY = 0.07;
const Z_TURF = 0.08, Z_KERB = 0.10, Z_LINE = 0.115;
const SITE_HX = 55.0, SITE_HZ = 37.0;
const LANE_CX = 52.0, LANE_CY = 34.5;  // 消防车道中心线
const PITCH_L = 60.0, PITCH_W = 32.0, LINE_W = 0.12;
const GOAL_W = 7.32, GOAL_H = 2.44;
const BOX_L = 40.32, BOX_W = 16.5;    // 罚球区
const GA_L = 18.32, GA_W = 5.5;       // 球门区
const STAND_ROWS = 5, STAND_ROW_D = 0.85, STAND_RISE = 0.45, STAND_L = 56.0, STAND_RAIL = 1.10;
const END_ROWS = 2, END_STAND_L = 24.0;
const LAMP_X = 42.0, LAMP_Y = 33.0, LAMP_H = 15.0, LAMP_HEADS = 6;
const FENCE_H = 4.0, FENCE_SPACING = 6.0;
const CANOPY_Z = 8.0;
const FLAG_H = 8.0;
const EQUIP_L = 6.0, EQUIP_D = 4.0, EQUIP_H = 3.0;

/** 平躺圆环 / 圆盘（three 的 Circle/Ring 在 XY 平面，绕 X 转 -90° 落到 XZ 地面）。 */
function flatMatrix(x: number, y: number, z: number) {
  return new THREE.Matrix4().compose(
    new THREE.Vector3(x, y, z),
    new THREE.Quaternion().setFromEuler(new THREE.Euler(-Math.PI / 2, 0, 0)),
    new THREE.Vector3(1, 1, 1),
  );
}

/**
 * 体育场形环带：**两直条 + 两半环**（three 无体育场图元，环用 `RingGeometry` 平躺）。
 * @param rIn 内半径 @param rOut 外半径 @param y 标高
 */
function stadiumBand(parts: MergePart[], rIn: number, rOut: number, y: number, color: string) {
  const w = rOut - rIn;
  const rMid = (rIn + rOut) / 2;
  for (const sz of [-1, 1]) {
    parts.push(boxPart(u(HALF_STRAIGHT * 2), 0.02, u(w), 0, u(y), u(sz * rMid), color));
  }
  for (const sx of [-1, 1]) {
    parts.push({
      geo: new THREE.RingGeometry(u(rIn), u(rOut), 40),
      matrix: flatMatrix(u(sx * HALF_STRAIGHT), u(y), 0),
      color,
    });
  }
}

/** 体育场形实心面（内场）：矩形直段 + 两个半圆。 */
function stadiumDisc(parts: MergePart[], r: number, y: number, color: string) {
  parts.push(boxPart(u(HALF_STRAIGHT * 2), 0.02, u(r * 2), 0, u(y), 0, color));
  for (const sx of [-1, 1]) {
    parts.push({
      geo: new THREE.CircleGeometry(u(r), 40),
      matrix: flatMatrix(u(sx * HALF_STRAIGHT), u(y), 0),
      color,
    });
  }
}

function sportsFieldParts(): MergePart[] {
  const parts: MergePart[] = [];

  // ① 混凝土基面 + 消防车道边线
  parts.push(boxPart(u(SITE_HX * 2), u(Z_SLAB), u(SITE_HZ * 2), 0, u(Z_SLAB / 2), 0, CONCRETE));
  for (const sx of [-1, 1]) {
    parts.push(boxPart(u(0.15), 0.02, u(SITE_HZ * 2 - 1), u(sx * (LANE_CX + 2)), u(Z_SLAB + 0.02), 0, LINE));
  }
  for (const sz of [-1, 1]) {
    parts.push(boxPart(u(SITE_HX * 2 - 1), 0.02, u(0.15), 0, u(Z_SLAB + 0.02), u(sz * (LANE_CY + 2)), LINE));
  }

  // ② 跑道外安全区 / 跑道面 / 内场草皮 / 内场路缘
  stadiumBand(parts, R_OUT, R_OUT + SAFETY, Z_SURF, TRACK);
  stadiumBand(parts, R_IN, R_OUT, Z_RUNWAY, TRACK);
  stadiumDisc(parts, R_IN, Z_TURF, TURF);
  stadiumBand(parts, R_IN - 0.15, R_IN, Z_KERB, TRACK);
  // 分道线 5 条（6 道分 5 线，线宽 0.12 m）
  for (let i = 1; i < 6; i++) {
    const r = R_IN + i * LANE_W;
    stadiumBand(parts, r - LINE_W / 2, r + LINE_W / 2, Z_RUNWAY + 0.01, LINE);
  }
  // 弯道起跑线
  for (const sx of [-1, 1]) {
    parts.push(boxPart(u(0.12), 0.02, u(R_OUT - R_IN), u(sx * HALF_STRAIGHT), u(Z_LINE), u(-(R_IN + R_OUT) / 2), LINE));
  }

  // ③ 七人制足球场标线（60 × 32 m）
  const hx = PITCH_L / 2, hz = PITCH_W / 2;
  parts.push(boxPart(u(PITCH_L), 0.02, u(LINE_W), 0, u(Z_LINE), u(hz - LINE_W / 2), LINE));
  parts.push(boxPart(u(PITCH_L), 0.02, u(LINE_W), 0, u(Z_LINE), u(-hz + LINE_W / 2), LINE));
  parts.push(boxPart(u(LINE_W), 0.02, u(PITCH_W), u(hx - LINE_W / 2), u(Z_LINE), 0, LINE));
  parts.push(boxPart(u(LINE_W), 0.02, u(PITCH_W), u(-hx + LINE_W / 2), u(Z_LINE), 0, LINE));
  parts.push(boxPart(u(LINE_W), 0.02, u(PITCH_W), 0, u(Z_LINE), 0, LINE));
  parts.push({
    geo: new THREE.RingGeometry(u(9.15 - LINE_W / 2), u(9.15 + LINE_W / 2), 48),
    matrix: flatMatrix(0, u(Z_LINE), 0),
    color: LINE,
  });
  for (const sx of [-1, 1]) {
    const bx = sx * (hx - BOX_L / 2);
    parts.push(boxPart(u(BOX_L), 0.02, u(LINE_W), u(bx), u(Z_LINE), u(BOX_W / 2 - LINE_W / 2), LINE));
    parts.push(boxPart(u(BOX_L), 0.02, u(LINE_W), u(bx), u(Z_LINE), u(-BOX_W / 2 + LINE_W / 2), LINE));
    parts.push(boxPart(u(LINE_W), 0.02, u(BOX_W), u(bx - sx * (BOX_L / 2 - LINE_W / 2)), u(Z_LINE), 0, LINE));
    const gx = sx * (hx - GA_L / 2);
    parts.push(boxPart(u(GA_L), 0.02, u(LINE_W), u(gx), u(Z_LINE), u(GA_W / 2 - LINE_W / 2), LINE));
    parts.push(boxPart(u(GA_L), 0.02, u(LINE_W), u(gx), u(Z_LINE), u(-GA_W / 2 + LINE_W / 2), LINE));
    parts.push(boxPart(u(LINE_W), 0.02, u(GA_W), u(gx - sx * (GA_L / 2 - LINE_W / 2)), u(Z_LINE), 0, LINE));
    parts.push(cylPart(u(0.10), u(0.10), 0.02, 8, u(sx * (hx - 11.0)), u(Z_LINE), 0, LINE));
    // 球门（横梁跨门宽 Z、柱高 Y）
    parts.push(boxPart(u(0.12), u(0.12), u(GOAL_W), u(sx * hx), u(GOAL_H), 0, STEEL));
    for (const sz of [-1, 1]) {
      parts.push(boxPart(u(0.12), u(GOAL_H), u(0.12), u(sx * hx), u(GOAL_H / 2), u(sz * (GOAL_W / 2 - 0.06)), STEEL));
      // 角旗
      parts.push(cylPart(u(0.05), u(0.05), u(1.5), 6, u(sx * hx), u(Z_TURF + 0.75), u(sz * hz), STEEL_DARK));
    }
  }

  // ④ 主看台（5 排）+ 后排 1.10 m 栏杆 + 纵过道扶手 + 罩棚
  const y0 = -(R_OUT + SAFETY);   // three -Z 侧 = Blender +Y 侧
  for (let r = 0; r < STAND_ROWS; r++) {
    const h = STAND_RISE * (r + 1) / 2;
    parts.push(boxPart(u(STAND_L), u(h * 2), u(STAND_ROW_D), 0, u(h), u(y0 - r * STAND_ROW_D - STAND_ROW_D / 2), CONCRETE));
    parts.push(boxPart(u(STAND_L - 1.0), u(0.06), u(0.40), 0, u(STAND_RISE * (r + 1) + 0.45), u(y0 - r * STAND_ROW_D - STAND_ROW_D / 2), STEEL));
  }
  const backY = y0 - STAND_ROWS * STAND_ROW_D;
  for (const [h, z] of [[STAND_RAIL, backY - 0.1], [0.90, y0 + 0.1]] as const) {
    parts.push(boxPart(u(STAND_L), u(0.06), u(0.06), 0, u(h), u(z), STEEL));
    const n = Math.floor(STAND_L / 2.5);
    for (let i = 0; i <= n; i++) {
      parts.push(boxPart(u(0.08), u(h), u(0.08), u(-STAND_L / 2 + i * (STAND_L / n)), u(h / 2), u(z), STEEL));
    }
  }
  for (const px of [-24, -8, 8, 24]) {
    parts.push(boxPart(u(0.06), u(0.06), u(STAND_ROWS * STAND_ROW_D), u(px), u(0.9), u(y0 - STAND_ROWS * STAND_ROW_D / 2), STEEL));
  }
  for (let i = 0; i < 6; i++) {
    parts.push(boxPart(u(0.35), u(CANOPY_Z - 2.8), u(0.35),
      u(-STAND_L / 2 + 5 + i * (STAND_L - 10) / 5), u((CANOPY_Z + 2.8) / 2), u(backY + 0.5), STEEL));
  }
  parts.push(boxPart(u(STAND_L + 6.0), u(0.30), u(5.0), 0, u(CANOPY_Z), u(y0 - 1.5), STEEL));
  for (let i = 0; i < 7; i++) {
    parts.push(boxPart(u(0.30), u(0.45), u(5.0), u(-STAND_L / 2 - 2.5 + i * (STAND_L + 5) / 6), u(CANOPY_Z - 0.36), u(y0 - 1.5), STEEL));
  }

  // ⑤ 端部看台 ×2（弯道外侧，2 排）
  const ex = HALF_STRAIGHT + R_OUT + SAFETY;
  for (const sx of [-1, 1]) {
    for (let r = 0; r < END_ROWS; r++) {
      const h = STAND_RISE * (r + 1) / 2;
      parts.push(boxPart(u(STAND_ROW_D), u(h * 2), u(END_STAND_L),
        u(sx * (ex + r * STAND_ROW_D + STAND_ROW_D / 2)), u(h), 0, CONCRETE));
      parts.push(boxPart(u(0.40), u(0.06), u(END_STAND_L - 1.0),
        u(sx * (ex + r * STAND_ROW_D + STAND_ROW_D / 2)), u(STAND_RISE * (r + 1) + 0.45), 0, STEEL));
    }
    parts.push(boxPart(u(0.06), u(1.10), u(END_STAND_L), u(sx * (ex + END_ROWS * STAND_ROW_D + 0.1)), u(1.10), 0, STEEL));
  }

  // ⑥ 灯杆 ×4（15 m，每杆 6 头）
  for (const sx of [-1, 1]) {
    for (const sz of [-1, 1]) {
      const px = sx * LAMP_X, pz = sz * LAMP_Y;
      parts.push(boxPart(u(1.2), u(0.4), u(1.2), u(px), u(0.2), u(pz), CONCRETE_DARK));
      parts.push(cylPart(u(0.16), u(0.32), u(LAMP_H), 8, u(px), u(0.4 + LAMP_H / 2), u(pz), STEEL));
      parts.push(boxPart(u(0.16), u(0.16), u(5.0), u(px), u(0.4 + LAMP_H + 0.1), u(pz), STEEL));
      for (let k = 0; k < LAMP_HEADS; k++) {
        const hy = pz - 2.5 + k * (5.0 / (LAMP_HEADS - 1));
        parts.push(boxPart(u(0.55), u(0.30), u(0.70), u(px + 0.3), u(0.4 + LAMP_H - 0.1), u(hy), LINE));
      }
    }
  }

  // ⑦ 围网 4.0 m（立柱 @6 m + 三道横杆 + 网片）
  const fx = SITE_HX - 0.3, fz = SITE_HZ - 0.3;
  const corners: Array<[number, number]> = [[-fx, -fz], [fx, -fz], [fx, fz], [-fx, fz]];
  for (let e = 0; e < 4; e++) {
    const [ax, az] = corners[e];
    const [bx2, bz2] = corners[(e + 1) % 4];
    const segLen = Math.hypot(bx2 - ax, bz2 - az);
    const alongX = Math.abs(bx2 - ax) > Math.abs(bz2 - az);
    const n = Math.max(2, Math.floor(segLen / FENCE_SPACING));
    for (let i = 0; i <= n; i++) {
      const t = i / n;
      parts.push(boxPart(u(0.10), u(FENCE_H), u(0.10), u(ax + (bx2 - ax) * t), u(FENCE_H / 2), u(az + (bz2 - az) * t), STEEL));
    }
    for (const h of [0.6, 2.0, FENCE_H - 0.1]) {
      parts.push(boxPart(u(alongX ? segLen : 0.06), u(0.06), u(alongX ? 0.06 : segLen),
        u((ax + bx2) / 2), u(h), u((az + bz2) / 2), STEEL));
    }
    parts.push(boxPart(u(alongX ? segLen : 0.03), u(FENCE_H - 0.3), u(alongX ? 0.03 : segLen),
      u((ax + bx2) / 2), u(FENCE_H / 2), u((az + bz2) / 2), CONCRETE_DARK));
  }

  // ⑧ 旗杆 ×3 + 器材室
  for (let i = 0; i < 3; i++) {
    parts.push(cylPart(u(0.06), u(0.08), u(FLAG_H), 6, u(-10 + i * 2), u(Z_SLAB + FLAG_H / 2), u(SITE_HZ - 2.5), STEEL));
  }
  parts.push(boxPart(u(EQUIP_L), u(EQUIP_H), u(EQUIP_D), u(-30), u(Z_SLAB + EQUIP_H / 2), u(SITE_HZ - 4.0), CONCRETE));
  parts.push(boxPart(u(EQUIP_L + 0.4), u(0.16), u(EQUIP_D + 0.4), u(-30), u(Z_SLAB + EQUIP_H + 0.08), u(SITE_HZ - 4.0), STEEL));
  return parts;
}

export function SportsField() {
  const geo = useMemo(() => mergeParts(sportsFieldParts()), []);
  useEffect(() => () => geo.dispose(), [geo]);

  return (
    <CivicGlbPiece
      glbName="sports_field"
      dimsKey="sportsField"
      infoId="civic.sports-field"
      anchorY={1.5}
      position={[10, 0, 38.5]}
      litMaterials={LIT}
      fallback={
        <mesh geometry={geo} receiveShadow>
          <meshStandardMaterial vertexColors roughness={FLAT_ROUGH} metalness={FLAT_METAL} />
        </mesh>
      }
    />
  );
}
