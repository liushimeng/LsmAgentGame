/**
 * BicycleRack — 自行车（15-3D城市全面真实感深化 · 阶段 M）：
 *
 * 简化的自行车几何（纯几何，无贴图依赖）：
 *   - 双轮（圆柱侧放）+ 车架（细圆柱组合）+ 座椅（小盒）
 *   - 真实尺度约 1.7m 长 × 1.2m 高，符合停车状态
 *
 * 米制统一经 cityScale.u()。
 *
 * 契约：lag_docs/虚拟城市/已实现/15-3D城市渲染深化/02-架构设计 §4.1。
 */

import { u } from '../cityScale';

const FRAME_COLOR = '#3a3f46';     // 深灰车架
const TIRE_COLOR = '#1a1a1d';      // 黑轮胎
const SEAT_COLOR = '#2a2e36';      // 黑座椅

interface Props {
  x: number;
  z: number;
  rotation?: number;
}

export function BicycleRack({ x, z, rotation = 0 }: Props) {
  const WHEEL_R = u(0.28);     // 轮半径
  const WHEEL_W = u(0.05);     // 轮厚
  const WHEEL_DX = u(0.55);    // 前后轮距
  const FRAME_R = u(0.018);    // 车架管半径
  const SEAT_H = u(0.85);      // 座椅高度

  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* 前轮（X+ 方向） */}
      <mesh position={[+WHEEL_DX, WHEEL_R, 0]} rotation={[0, 0, Math.PI / 2]}>
        <cylinderGeometry args={[WHEEL_R, WHEEL_R, WHEEL_W, 16]} />
        <meshStandardMaterial color={TIRE_COLOR} roughness={0.9} />
      </mesh>
      {/* 后轮 */}
      <mesh position={[-WHEEL_DX, WHEEL_R, 0]} rotation={[0, 0, Math.PI / 2]}>
        <cylinderGeometry args={[WHEEL_R, WHEEL_R, WHEEL_W, 16]} />
        <meshStandardMaterial color={TIRE_COLOR} roughness={0.9} />
      </mesh>
      {/* 车架下管（座管→五通） */}
      <mesh position={[0, SEAT_H * 0.5, 0]} rotation={[0, 0, Math.PI / 4]}>
        <cylinderGeometry args={[FRAME_R, FRAME_R, SEAT_H * 0.7, 6]} />
        <meshStandardMaterial color={FRAME_COLOR} metalness={0.5} roughness={0.4} />
      </mesh>
      {/* 车架立管（五通→头管） */}
      <mesh position={[+WHEEL_DX * 0.6, SEAT_H * 0.55, 0]} rotation={[0, 0, -Math.PI / 6]}>
        <cylinderGeometry args={[FRAME_R, FRAME_R, SEAT_H * 0.6, 6]} />
        <meshStandardMaterial color={FRAME_COLOR} metalness={0.5} roughness={0.4} />
      </mesh>
      {/* 座椅（小盒） */}
      <mesh position={[0, SEAT_H, 0]}>
        <boxGeometry args={[u(0.18), u(0.05), u(0.08)]} />
        <meshStandardMaterial color={SEAT_COLOR} roughness={0.7} />
      </mesh>
    </group>
  );
}

export default BicycleRack;