/**
 * CanalBridge — 跨运河桥（16-3D城市WebGL质感与城市补全 · 阶段 S）：
 *
 * 桥面（box 微高路面）+ 两侧栏杆 + 端柱 + 4 桥墩 + 2 桥头灯。
 * 位置由 CanalBridgeSpots() 通式计算：len>12 放射干道与运河 z=17 的交点
 * （当前数据命中 edu_district / medical_city 两条 → 2 座桥）。
 *
 * 契约：lag_docs/虚拟城市/已实现/16-3D城市WebGL质感与城市补全/02-架构设计 §6。
 */

import { useMemo } from 'react';
import { streetTileUrl } from '@/assets/images/wealth';
import { districtCenter, WEALTH_DISTRICTS } from '@/types/wealth';
import { useSharedTexture } from './textureCache';

/** 运河中心 z（与 WealthCityMap::WaterLayer 契约一致）。 */
const CANAL_Z = 17;
/** 运河 x 半跨（水面横贯 x ∈ [-32, 32]）。 */
const CANAL_HALF_X = 32;
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
  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* 桥面 */}
      <mesh position={[0, DECK_TOP_Y, 0]} receiveShadow castShadow>
        <boxGeometry args={[DECK_W, DECK_H, length]} />
        <meshStandardMaterial
          map={asphalt ?? undefined}
          color={asphalt ? '#ffffff' : '#2a3240'}
          roughness={0.9}
          metalness={0.05}
        />
      </mesh>
      {/* 两侧栏杆（纵梁） */}
      {[-1, 1].map((side) => (
        <mesh key={`rail-${side}`} position={[side * (DECK_W / 2 - 0.03), railY, 0]} castShadow>
          <boxGeometry args={[0.06, 0.18, length]} />
          <meshStandardMaterial color="#9aa1ab" roughness={0.6} metalness={0.4} envMapIntensity={0.8} />
        </mesh>
      ))}
      {/* 栏杆端柱（4 根） */}
      {[-1, 1].flatMap((side) => [-1, 1].map((end) => (
        <mesh
          key={`post-${side}-${end}`}
          position={[side * (DECK_W / 2 - 0.03), railY + 0.02, end * (length / 2 - 0.12)]}
          castShadow
        >
          <boxGeometry args={[0.09, 0.22, 0.09]} />
          <meshStandardMaterial color="#8a919c" roughness={0.6} metalness={0.4} />
        </mesh>
      )))}
      {/* 桥墩 ×4（入水） */}
      {[-1, 1].flatMap((sx) => [-1, 1].map((sz) => (
        <mesh key={`pier-${sx}-${sz}`} position={[sx * 0.6, -0.2, sz * (length / 2 - 0.35)]}>
          <boxGeometry args={[0.3, 0.5, 0.3]} />
          <meshStandardMaterial color="#6b7280" roughness={0.85} />
        </mesh>
      )))}
      {/* 桥头灯 ×2（立柱 + 暖光顶，呼应 StreetLight 配色） */}
      {[-1, 1].map((side) => (
        <group key={`lamp-${side}`} position={[side * (DECK_W / 2 + 0.12), 0, -length / 2 + 0.15]}>
          <mesh position={[0, 0.28, 0]} castShadow>
            <cylinderGeometry args={[0.025, 0.035, 0.56, 6]} />
            <meshStandardMaterial color="#4a5260" roughness={0.5} metalness={0.6} envMapIntensity={0.8} />
          </mesh>
          <mesh position={[0, 0.58, 0]}>
            <sphereGeometry args={[0.05, 8, 6]} />
            <meshStandardMaterial color="#ffd9a0" emissive="#ffd9a0" emissiveIntensity={0.55} roughness={0.3} />
          </mesh>
        </group>
      ))}
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
 * 通式求桥位：对每条 len>12 放射干道，解与运河 z=CANAL_Z 的交点；
 * 交点 |x| ≤ CANAL_HALF_X 即建桥。当前 16 城区数据命中 edu / medical 两条。
 */
export function CanalBridgeSpots(): BridgeSpot[] {
  const spots: BridgeSpot[] = [];
  for (const d of WEALTH_DISTRICTS) {
    if (d.id === 'finance') continue;
    const c = districtCenter(d.id);
    if (c.z <= CANAL_Z) continue; // 只考虑运河以北（z 更大）城区的放射路
    const len = Math.sqrt(c.x * c.x + c.z * c.z);
    if (len < 12) continue; // 仅主干道
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

/** 全部运河桥（WealthCityMap 单挂载点）。 */
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
