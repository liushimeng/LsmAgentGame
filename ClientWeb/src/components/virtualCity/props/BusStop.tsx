/**
 * BusStop — 几何公交站台（14-3D城市渲染深化 · 阶段 I）：
 *
 * 双柱 + 顶棚 + 发光灯箱（纯几何，无贴图依赖）。
 * 米制全部经 cityScale.u() 换算（契约 02 §3.2）：
 *   柱 r=u(0.08) h=u(2.8)；顶棚 u(2.4)×u(0.12)×u(1.0)；灯箱 u(1.6)×u(1.0)×u(0.08)。
 */

import { u } from '../cityScale';

const POLE_R = u(0.08);
const POLE_H = u(2.8);
const SHELTER_W = u(2.4);
const SHELTER_T = u(0.12);
const SHELTER_D = u(1.0);
const LIGHTBOX_W = u(1.6);
const LIGHTBOX_H = u(1.0);
const LIGHTBOX_T = u(0.08);
const POLE_COLOR = '#5a6270';
const ROOF_COLOR = '#3a414c';
/** 灯箱暖光（ACES 下 0.5 不过曝）。 */
const LIGHTBOX_EMISSIVE = '#ffe9b8';

interface Props {
  x: number;
  z: number;
  rotation?: number;
}

export function BusStop({ x, z, rotation = 0 }: Props) {
  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* 双柱 */}
      {[-1, 1].map((side) => (
        <mesh
          key={`pole-${side}`}
          castShadow
          position={[side * SHELTER_W * 0.42, POLE_H / 2, -SHELTER_D * 0.35]}
        >
          <cylinderGeometry args={[POLE_R, POLE_R, POLE_H, 8]} />
          <meshStandardMaterial color={POLE_COLOR} roughness={0.6} metalness={0.4} />
        </mesh>
      ))}
      {/* 顶棚 */}
      <mesh castShadow position={[0, POLE_H + SHELTER_T / 2, 0]}>
        <boxGeometry args={[SHELTER_W, SHELTER_T, SHELTER_D]} />
        <meshStandardMaterial color={ROOF_COLOR} roughness={0.8} metalness={0.2} />
      </mesh>
      {/* 发光灯箱（靠柱侧竖板） */}
      <mesh position={[0, LIGHTBOX_H / 2 + u(0.3), -SHELTER_D * 0.35]}>
        <boxGeometry args={[LIGHTBOX_W, LIGHTBOX_H, LIGHTBOX_T]} />
        <meshStandardMaterial
          color={LIGHTBOX_EMISSIVE}
          emissive={LIGHTBOX_EMISSIVE}
          emissiveIntensity={0.5}
          roughness={0.4}
          metalness={0.1}
        />
      </mesh>
    </group>
  );
}
