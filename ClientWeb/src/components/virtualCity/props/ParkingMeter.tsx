/**
 * ParkingMeter — 停车计费牌（15-3D城市全面真实感深化 · 阶段 M）：
 *
 * 立柱 + 表盘（box）+ 顶部指示牌（蓝底白 P，可选 sprite）。
 * 米制统一经 cityScale.u()。
 *
 * 契约：lag_docs/虚拟城市/已实现/15-3D城市渲染深化/02-架构设计 §4.1。
 */

import { signUrlExt } from '@/assets/images/virtualCity';
import { u } from '../cityScale';
import { useSharedTexture } from '@/engine3d';

const POLE = '#5a6270';
const HEAD_BLUE = '#3a78c8';
const HEAD_WHITE = '#ffffff';

interface Props {
  x: number;
  z: number;
  rotation?: number;
}

export function ParkingMeter({ x, z, rotation = 0 }: Props) {
  const POLE_R = u(0.04);
  const POLE_H = u(1.4);
  const HEAD_W = u(0.3);
  const HEAD_H = u(0.3);

  // 可选贴图 sprite（缺失跳过）
  const tex = useSharedTexture(signUrlExt('parking_sign'));

  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* 立柱 */}
      <mesh castShadow position={[0, POLE_H / 2, 0]}>
        <cylinderGeometry args={[POLE_R, POLE_R, POLE_H, 6]} />
        <meshStandardMaterial color={POLE} roughness={0.5} metalness={0.5} />
      </mesh>
      {/* 顶部表盘（蓝底盒） */}
      <mesh castShadow position={[0, POLE_H + HEAD_H / 2, 0]}>
        <boxGeometry args={[HEAD_W, HEAD_H, u(0.06)]} />
        <meshStandardMaterial color={HEAD_BLUE} roughness={0.5} metalness={0.2} />
      </mesh>
      {/* 白色 P 字（前面中央小矩形代替，保留占位） */}
      <mesh position={[0, POLE_H + HEAD_H / 2, u(0.03) + 0.001]}>
        <planeGeometry args={[HEAD_W * 0.5, HEAD_H * 0.5]} />
        <meshStandardMaterial color={HEAD_WHITE} roughness={0.4} />
      </mesh>
      {/* sprite 叠加（缺失跳过） */}
      {tex && (
        <mesh position={[0, POLE_H + HEAD_H / 2, u(0.03) + 0.003]}>
          <planeGeometry args={[HEAD_W, HEAD_H]} />
          <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
        </mesh>
      )}
    </group>
  );
}

export default ParkingMeter;