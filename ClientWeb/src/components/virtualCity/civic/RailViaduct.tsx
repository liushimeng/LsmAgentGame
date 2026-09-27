/**
 * 北环轻轨（18-AA · §5.2 RailViaduct）
 *   沿 z=+46 横向架设，箱梁 + 13 桥墩 + 2 车站（西站/北站）+ 停靠列车 3 节。
 *   批次 20（文档 1 §3.4）：z=30 → 46 北迁 —— 32 区后 wetland(-12,40)/
 *   sports(10,40)/university(-32,34)/fin_sub(24,30)/bay(40,28) 底板半径 4
 *   与旧 z=30 走廊交叠；z=46 位于 fin_sub/sports 北侧、|z|≤48 底板带内
 *   且 WORLD_SIZE=120 边缘留 ≥14 单位缓冲；进出站方向不变。
 *   布点 x∈[-30,30]（已预检：z=46 不与任何城区底板相交）。
 *
 * 批次 28 二轮（DC 攻坚）：62 mesh → 5 mesh（几何全等合并，engine3d/geoMerge）：
 *   1) 箱梁+桥墩 mesh（多 group）：梁 #7a8290 / 墩 #8a8f98 两档 concrete PBR 分桶。
 *   2) 栏杆 mesh（顶点色）：两侧 24 根立柱合并。
 *   3) 车站静件 mesh（顶点色）：2 站 ×（月台 + 雨棚 + 4 支柱 + 站牌柱）共 14 件。
 *   4) 站牌 mesh（多 group）：2 面自发光牌 + 2 块 basic 站名底板分桶。
 *   5) 列车 mesh（多 group）：3 节车体 + 3 条半透明窗带分桶（透明组仍走
 *      three 透明队列，与原独立 mesh 渲染次序语义一致）。
 * 批次 28 二轮取舍：车站静件粗糙度/金属度统一 0.55/0.45（原 0.5–0.6 / 0.3–0.5）；
 *   caster 裁剪——轻轨设施不投影（shadow pass 实测 1044 DC 超 500 阈值，
 *   只留楼体+树干），receiveShadow 保留（桥面/站台仍接收楼体投影）。
 */
import { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { mergeGrouped, mergeParts, type GroupedMergePart, type MergePart } from '@/engine3d';
import { useCivicPBR } from './CivicPBR';
import { u } from '../cityScale';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const STATION_SILVER = '#c0c5cd';
const TRAIN_BODY = '#b8c5d5';
const TRAIN_DARK = '#3a4250';
const GIRDER_GREY = '#7a8290';
const PIER_GREY = '#8a8f98';
const CANOPY_GREY = '#5a6270';
const SIGN_GOLD = '#d4a017';
const SIGN_TEXT_DARK = '#161d28';

/** 车站静件统一材质参数（批次 28 二轮视觉取舍：原 roughness 0.5–0.6 / metalness 0.3–0.5 取中）。 */
const STATION_ROUGH = 0.55;
const STATION_METAL = 0.45;

/** 欧拉旋转 + 平移的部件矩阵（等价原 JSX 的 rotation + position 组合）。 */
function partMatrix(x: number, y: number, z: number, rx: number, ry: number, rz: number): THREE.Matrix4 {
  return new THREE.Matrix4().compose(
    new THREE.Vector3(x, y, z),
    new THREE.Quaternion().setFromEuler(new THREE.Euler(rx, ry, rz)),
    new THREE.Vector3(1, 1, 1),
  );
}

export function RailViaduct() {
  // 批次 28 B2：轻轨高架信息交互（根组承接轨面/桥墩/站台事件冒泡）。
  const info = useObjectInfoProps('civic.rail-viaduct', { anchorY: 2 });
  const concrete = useCivicPBR('concrete', [0.8, 0.8]);
  const c = concrete.matProps;

  // 1) 箱梁 + 13 桥墩（多 group 分桶；桥墩每 5 世界单位一根）
  const viaductGeo = useMemo(() => {
    const parts: GroupedMergePart[] = [
      // 箱梁：60m × 0.16m × 4.5m @ y=u(9)
      { geo: new THREE.BoxGeometry(60, u(1.6), u(4.5)), x: 0, y: u(9), z: 46, mat: 0 },
    ];
    for (let i = 0; i < 13; i++) {
      parts.push({ geo: new THREE.BoxGeometry(u(1.8), u(9), u(1.8)), x: -30 + i * 5, y: u(4.5), z: 46, mat: 1 });
    }
    return mergeGrouped(parts);
  }, []);
  useEffect(() => () => viaductGeo.dispose(), [viaductGeo]);

  // 2) 栏杆：箱梁两侧每 5m 一根，12×2 = 24 根（顶点色合并）
  const railGeo = useMemo(() => {
    const parts: MergePart[] = [];
    for (const side of [0, 1]) {
      for (const x of [-28, -23, -18, -13, -8, -3, 2, 7, 12, 17, 22, 27]) {
        parts.push({
          geo: new THREE.CylinderGeometry(u(0.04), u(0.04), u(1.2), 6),
          x, y: u(10.6), z: 46 + side * u(2.3),
          color: GIRDER_GREY,
        });
      }
    }
    return mergeParts(parts);
  }, []);
  useEffect(() => () => railGeo.dispose(), [railGeo]);

  // 3) 车站静件：2 座（西站 -18 / 北站 0）×（月台 + 雨棚 + 4 支柱 + 站牌柱）
  const stationGeo = useMemo(() => {
    const parts: MergePart[] = [];
    for (const sx of [-18, 0]) {
      parts.push(boxStationPart(sx, 0, u(10), u(6), STATION_SILVER));           // 月台
      parts.push(boxStationPart(sx, u(13), 0, u(8), CANOPY_GREY));              // 雨棚
      for (const dx of [-u(35), -u(10), u(10), u(35)]) {
        parts.push({ geo: new THREE.CylinderGeometry(u(0.15), u(0.15), u(3), 6), x: sx + dx, y: u(11.5), z: 46 + u(2), color: GIRDER_GREY });
      }
      parts.push({ geo: new THREE.CylinderGeometry(u(0.08), u(0.08), u(3), 6), x: sx - u(45), y: u(11.5), z: 46 + u(2), color: CANOPY_GREY });
    }
    return mergeParts(parts);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useEffect(() => () => stationGeo.dispose(), [stationGeo]);

  // 4) 站牌：2 面自发光牌（mat 0）+ 2 块 basic 站名底板（mat 1）
  const signGeo = useMemo(() => {
    const parts: GroupedMergePart[] = [];
    for (const sx of [-18, 0]) {
      parts.push({ geo: new THREE.BoxGeometry(u(4), u(1.4), u(0.1)), x: sx - u(45), y: u(13), z: 46 + u(2.2), mat: 0 });
      parts.push({ geo: new THREE.BoxGeometry(u(3.8), u(1.2), u(0.02)), x: sx - u(45), y: u(13), z: 46 + u(2.3), mat: 1 });
    }
    return mergeGrouped(parts);
  }, []);
  useEffect(() => () => signGeo.dispose(), [signGeo]);

  // 5) 列车 3 节（mat 0 车体 / mat 1 半透明窗带；车体原绕 Y 旋转 π，box 全等保留矩阵）
  const trainGeo = useMemo(() => {
    const parts: GroupedMergePart[] = [];
    for (const i of [-1, 0, 1]) {
      const x = 4 + i * u(23);
      parts.push({ geo: new THREE.BoxGeometry(u(22), u(3.2), u(3.2)), matrix: partMatrix(x, u(11.5), 46, 0, Math.PI, 0), mat: 0 });
      parts.push({ geo: new THREE.BoxGeometry(u(20), u(1.0), u(3.4)), x, y: u(12), z: 46, mat: 1 });
    }
    return mergeGrouped(parts);
  }, []);
  useEffect(() => () => trainGeo.dispose(), [trainGeo]);

  // 材质表：高架 2 档（concrete PBR 逐字段保留）/ 站牌 2 档 / 列车 2 档
  const viaductMaterials = useMemo(
    () => [
      new THREE.MeshStandardMaterial({ color: GIRDER_GREY, ...c, roughness: c.roughnessMap ? undefined : 0.85 }),
      new THREE.MeshStandardMaterial({ color: PIER_GREY, ...c, roughness: c.roughnessMap ? undefined : 0.85 }),
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [c],
  );
  useEffect(() => () => viaductMaterials.forEach((m) => m.dispose()), [viaductMaterials]);

  const signMaterials = useMemo(
    () => [
      new THREE.MeshStandardMaterial({ color: SIGN_GOLD, emissive: SIGN_GOLD, emissiveIntensity: 0.4 }),
      new THREE.MeshBasicMaterial({ color: SIGN_TEXT_DARK }),
    ],
    [],
  );
  useEffect(() => () => signMaterials.forEach((m) => m.dispose()), [signMaterials]);

  const trainMaterials = useMemo(
    () => [
      new THREE.MeshStandardMaterial({ color: TRAIN_BODY, metalness: 0.6, roughness: 0.3 }),
      new THREE.MeshStandardMaterial({ color: TRAIN_DARK, transparent: true, opacity: 0.65, roughness: 0.1, metalness: 0.4 }),
    ],
    [],
  );
  useEffect(() => () => trainMaterials.forEach((m) => m.dispose()), [trainMaterials]);

  return (
    <group {...info}>
      {/* 箱梁 + 桥墩（concrete PBR 2 档；caster 裁剪不投影） */}
      <mesh geometry={viaductGeo} material={viaductMaterials} receiveShadow />
      {/* 栏杆 24 根 */}
      <mesh geometry={railGeo}>
        <meshStandardMaterial vertexColors metalness={0.4} roughness={0.5} />
      </mesh>
      {/* 车站静件（2 站 14 件合 1；caster 裁剪不投影） */}
      <mesh geometry={stationGeo} receiveShadow>
        <meshStandardMaterial vertexColors roughness={STATION_ROUGH} metalness={STATION_METAL} />
      </mesh>
      {/* 站牌（自发光 + basic 站名底板） */}
      <mesh geometry={signGeo} material={signMaterials} />
      {/* 列车 3 节（车体 + 半透明窗带；caster 裁剪不投影） */}
      <mesh geometry={trainGeo} material={trainMaterials} />
    </group>
  );
}

/** 车站水平 box 部件（月台/雨棚，@ z=46 走廊，y 与深度由调用方给出）。 */
function boxStationPart(sx: number, y: number, dz: number, d: number, color: string): MergePart {
  const w = u(80);
  return { geo: new THREE.BoxGeometry(w, u(0.5), d), x: sx, y, z: 46 + dz, color };
}
