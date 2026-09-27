/**
 * ParkGrounds — 中央公园园路 + 花坛（16-3D城市WebGL质感与城市补全 · 阶段 T）：
 *
 * 十字园路（plaza_tile，压草地 y=0.027）+ 4 象限花坛（深土圈 + 3 色花球，
 * 确定性取色）。公园中心 = districtCenter('central_park')。
 *
 * 批次 28 二轮（DC 攻坚）：18 mesh → 2 mesh（engine3d/geoMerge，几何全等）：
 *   1) 十字园路 2 条 plane 合并单 mesh（同一 plaza_tile 材质，两条路的
 *      躺平 + 交叉旋转经矩阵逐位保留，UV 不动）；
 *   2) 4 花坛（深土圈 ×4 + 花球 ×12）顶点色合并单 mesh。
 * 批次 28 二轮取舍：土圈 0.95 与花球 0.7 粗糙度统一为 0.85、金属度恒 0。
 * 批次 28 二轮取舍：caster 裁剪——地标不投影（shadow pass 实测 1044 DC 超阈，
 * 全地标 castShadow=false；原花球的投影随批取消）。
 *
 * 契约：lag_docs/虚拟城市/已实现/16-3D城市WebGL质感与城市补全/02-架构设计 §8.2。
 */

import { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { districtCenter } from '@/types/virtualCity';
import { groundTileUrl } from '@/assets/images/virtualCity';
import { u } from '../cityScale';
import { type MergePart, mergeParts, useSharedTexture } from '@/engine3d';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const FLOWER_COLORS = ['#c8453a', '#d8c44a', '#7c3aed', '#e07a3f'];
const SOIL = '#4a3f30';

/** 花坛合并统一粗糙度（批次 28 二轮取舍：土圈 0.95 / 花球 0.7 取中）。 */
const BED_ROUGH = 0.85;

/** 4 花坛象限偏移（园路内侧，避开花路 0.5 半宽 + 喷泉 r=2.3）。 */
const BED_OFFSETS: Array<[number, number]> = [
  [1.7, 1.7],
  [-1.7, 1.7],
  [1.7, -1.7],
  [-1.7, -1.7],
];

/** 花球品字布点（原 JSX 内联数组逐位搬移）。 */
const FLOWER_SPOTS: Array<[number, number, number]> = [
  [0, u(0.16), 0],
  [u(0.16), u(0.14), u(0.1)],
  [-u(0.14), u(0.15), -u(0.08)],
];

/** 平躺 plane/circle 的 TRS 矩阵（等价 rotation=[-π/2,0,rotZ]）。 */
function flatMatrix(x: number, y: number, z: number, rotZ = 0): THREE.Matrix4 {
  return new THREE.Matrix4().compose(
    new THREE.Vector3(x, y, z),
    new THREE.Quaternion().setFromEuler(new THREE.Euler(-Math.PI / 2, 0, rotZ)),
    new THREE.Vector3(1, 1, 1),
  );
}

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

  // 十字园路 2 条 plane → 单 mesh（同贴图材质；交叉角 rotZ 经矩阵保留）
  const pathGeo = useMemo(
    () =>
      mergeParts(
        [0, Math.PI / 2].map((rot) => ({
          geo: new THREE.PlaneGeometry(0.5, 7.4),
          matrix: flatMatrix(0, 0.027, 0, rot),
        })),
      ),
    [],
  );
  useEffect(() => () => pathGeo.dispose(), [pathGeo]);

  // 花坛 ×4（深土圈 + 花球 ×3）→ 单顶点色 mesh（象限偏移烘焙进部件坐标）
  const bedGeo = useMemo(() => {
    const parts: MergePart[] = [];
    for (const bed of beds) {
      // 深土圈（躺平圆盘）
      parts.push({
        geo: new THREE.CircleGeometry(u(0.42), 12),
        matrix: flatMatrix(bed.ox, 0.029, bed.oz),
        color: SOIL,
      });
      // 花球 ×3（品字布点）
      for (let j = 0; j < FLOWER_SPOTS.length; j++) {
        const [px, py, pz] = FLOWER_SPOTS[j];
        parts.push({
          geo: new THREE.SphereGeometry(u(0.12), 8, 6),
          x: bed.ox + px,
          y: py,
          z: bed.oz + pz,
          color: bed.colors[j],
        });
      }
    }
    return mergeParts(parts);
  }, [beds]);
  useEffect(() => () => bedGeo.dispose(), [bedGeo]);

  const info = useObjectInfoProps('landmark.park-grounds', { anchorY: 0.5 });
  return (
    <group {...info} position={[park.x, 0, park.z]}>
      {/* 十字园路（合并 mesh；贴图材质与原一致） */}
      <mesh geometry={pathGeo} receiveShadow>
        <meshStandardMaterial
          map={plaza ?? undefined}
          color={plaza ? '#ffffff' : '#9aa1ab'}
          roughness={0.9}
        />
      </mesh>
      {/* 花坛 ×4（顶点色合并；批次 28 二轮 caster 裁剪 → 不投影） */}
      <mesh geometry={bedGeo}>
        <meshStandardMaterial vertexColors roughness={BED_ROUGH} metalness={0} />
      </mesh>
    </group>
  );
}

export default ParkGrounds;
