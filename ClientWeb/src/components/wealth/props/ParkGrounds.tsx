/**
 * ParkGrounds — 中央公园园路 + 花坛（16-3D城市WebGL质感与城市补全 · 阶段 T）：
 *
 * 十字园路（plaza_tile，压草地 y=0.027）+ 4 象限花坛（深土圈 + 3 色花球，
 * 确定性取色）。公园中心 = districtCenter('central_park')。
 *
 * 契约：lag_docs/虚拟城市/已实现/16-3D城市WebGL质感与城市补全/02-架构设计 §8.2。
 */

import { useMemo } from 'react';
import { districtCenter } from '@/types/wealth';
import { groundTileUrl } from '@/assets/images/wealth';
import { u } from '../cityScale';
import { useSharedTexture } from '../textureCache';

const FLOWER_COLORS = ['#c8453a', '#d8c44a', '#7c3aed', '#e07a3f'];
const SOIL = '#4a3f30';

/** 4 花坛象限偏移（园路内侧，避开花路 0.5 半宽 + 喷泉 r=2.3）。 */
const BED_OFFSETS: Array<[number, number]> = [
  [1.7, 1.7],
  [-1.7, 1.7],
  [1.7, -1.7],
  [-1.7, -1.7],
];

export function ParkGrounds() {
  const park = districtCenter('central_park');
  const plaza = useSharedTexture(groundTileUrl('plaza_tile'), {
    wrap: 'repeat',
    repeat: [1, 8],
  });

  // 花坛花球配色（确定性：象限序号取色，不引随机源）
  const beds = useMemo(
    () =>
      BED_OFFSETS.map(([ox, oz], i) => ({
        ox,
        oz,
        colors: [FLOWER_COLORS[i % 4], FLOWER_COLORS[(i + 1) % 4], FLOWER_COLORS[(i + 2) % 4]],
      })),
    [],
  );

  return (
    <group position={[park.x, 0, park.z]}>
      {/* 十字园路（两条交叉 plane，宽 0.5 长 7.4） */}
      {[0, Math.PI / 2].map((rot, i) => (
        <mesh
          key={`path-${i}`}
          rotation={[-Math.PI / 2, 0, rot]}
          position={[0, 0.027, 0]}
          receiveShadow
        >
          <planeGeometry args={[0.5, 7.4]} />
          <meshStandardMaterial
            map={plaza ?? undefined}
            color={plaza ? '#ffffff' : '#9aa1ab'}
            roughness={0.9}
          />
        </mesh>
      ))}
      {/* 花坛 ×4 */}
      {beds.map((bed, i) => (
        <group key={`bed-${i}`} position={[bed.ox, 0, bed.oz]}>
          {/* 深土圈 */}
          <mesh position={[0, 0.029, 0]} rotation={[-Math.PI / 2, 0, 0]}>
            <circleGeometry args={[u(0.42), 12]} />
            <meshStandardMaterial color={SOIL} roughness={0.95} />
          </mesh>
          {/* 花球 ×3（品字布点） */}
          {[
            [0, u(0.16), 0],
            [u(0.16), u(0.14), u(0.1)],
            [-u(0.14), u(0.15), -u(0.08)],
          ].map((p, j) => (
            <mesh key={`flower-${j}`} position={p as [number, number, number]} castShadow>
              <sphereGeometry args={[u(0.12), 8, 6]} />
              <meshStandardMaterial color={bed.colors[j]} roughness={0.7} />
            </mesh>
          ))}
        </group>
      ))}
    </group>
  );
}

export default ParkGrounds;
