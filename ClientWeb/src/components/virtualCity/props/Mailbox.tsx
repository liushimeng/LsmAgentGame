/**
 * Mailbox — 邮筒（15-3D城市全面真实感深化 · 阶段 M）：
 *
 * 圆柱桶身 + 顶帽（圆顶）+ 投信口（黑色 box）+ 标志色（中国红）。
 * 米制统一经 cityScale.u()。
 *
 * 契约：lag_docs/虚拟城市/已实现/15-3D城市渲染深化/02-架构设计 §4.1。
 */

import { u } from '../cityScale';

const POST_RED = '#c8453a';
const POST_DARK = '#9c352e';
const SLOT_BLACK = '#1a1a1d';

interface Props {
  x: number;
  z: number;
  rotation?: number;
}

export function Mailbox({ x, z, rotation = 0 }: Props) {
  const R = u(0.16);
  const H = u(0.65);
  const CAP_R = R * 1.05;
  const CAP_H = u(0.06);

  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* 桶身 */}
      <mesh castShadow position={[0, H / 2, 0]}>
        <cylinderGeometry args={[R, R * 0.95, H, 12]} />
        <meshStandardMaterial color={POST_RED} roughness={0.5} metalness={0.3} />
      </mesh>
      {/* 圆顶盖 */}
      <mesh castShadow position={[0, H + CAP_H / 2, 0]}>
        <cylinderGeometry args={[CAP_R, R, CAP_H, 12]} />
        <meshStandardMaterial color={POST_DARK} roughness={0.5} metalness={0.4} />
      </mesh>
      {/* 投信口（前面，黑色小矩形） */}
      <mesh position={[0, H * 0.75, R + 0.002]}>
        <planeGeometry args={[u(0.18), u(0.08)]} />
        <meshStandardMaterial color={SLOT_BLACK} roughness={0.6} />
      </mesh>
    </group>
  );
}

export default Mailbox;