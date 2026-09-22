/**
 * StreetFurniture — 街道小件家具（14-3D城市渲染深化 · 阶段 I）：
 *
 *   bench   长椅：座板 + 靠背（木色，城区树池旁）
 *   hydrant 消防栓：圆柱 + 顶盖（红）
 *
 * 纯几何；米制经 cityScale.u()（契约 02 §3.3）。
 */

import { u } from '../cityScale';

const WOOD = '#6b4f3a';
const WOOD_DARK = '#5a4230';
const HYDRANT_RED = '#c0392b';
const HYDRANT_CAP = '#992d22';

interface Props {
  x: number;
  z: number;
  variant: 'bench' | 'hydrant';
  rotation?: number;
}

/** 长椅：座板 u(1.5)×u(0.45)×u(0.1) @h u(0.45) + 靠背 u(1.5)×u(0.4)×u(0.06)。 */
function Bench() {
  return (
    <group>
      {/* 座板 */}
      <mesh castShadow position={[0, u(0.45), 0]}>
        <boxGeometry args={[u(1.5), u(0.1), u(0.45)]} />
        <meshStandardMaterial color={WOOD} roughness={0.85} />
      </mesh>
      {/* 靠背 */}
      <mesh castShadow position={[0, u(0.7), -u(0.18)]}>
        <boxGeometry args={[u(1.5), u(0.4), u(0.06)]} />
        <meshStandardMaterial color={WOOD_DARK} roughness={0.85} />
      </mesh>
      {/* 双腿 */}
      {[-1, 1].map((side) => (
        <mesh key={`leg-${side}`} position={[side * u(0.6), u(0.22), 0]}>
          <boxGeometry args={[u(0.08), u(0.45), u(0.4)]} />
          <meshStandardMaterial color={WOOD_DARK} roughness={0.9} />
        </mesh>
      ))}
    </group>
  );
}

/** 消防栓：柱 r=u(0.12) h=u(0.55) + 顶盖。 */
function Hydrant() {
  return (
    <group>
      <mesh castShadow position={[0, u(0.275), 0]}>
        <cylinderGeometry args={[u(0.12), u(0.14), u(0.55), 10]} />
        <meshStandardMaterial color={HYDRANT_RED} roughness={0.5} metalness={0.3} />
      </mesh>
      <mesh position={[0, u(0.57), 0]}>
        <boxGeometry args={[u(0.14), u(0.08), u(0.14)]} />
        <meshStandardMaterial color={HYDRANT_CAP} roughness={0.5} metalness={0.3} />
      </mesh>
    </group>
  );
}

export function StreetFurniture({ x, z, variant, rotation = 0 }: Props) {
  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {variant === 'bench' ? <Bench /> : <Hydrant />}
    </group>
  );
}
