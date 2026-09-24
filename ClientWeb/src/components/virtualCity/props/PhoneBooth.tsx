/**
 * PhoneBooth — 电话亭（15-3D城市全面真实感深化 · 阶段 M）：
 *
 * 玻璃盒（半透明）+ 顶（红色/绿色）+ 底座（深灰）+ 内部电话听筒（极简）。
 * 米制统一经 cityScale.u()。
 *
 * 契约：lag_docs/虚拟城市/已实现/15-3D城市渲染深化/02-架构设计 §4.1。
 */

import { u } from '../cityScale';

const GLASS = '#a8d5e8';
const FRAME = '#5a6270';

interface Props {
  x: number;
  z: number;
  rotation?: number;
  variant?: 'red' | 'green';
}

export function PhoneBooth({ x, z, rotation = 0, variant = 'red' }: Props) {
  const W = u(0.6);
  const D = u(0.6);
  const H = u(2.2);
  const ROOF_T = u(0.1);
  const topColor = variant === 'red' ? '#c8453a' : '#3f8a48';

  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* 底座（深灰） */}
      <mesh castShadow position={[0, u(0.1), 0]}>
        <boxGeometry args={[W + u(0.08), u(0.2), D + u(0.08)]} />
        <meshStandardMaterial color={FRAME} roughness={0.6} metalness={0.4} />
      </mesh>
      {/* 玻璃主体 */}
      <mesh castShadow position={[0, u(0.2) + (H - u(0.2)) / 2, 0]}>
        <boxGeometry args={[W, H - u(0.2), D]} />
        <meshStandardMaterial
          color={GLASS}
          transparent
          opacity={0.35}
          roughness={0.05}
          metalness={0.3}
        />
      </mesh>
      {/* 顶棚（红/绿） */}
      <mesh castShadow position={[0, H + ROOF_T / 2, 0]}>
        <boxGeometry args={[W + u(0.08), ROOF_T, D + u(0.08)]} />
        <meshStandardMaterial color={topColor} roughness={0.5} metalness={0.3} />
      </mesh>
      {/* 边框（细线，框出玻璃盒） */}
      {[
        [+W / 2, 0, 0, 0.02, H, D],
        [-W / 2, 0, 0, 0.02, H, D],
        [0, 0, +D / 2, W, H, 0.02],
        [0, 0, -D / 2, W, H, 0.02],
      ].map(([px, , pz, pw, ph, pd], i) => (
        <mesh key={`frame-${i}`} position={[px as number, (ph as number) / 2 + u(0.2), pz as number]}>
          <boxGeometry args={[pw as number, ph as number, pd as number]} />
          <meshStandardMaterial color={FRAME} metalness={0.5} roughness={0.4} />
        </mesh>
      ))}
    </group>
  );
}

export default PhoneBooth;