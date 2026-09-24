/**
 * TrashCan — 垃圾箱（15-3D城市全面真实感深化 · 阶段 M）：
 *
 * 圆桶 + 顶盖 + 分类色（蓝色可回收 / 灰色其他 / 红色有害）。
 * 米制统一经 cityScale.u()。
 *
 * 契约：lag_docs/虚拟城市/已实现/15-3D城市渲染深化/02-架构设计 §4.1。
 */

import { u } from '../cityScale';

const BIN_COLORS = ['#3a78c8', '#8a8d96', '#c8453a']; // 蓝 / 灰 / 红（3 分类）

interface Props {
  x: number;
  z: number;
  rotation?: number;
  /** 分类（0=蓝/可回收, 1=灰/其他, 2=红/有害）；缺省按 district id 散列。 */
  variant?: 0 | 1 | 2;
}

export function TrashCan({ x, z, rotation = 0, variant = 0 }: Props) {
  const R = u(0.18);
  const H = u(0.55);
  const CAP_T = u(0.05);
  const color = BIN_COLORS[variant];

  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* 桶身 */}
      <mesh castShadow position={[0, H / 2, 0]}>
        <cylinderGeometry args={[R, R * 0.92, H, 12]} />
        <meshStandardMaterial color={color} roughness={0.6} metalness={0.2} />
      </mesh>
      {/* 顶盖（略外扩，色深一档） */}
      <mesh castShadow position={[0, H + CAP_T / 2, 0]}>
        <cylinderGeometry args={[R * 1.05, R * 1.05, CAP_T, 6]} />
        <meshStandardMaterial color={color} roughness={0.5} metalness={0.3} />
      </mesh>
      {/* 投口标识：白色小条（前面） */}
      <mesh position={[0, H * 0.7, R + 0.001]}>
        <planeGeometry args={[u(0.15), u(0.04)]} />
        <meshStandardMaterial color="#ffffff" roughness={0.6} />
      </mesh>
    </group>
  );
}

export default TrashCan;