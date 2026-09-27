/**
 * 运河生活（18-AA · §5.2 CanalExtras）
 *   2 艘小艇（船体 + 篷，慢速巡航）+ 系船柱 6 只 + 两岸护栏。
 *   船位 (-9.5, 17) / (16.0, 17) ——避开跨河桥 x=±6.2。
 *
 * 批次 28 二轮（DC 攻坚）：40 mesh → 4 mesh（几何全等合并，engine3d/geoMerge）：
 *   1/2) 小艇 ×2：每艘（船体 + 船头锥 + 船篷）3 件合 1 mesh（顶点色）——
 *      useFrame 漂移组保留独立 mesh，子装配整体随组移动。
 *   3) 系船柱 mesh：两岸 12 只圆柱合并。
 *   4) 护栏 mesh：两岸矮柱合并（材质原值统一，零取舍）。
 * 批次 28 二轮取舍：小艇件原粗糙度即统一 0.7（零取舍）；caster 裁剪——
 *   运河设施不投影（shadow pass 实测 1044 DC 超 500 阈值，只留楼体+树干）。
 */
import React, { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { cylPart, mergeParts, type MergePart } from '@/engine3d';
import { u } from '../cityScale';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const HULL = '#a87055';
const CANOPY = '#c0392b';
const POST = '#3a3f4a';
const RAIL_GREY = '#7a8290';

export function CanalExtras() {
  const bollards = useMemo(() => {
    const out: Array<[number, number]> = [];
    for (let i = -3; i <= 3; i++) {
      if (i === 0) continue;
      out.push([i * 4.5, 0]);
    }
    return out;
  }, []);

  // 系船柱 6 只 ×2 岸（12 件合 1；沿 z=17±1.6 两岸，x=±4.5 整数倍避开桥 ±6.2）
  const bollardGeo = useMemo(() => {
    const parts: MergePart[] = [];
    for (const [bx] of bollards) {
      parts.push(cylPart(u(0.15), u(0.18), u(0.7), 8, bx, u(0.35), 17 + 1.6, POST));
      parts.push(cylPart(u(0.15), u(0.18), u(0.7), 8, bx, u(0.35), 17 - 1.6, POST));
    }
    return mergeParts(parts);
  }, [bollards]);
  useEffect(() => () => bollardGeo.dispose(), [bollardGeo]);

  // 两岸护栏矮柱（每岸 11 根，避开桥 ±6.2）
  const railGeo = useMemo(() => {
    const parts: MergePart[] = [];
    for (const bx of [-12, -8, -4, 0, 4, 8, 12, 16, 20, 24, 28]) {
      if (Math.abs(bx) > 5 && Math.abs(bx) < 7) continue;
      parts.push(cylPart(u(0.04), u(0.04), u(1.2), 6, bx, u(0.6), 17 + 2.0, RAIL_GREY));
      parts.push(cylPart(u(0.04), u(0.04), u(1.2), 6, bx, u(0.6), 17 - 2.0, RAIL_GREY));
    }
    return mergeParts(parts);
  }, []);
  useEffect(() => () => railGeo.dispose(), [railGeo]);

  // 批次 28 B2：运河配套信息交互（系船柱/护栏/游船事件冒泡至根组）。
  const info = useObjectInfoProps('civic.canal-extras', { anchorY: 0.8 });

  return (
    <group {...info}>
      {/* 船 1：起点 (-9.5, 17)，沿 +x 漂移到 +18，再折返 */}
      <Boat start={-9.5} range={18} direction={1} />
      {/* 船 2：起点 (16.0, 17)，沿 -x 漂移到 -16，再折返 */}
      <Boat start={16.0} range={-16} direction={-1} />
      {/* 系船柱 12 只合 1（caster 裁剪不投影） */}
      <mesh geometry={bollardGeo}>
        <meshStandardMaterial vertexColors metalness={0.5} roughness={0.5} />
      </mesh>
      {/* 两岸护栏矮柱合 1（材质原值：metal 0.5 / 默认 roughness 1.0） */}
      <mesh geometry={railGeo}>
        <meshStandardMaterial vertexColors metalness={0.5} roughness={1.0} />
      </mesh>
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
  // 船体 3 件合 1 mesh（船体 box + 船头锥绕 Z -π/8 + 船篷；粗糙度原统一 0.7）
  const hullGeo = useMemo(
    () =>
      mergeParts([
        // 船体：半柱壳（用 box 简化）
        { geo: new THREE.BoxGeometry(u(2.5), u(0.4), u(0.9)), x: 0, y: 0, z: 0, color: HULL },
        // 船头尖（绕 Z 倾斜 -π/8）
        { geo: new THREE.ConeGeometry(u(0.5), u(1.2), 3), rotZ: -Math.PI / 8, x: u(1.4), y: u(0.1), z: 0, color: HULL },
        // 船篷
        { geo: new THREE.BoxGeometry(u(1.4), u(0.5), u(0.85)), x: -u(0.3), y: u(0.7), z: 0, color: CANOPY },
      ]),
    [],
  );
  useEffect(() => () => hullGeo.dispose(), [hullGeo]);
  return (
    <group ref={ref} position={[start, u(0.15), 17]}>
      {/* 小艇（顶点色；caster 裁剪不投影；组随 useFrame 漂移） */}
      <mesh geometry={hullGeo}>
        <meshStandardMaterial vertexColors roughness={0.7} />
      </mesh>
    </group>
  );
}
