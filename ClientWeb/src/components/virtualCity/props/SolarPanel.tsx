/**
 * SolarPanel — 屋顶太阳能板（16-3D城市WebGL质感与城市补全 · 阶段 T）：
 *
 * 倾斜 20° 深蓝板 + 白色分格线 + 2 支架。布点由 StreetPropsLayer 在
 * suburb / oldtown 屋顶确定性生成（每区 1-2 块）。
 *
 * 契约：lag_docs/虚拟城市/已实现/16-3D城市WebGL质感与城市补全/02-架构设计 §8.5。
 */

import { u } from '../cityScale';

const PANEL_BLUE = '#1a3a6e';
const FRAME = '#5a6270';

interface Props {
  x: number;
  /** 屋顶面 y（由调用方按 roofY 传入）。 */
  y: number;
  z: number;
  rotation?: number;
}

export function SolarPanel({ x, y, z, rotation = 0 }: Props) {
  const tilt = -Math.PI / 9; // 20°
  return (
    <group position={[x, y, z]} rotation={[0, rotation, 0]}>
      {/* 板体（绕 x 倾斜；box 中心抬高避免插进屋顶） */}
      <mesh position={[0, u(0.18), 0]} rotation={[tilt, 0, 0]} castShadow>
        <boxGeometry args={[u(1.0), 0.012, u(0.6)]} />
        <meshStandardMaterial color={PANEL_BLUE} roughness={0.25} metalness={0.6} envMapIntensity={1.1} />
      </mesh>
      {/* 分格白线（2 条横线，随板倾斜） */}
      {[-0.15, 0.15].map((dz, i) => (
        <mesh key={`grid-${i}`} position={[0, u(0.19), dz]} rotation={[tilt, 0, 0]}>
          <boxGeometry args={[u(0.96), 0.014, 0.006]} />
          <meshStandardMaterial color="#dfe5ec" roughness={0.5} />
        </mesh>
      ))}
      {/* 支架 2 个（小方柱） */}
      {[-0.18, 0.18].map((dz, i) => (
        <mesh key={`leg-${i}`} position={[0, u(0.06), dz]}>
          <boxGeometry args={[0.02, u(0.12), 0.02]} />
          <meshStandardMaterial color={FRAME} roughness={0.6} metalness={0.5} />
        </mesh>
      ))}
    </group>
  );
}

export default SolarPanel;
