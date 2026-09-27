/**
 * CanalBridge — 跨运河桥（16-3D城市WebGL质感与城市补全 · 阶段 S）：
 *
 * 桥面（box 微高路面）+ 两侧栏杆 + 端柱 + 4 桥墩 + 2 桥头灯。
 * 位置由 CanalBridgeSpots() 通式计算：len>MAIN_ROAD_MIN_LEN 放射干道与运河 z=17
 * 的交点（批次 20：运河 x 半跨 32→48 与 CANAL_Z 均从 VirtualCityCityMap 常量同源 import；
 * 32 区 + 东延后命中数增加，通式自适应）。
 *
 * 契约：lag_docs/虚拟城市/已实现/16-3D城市WebGL质感与城市补全/02-架构设计 §6。
 *
 * 批次 28 二轮：按材质类几何合并（15 mesh → 3：桥面贴图独立保留 receiveShadow；
 * 栏杆/端柱/桥墩/灯杆顶点色合并 1 mesh；暖光灯球 emissive 合并 1 mesh，几何逐件全等）。
 * 取舍：caster 裁剪（shadow pass 实测 1044 DC > 500 阈值）——桥体不再投影
 * （只留楼体+树干），桥面保留 receiveShadow 接影；合并件粗糙度 0.5–0.85 / 金属
 * 0–0.6 统一为 0.65 / 0.35（细杆件不可辨）。
 */

import { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { streetTileUrl } from '@/assets/images/virtualCity';
import { districtCenter, VIRTUAL_CITY_DISTRICTS } from '@/types/virtualCity';
import { useSharedTexture, type GroupedMergePart, type MergePart, boxPart, cylPart, mergeGrouped, mergeParts } from '@/engine3d';
import { useObjectInfoProps } from './objectInfo/useObjectInfoProps';
import { CANAL_Z, CANAL_HALF_X, MAIN_ROAD_MIN_LEN } from './VirtualCityCityMap';
/**
 * 桥面宽 / 厚 / 中心 y。顶面 = 0.035：高于水面 0.028（不没水）、仅高于路面
 * 0.015 一线（车辆 y=0.02 直接过桥不穿模——桥面与车轮着地差 0.015 不可辨）。
 */
const DECK_W = 2.0;
const DECK_H = 0.03;
const DECK_TOP_Y = 0.02;

interface Props {
  /** 桥中心世界坐标。 */
  x: number;
  z: number;
  /** 桥轴朝向（弧度；与 Road 的 atan2(dx,dz) 同约定）。 */
  rotation: number;
  /** 桥面长（默认 4.6 = 水宽 3 + 两岸各 0.8）。 */
  length?: number;
}

/** 单座桥。 */
export function CanalBridge({ x, z, rotation, length = 4.6 }: Props) {
  const asphalt = useSharedTexture(streetTileUrl('asphalt_main'), {
    wrap: 'repeat',
    repeat: [1, 2],
  });
  const railY = DECK_TOP_Y + DECK_H / 2 + 0.09;
  const info = useObjectInfoProps('road.bridge', { anchorY: 0.8 });

  // ── 批次 28 二轮：静态件合并（几何逐件全等；灯组原嵌套 group 偏移烘进部件坐标）──
  const geos = useMemo(() => {
    const solids: MergePart[] = [
      // 两侧栏杆（纵梁）
      boxPart(0.06, 0.18, length, +(DECK_W / 2 - 0.03), railY, 0, '#9aa1ab'),
      boxPart(0.06, 0.18, length, -(DECK_W / 2 - 0.03), railY, 0, '#9aa1ab'),
    ];
    // 栏杆端柱 ×4
    for (const side of [-1, 1]) for (const end of [-1, 1]) {
      solids.push(boxPart(0.09, 0.22, 0.09, side * (DECK_W / 2 - 0.03), railY + 0.02, end * (length / 2 - 0.12), '#8a919c'));
    }
    // 桥墩 ×4（入水）
    for (const sx of [-1, 1]) for (const sz of [-1, 1]) {
      solids.push(boxPart(0.3, 0.5, 0.3, sx * 0.6, -0.2, sz * (length / 2 - 0.35), '#6b7280'));
    }
    // 桥头灯立柱 ×2（暖光灯球另入 glow 组）
    for (const side of [-1, 1]) {
      solids.push(cylPart(0.025, 0.035, 0.56, 6, side * (DECK_W / 2 + 0.12), 0.28, -length / 2 + 0.15, '#4a5260'));
    }
    const glow: GroupedMergePart[] = [];
    for (const side of [-1, 1]) {
      glow.push({
        geo: new THREE.SphereGeometry(0.05, 8, 6),
        x: side * (DECK_W / 2 + 0.12), y: 0.58, z: -length / 2 + 0.15,
        mat: 0,
      });
    }
    return { solids: mergeParts(solids), glow: mergeGrouped(glow) };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [length]);
  useEffect(() => () => {
    geos.solids.dispose();
    geos.glow.dispose();
  }, [geos]);

  return (
    <group {...info} position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* 桥面（贴图独立，接影） */}
      <mesh position={[0, DECK_TOP_Y, 0]} receiveShadow>
        <boxGeometry args={[DECK_W, DECK_H, length]} />
        <meshStandardMaterial
          map={asphalt ?? undefined}
          color={asphalt ? '#ffffff' : '#2a3240'}
          roughness={0.9}
          metalness={0.05}
        />
      </mesh>
      {/* 栏杆/端柱/桥墩/灯杆 —— 顶点色合并 1 mesh（批次 28 二轮） */}
      <mesh geometry={geos.solids}>
        <meshStandardMaterial vertexColors roughness={0.65} metalness={0.35} envMapIntensity={0.8} />
      </mesh>
      {/* 桥头灯暖光球 ×2 —— emissive 合并 1 mesh（材质逐字段与原一致） */}
      <mesh geometry={geos.glow}>
        <meshStandardMaterial color="#ffd9a0" emissive="#ffd9a0" emissiveIntensity={0.55} roughness={0.3} />
      </mesh>
    </group>
  );
}

export interface BridgeSpot {
  key: string;
  x: number;
  z: number;
  rotation: number;
}

/**
 * 通式求桥位：对每条 len > MAIN_ROAD_MIN_LEN 放射主干道，解与运河 z=CANAL_Z 的交点；
 * 交点 |x| ≤ CANAL_HALF_X 即建桥（批次 20 通式自适应：CANAL_Z/CANAL_HALF_X 与
 * VirtualCityCityMap::WaterLayer 常量同源 import，运河东延 ±48 后命中
 * edu / medical / fin_sub / sports_new_city / bay_new_town / university_town / wetland 等）。
 */
export function CanalBridgeSpots(): BridgeSpot[] {
  const spots: BridgeSpot[] = [];
  for (const d of VIRTUAL_CITY_DISTRICTS) {
    if (d.id === 'finance') continue;
    const c = districtCenter(d.id);
    if (c.z <= CANAL_Z) continue; // 只考虑运河以北（z 更大）城区的放射路
    const len = Math.sqrt(c.x * c.x + c.z * c.z);
    if (len < MAIN_ROAD_MIN_LEN) continue; // 仅主干道
    const xAt = c.x * (CANAL_Z / c.z);
    if (Math.abs(xAt) > CANAL_HALF_X) continue;
    spots.push({
      key: `bridge-${d.id}`,
      x: xAt,
      z: CANAL_Z,
      rotation: Math.atan2(-c.x, -c.z),
    });
  }
  return spots;
}

/** 全部运河桥（VirtualCityCityMap 单挂载点）。 */
export function CanalBridges() {
  const spots = useMemo(() => CanalBridgeSpots(), []);
  return (
    <>
      {spots.map((s) => (
        <CanalBridge key={s.key} x={s.x} z={s.z} rotation={s.rotation} />
      ))}
    </>
  );
}
