/**
 * BicycleRack — **倒 U 停车架 + 斜靠自行车**（批次 42 街具真实感 · B1/C3/D1 正名）：
 *
 * 正名（C3）：旧组件渲染的是"实心圆柱轮玩具车"且**完全没有停车架**（名实不符），
 * 批次 42 起为「倒 U 停车架 ×2 + 写实自行车（辐条轮/三角架/车把/座垫/曲柄脚踏/
 * 挡泥板/脚撑）」，catalog `prop.bicycle-rack` 文案同步为「自行车停放架」。
 *
 * 双渲染路径（CLAUDE.md §27.3 硬约束 3/7，包围盒同取 cityScale.REAL_DIMS_M.bikeRack
 * = 1.80 × 1.10 × 0.62 m，长边 X 沿道路向）：
 *   - GLB 优先：`road/bike_rack.glb` 双节点 `BikeRack` + `BikeRack_Bike`（停车架 +
 *     斜靠自行车，同 GLB）—— `<Model>` 克隆**整场景**故两节点都在（任务契约 B1b）；
 *   - 程序化 fallback：倒 U 架 ×2（⌀0.035 管）+ 写实自行车（12 辐条轮 ×2）。
 *
 * 布点（批次 42 C1）：城区朝 finance 侧缘（与 mailbox 错开），rotation 面向路。
 */

