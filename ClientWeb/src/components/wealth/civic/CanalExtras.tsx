/**
 * 运河生活（18-AA · §5.2 CanalExtras）
 *   2 艘小艇（船体 + 篷，慢速巡航）+ 系船柱 6 只 + 两岸护栏。
 *   船位 (-9.5, 17) / (16.0, 17) ——避开跨河桥 x=±6.2。
 */
import React, { useMemo } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { u } from '../cityScale';

const HULL = '#a87055';
const CANOPY = '#c0392b';
const POST = '#3a3f4a';

export function CanalExtras() {
  const bollards = useMemo(() => {
    const out: Array<[number, number]> = [];
    for (let i = -3; i <= 3; i++) {
      if (i === 0) continue;
      out.push([i * 4.5, 0]);
    }
    return out;
  }, []);

  return (
    <group>
      {/* 船 1：起点 (-9.5, 17)，沿 +x 漂移到 +18，再折返 */}
      <Boat start={-9.5} range={18} direction={1} />
      {/* 船 2：起点 (16.0, 17)，沿 -x 漂移到 -16，再折返 */}
      <Boat start={16.0} range={-16} direction={-1} />
      {/* 系船柱 6 只：沿 z=17±1.6 两岸，x=±4.5 整数倍（避开桥 ±6.2） */}
      {bollards.map(([bx, _bz], i) => {
        const zSide = 17 + 1.6;
        const zSide2 = 17 - 1.6;
        return (
          <group key={`bollard-${i}`}>
            <mesh position={[bx, u(0.35), zSide]} castShadow>
              <cylinderGeometry args={[u(0.15), u(0.18), u(0.7), 8]} />
              <meshStandardMaterial color={POST} metalness={0.5} roughness={0.5} />
            </mesh>
            <mesh position={[bx, u(0.35), zSide2]} castShadow>
              <cylinderGeometry args={[u(0.15), u(0.18), u(0.7), 8]} />
              <meshStandardMaterial color={POST} metalness={0.5} roughness={0.5} />
            </mesh>
          </group>
        );
      })}
      {/* 两岸护栏（每岸矮柱，避开桥 ±6.2） */}
      {[-12, -8, -4, 0, 4, 8, 12, 16, 20, 24, 28]
        .filter((bx) => !(Math.abs(bx) > 5 && Math.abs(bx) < 7))
        .map((bx, i) => (
          <group key={`rail-${i}`}>
            <mesh position={[bx, u(0.6), 17 + 2.0]}>
              <cylinderGeometry args={[u(0.04), u(0.04), u(1.2), 6]} />
              <meshStandardMaterial color="#7a8290" metalness={0.5} />
            </mesh>
            <mesh position={[bx, u(0.6), 17 - 2.0]}>
              <cylinderGeometry args={[u(0.04), u(0.04), u(1.2), 6]} />
              <meshStandardMaterial color="#7a8290" metalness={0.5} />
            </mesh>
          </group>
        ))}
    </group>
  );
}

function Boat({ start, range, direction }: { start: number; range: number; direction: 1 | -1 }) {
  // 沿 x 在 [min(start, range), max(start, range)] 之间往复，1.5 单位/秒
  const ref = React.useRef<THREE.Group>(null);
  useFrame((_, delta) => {
    if (!ref.current) return;
    const lo = Math.min(start, range);
    const hi = Math.max(start, range);
    let x = ref.current.position.x + delta * 1.5 * direction;
    if (x > hi) {
      x = hi;
      ref.current.userData.dir = -direction;
    } else if (x < lo) {
      x = lo;
      ref.current.userData.dir = -direction;
    }
    ref.current.position.x = x;
    // 朝向根据方向
    ref.current.rotation.y = direction > 0 ? 0 : Math.PI;
  });
  return (
    <group ref={ref} position={[start, u(0.15), 17]}>
      {/* 船体：半柱壳（用 box 简化） */}
      <mesh castShadow>
        <boxGeometry args={[u(2.5), u(0.4), u(0.9)]} />
        <meshStandardMaterial color={HULL} roughness={0.7} />
      </mesh>
      {/* 船头尖 */}
      <mesh position={[u(1.4), u(0.1), 0]} rotation={[0, 0, -Math.PI / 8]} castShadow>
        <coneGeometry args={[u(0.5), u(1.2), 3]} />
        <meshStandardMaterial color={HULL} roughness={0.7} />
      </mesh>
      {/* 船篷 */}
      <mesh position={[-u(0.3), u(0.7), 0]} castShadow>
        <boxGeometry args={[u(1.4), u(0.5), u(0.85)]} />
        <meshStandardMaterial color={CANOPY} roughness={0.7} />
      </mesh>
    </group>
  );
}