import { memo, useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { Model, type MergePart, mergeParts } from '@/engine3d';
import { modelUrl } from '@/assets/models';
import { u, worldDims, sizeTargetFor } from '../cityScale';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

/** 尺寸/落地校验目标（dev 态 glbSizeGuard；与 fallback 同行表值）。 */
const RACK_SIZE_TARGET = sizeTargetFor('bikeRack', { label: 'road/bike_rack' });

// ── fallback 构图常量（米制经 u()；包络取 worldDims('bikeRack') 唯一事实来源）──
const D = worldDims('bikeRack'); // x 0.18 / y 0.11 / z 0.062（世界单位）
const RACK_STEEL = '#9aa3ad';
const FRAME_COLOR = '#2f5f9e'; // 城市车蓝
const TIRE_COLOR = '#1a1a1d';
const RIM_COLOR = '#c5c8ce';
const SEAT_COLOR = '#2a2e36';
const FENDER_COLOR = '#3a414c';

const WHEEL_R = u(0.33); // 26″ 轮径 0.66 m
const WHEEL_T = u(0.035);
const WHEEL_X = u(0.55); // 前后轮心距 1.10 m
const TUBE_R = u(0.016); // 车架管 ⌀0.032
const RACK_R = u(0.0175); // 倒 U 架 ⌀0.035
const RACK_H = u(0.8);
const RACK_SPAN = u(0.85); // 跨距 0.85 m

/** TRS 矩阵（旋转四元数 × 平移）。 */
function trs(x: number, y: number, z: number, q?: THREE.Quaternion): THREE.Matrix4 {
  return new THREE.Matrix4().compose(
    new THREE.Vector3(x, y, z),
    q ?? new THREE.Quaternion(),
    new THREE.Vector3(1, 1, 1),
  );
}

/** 细圆柱杆件：从 (x0,y0,z0) 到 (x1,y1,z1)。 */
function tubePart(
  x0: number, y0: number, z0: number,
  x1: number, y1: number, z1: number,
  r: number,
  color: string,
): MergePart {
  const dx = x1 - x0;
  const dy = y1 - y0;
  const dz = z1 - z0;
  const len = Math.sqrt(dx * dx + dy * dy + dz * dz) || 1e-6;
  const geo = new THREE.CylinderGeometry(r, r, len, 6);
  // 圆柱局部 +Y 对齐杆向：q = 从 +Y 旋到 dir
  const q = new THREE.Quaternion().setFromUnitVectors(
    new THREE.Vector3(0, 1, 0),
    new THREE.Vector3(dx / len, dy / len, dz / len),
  );
  return { geo, matrix: trs((x0 + x1) / 2, (y0 + y1) / 2, (z0 + z1) / 2, q), color };
}

/** 侧放圆柱（轴沿 Z，用于轮毂/曲柄轴）。 */
function axlePart(x: number, y: number, z: number, r: number, h: number, color: string): MergePart {
  const geo = new THREE.CylinderGeometry(r, r, h, 10);
  const q = new THREE.Quaternion().setFromEuler(new THREE.Euler(Math.PI / 2, 0, 0));
  return { geo, matrix: trs(x, y, z, q), color };
}

/** 单个辐条轮：胎圈 + 轮辋 + 12 辐条 + 花鼓（轮平面 = XY，轴沿 Z）。 */
function wheelParts(cx: number, cz: number): MergePart[] {
  const parts: MergePart[] = [];
  const cy = WHEEL_R;
  // 胎圈（薄圆柱侧放）
  parts.push(axlePart(cx, cy, cz, WHEEL_R, WHEEL_T, TIRE_COLOR));
  // 轮辋（亮金属，半径 0.88·R）
  parts.push(axlePart(cx, cy, cz, WHEEL_R * 0.88, WHEEL_T * 0.7, RIM_COLOR));
  // 12 辐条（细圆柱，径向）
  for (let i = 0; i < 12; i++) {
    const a = (i / 12) * Math.PI * 2;
    const len = WHEEL_R * 0.86;
    const geo = new THREE.CylinderGeometry(u(0.004), u(0.004), len, 4);
    const q = new THREE.Quaternion().setFromEuler(new THREE.Euler(0, 0, a - Math.PI / 2));
    parts.push({
      geo,
      matrix: trs(cx + Math.cos(a) * len * 0.5, cy + Math.sin(a) * len * 0.5, cz, q),
      color: RIM_COLOR,
    });
  }
  // 花鼓
  parts.push(axlePart(cx, cy, cz, u(0.03), WHEEL_T * 2.2, RIM_COLOR));
  return parts;
}

/**
 * fallback 原语 → 顶点色合并 1 mesh：倒 U 架 ×2 + 写实自行车。
 * 包络推导：架腿 x=±0.875/±0.025 m（两架 0.85 m 跨距沿 X 拼接）、高 0.80 m；
 * 自行车轮心 x=±0.55 m、车把宽 0.55 m 沿 Z ⇒ 总包络 1.80 × 1.10 × 0.62 = 表值。
 */
function rackAndBikeParts(): MergePart[] {
  const parts: MergePart[] = [];
  const zRack = -u(0.28); // 停车架在车后侧（车向架倾靠；外缘 -0.30 m 仍在表值 z/2=0.31 内）
  // ── 倒 U 停车架 ×2（⌀0.035 不锈钢管；两架沿 X 拼接、外缘贴表值 x）──
  for (const cx of [-D.x / 2 + RACK_SPAN / 2, D.x / 2 - RACK_SPAN / 2]) {
    const x0 = cx - RACK_SPAN / 2;
    const x1 = cx + RACK_SPAN / 2;
    parts.push(tubePart(x0, 0, zRack, x0, RACK_H, zRack, RACK_R, RACK_STEEL));
    parts.push(tubePart(x1, 0, zRack, x1, RACK_H, zRack, RACK_R, RACK_STEEL));
    // 顶横杆（拱形简化为直杆）
    parts.push(tubePart(x0, RACK_H, zRack, x1, RACK_H, zRack, RACK_R, RACK_STEEL));
  }

  // ── 自行车（26″ 城市车，轮平面 XY、轴沿 Z；z 居中使车把 ±0.275 m 落在包络 z=0.62 内）──
  const zBike = 0;
  parts.push(...wheelParts(-WHEEL_X, zBike));
  parts.push(...wheelParts(+WHEEL_X, zBike));
  // 车架三角（五通 BB / 头管 / 座管）
  const bbY = u(0.28);
  const headX = WHEEL_X * 0.82;
  const headY = u(0.82);
  const seatX = -WHEEL_X * 0.28;
  const seatY = u(0.88);
  // 下管 / 上管 / 立管 / 后下叉 / 后上叉
  parts.push(tubePart(headX, headY, zBike, 0, bbY, zBike, TUBE_R, FRAME_COLOR));
  parts.push(tubePart(headX, headY - u(0.08), zBike, seatX, seatY - u(0.04), zBike, TUBE_R, FRAME_COLOR));
  parts.push(tubePart(0, bbY, zBike, seatX, seatY, zBike, TUBE_R, FRAME_COLOR));
  parts.push(tubePart(0, bbY, zBike, -WHEEL_X, WHEEL_R, zBike, TUBE_R * 0.85, FRAME_COLOR));
  parts.push(tubePart(seatX, seatY - u(0.06), zBike, -WHEEL_X, WHEEL_R, zBike, TUBE_R * 0.85, FRAME_COLOR));
  // 前叉（头管 → 前花鼓）
  parts.push(tubePart(headX, headY - u(0.12), zBike, WHEEL_X, WHEEL_R, zBike, TUBE_R * 0.85, FRAME_COLOR));
  // 座垫 + 座管
  parts.push(tubePart(seatX, seatY - u(0.1), zBike, seatX, seatY + u(0.02), zBike, TUBE_R * 0.7, RIM_COLOR));
  {
    const geo = new THREE.BoxGeometry(u(0.22), u(0.05), u(0.1));
    parts.push({ geo, matrix: trs(seatX - u(0.02), seatY + u(0.04), zBike), color: SEAT_COLOR });
  }
  // 车把（把立 + 横把，宽 0.55 m 沿 Z）+ 把套
  const barY = headY + u(0.23); // 把横管 y = 1.05 m（全高 1.10 内）
  parts.push(tubePart(headX, headY - u(0.05), zBike, headX + u(0.04), barY, zBike, TUBE_R * 0.7, RIM_COLOR));
  parts.push(tubePart(headX + u(0.04), barY, zBike - u(0.275), headX + u(0.04), barY, zBike + u(0.275), TUBE_R * 0.7, RIM_COLOR));
  for (const sz of [-1, 1]) {
    parts.push(tubePart(
      headX + u(0.04), barY, zBike + sz * u(0.2),
      headX + u(0.04), barY, zBike + sz * u(0.275),
      TUBE_R * 1.1, SEAT_COLOR,
    ));
  }
  // 曲柄 + 脚踏
  parts.push(axlePart(0, bbY, zBike, u(0.022), u(0.14), RIM_COLOR));
  parts.push(tubePart(0, bbY, zBike + u(0.06), u(0.09), bbY - u(0.07), zBike + u(0.06), TUBE_R * 0.6, RIM_COLOR));
  parts.push(tubePart(0, bbY, zBike - u(0.06), -u(0.09), bbY + u(0.07), zBike - u(0.06), TUBE_R * 0.6, RIM_COLOR));
  for (const [px, pz] of [[u(0.09), zBike + u(0.06)], [-u(0.09), zBike - u(0.06)]] as Array<[number, number]>) {
    const geo = new THREE.BoxGeometry(u(0.12), u(0.03), u(0.07));
    parts.push({ geo, matrix: trs(px, px > 0 ? bbY - u(0.07) : bbY + u(0.07), pz), color: SEAT_COLOR });
  }
  // 挡泥板（前后，薄弧条）
  for (const wx of [-WHEEL_X, WHEEL_X]) {
    for (let i = -2; i <= 2; i++) {
      const a = Math.PI / 2 + i * 0.38;
      const geo = new THREE.BoxGeometry(u(0.06), u(0.012), WHEEL_T * 1.6);
      const q = new THREE.Quaternion().setFromEuler(new THREE.Euler(0, 0, -a + Math.PI / 2));
      parts.push({
        geo,
        matrix: trs(wx + Math.cos(a) * WHEEL_R * 1.05, WHEEL_R + Math.sin(a) * WHEEL_R * 1.05, zBike, q),
        color: FENDER_COLOR,
      });
    }
  }
  // 脚撑 15°
  parts.push(tubePart(u(0.02), bbY - u(0.02), zBike - u(0.05), u(0.1), u(0.01), zBike - u(0.12), TUBE_R * 0.5, RIM_COLOR));
  return parts;
}

interface Props {
  x: number;
  z: number;
  rotation?: number;
}

// 批次 28 A1：memo —— props 稳定引用（原语 / 常量）。
export const BicycleRack = memo(function BicycleRack({ x, z, rotation = 0 }: Props) {
  const info = useObjectInfoProps('prop.bicycle-rack', { anchorY: 1.2 });
  const solidsGeo = useMemo(() => mergeParts(rackAndBikeParts()), []);
  useEffect(() => () => solidsGeo.dispose(), [solidsGeo]);

  return (
    <group {...info} position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* GLB 优先：整场景含 BikeRack + BikeRack_Bike 两节点（children = fallback） */}
      <Model url={modelUrl('road', 'bike_rack')} sizeTarget={RACK_SIZE_TARGET}>
        <mesh geometry={solidsGeo}>
          <meshStandardMaterial vertexColors roughness={0.62} metalness={0.3} />
        </mesh>
      </Model>
    </group>
  );
});

export default BicycleRack;
