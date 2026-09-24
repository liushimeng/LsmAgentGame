/**
 * ParkingLot — 划线停车场 + 静态车（16-3D城市WebGL质感与城市补全 · 阶段 T）：
 *
 * 商业中心东北角：沥青场面 + 8 条白色划线（双排 4 位）+ 3 辆简化静态车
 * （2-box 车身 + 4 轮；色取自 StreetPropsLayer::VEHICLE_COLORS 同源色板）。
 *
 * 契约：lag_docs/虚拟城市/已实现/16-3D城市WebGL质感与城市补全/02-架构设计 §8.4。
 */

import { streetTileUrl } from '@/assets/images/virtualCity';
import { u } from '../cityScale';
import { useSharedTexture } from '../textureCache';

const LOT_X = 12.4; // commerce (10,-2) 东北角：pad 内缘 x 6..14 / z -6..2，
const LOT_Z = -4.9; // 楼群占中心 ±3.1 → 场地收窄到 2.8×2.0 贴 pad 东北角避让
const LOT_W = 2.8;
const LOT_D = 2.0;

/** 静态车色板（与 StreetPropsLayer VEHICLE_VARIANTS 主色同源；此处独立常量避免循环 import）。 */
const CAR_COLORS = ['#3b6bb0', '#e8b930', '#8a6a3d'];

/** 车位划线：双排 × 4 条竖线 + 中通道。 */
const STALL_X = [-1.2, -0.4, 0.4, 1.2];

interface Props {
  rotation?: number;
}

function StaticCar({ x, z, color, rotation = 0 }: { x: number; z: number; color: string; rotation?: number }) {
  const l = u(4.5) / 2; // 车长一半 ≈ 0.225（借 Vehicle sedan 尺寸）
  const w = 0.09;
  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* 车身 */}
      <mesh position={[0, u(0.55), 0]} castShadow>
        <boxGeometry args={[w, u(1.1), l * 2]} />
        <meshStandardMaterial color={color} roughness={0.4} metalness={0.5} envMapIntensity={0.9} />
      </mesh>
      {/* 车顶（略窄） */}
      <mesh position={[0, u(1.25), -0.02]} castShadow>
        <boxGeometry args={[w * 0.85, u(0.5), l * 1.1]} />
        <meshStandardMaterial color={color} roughness={0.4} metalness={0.5} envMapIntensity={0.9} />
      </mesh>
      {/* 4 轮（扁圆柱，轴沿 x） */}
      {[
        [-w / 2 - 0.005, l * 0.6],
        [w / 2 + 0.005, l * 0.6],
        [-w / 2 - 0.005, -l * 0.6],
        [w / 2 + 0.005, -l * 0.6],
      ].map(([px, pz], i) => (
        <mesh key={`wheel-${i}`} position={[px, u(0.35), pz]} rotation={[Math.PI / 2, 0, 0]}>
          <cylinderGeometry args={[u(0.35), u(0.35), 0.02, 10]} />
          <meshStandardMaterial color="#1a1d22" roughness={0.9} />
        </mesh>
      ))}
    </group>
  );
}

export function ParkingLot({ rotation }: Props) {
  const asphalt = useSharedTexture(streetTileUrl('asphalt_side'), {
    wrap: 'repeat',
    repeat: [3, 2],
  });
  // 静态车布点（确定性：3 辆停 1/3 排）
  const cars = [
    { x: -1.2, z: -0.55, color: CAR_COLORS[0], rotation: Math.PI },
    { x: -0.4, z: -0.55, color: CAR_COLORS[1], rotation: Math.PI },
    { x: 0.8, z: 0.55, color: CAR_COLORS[2], rotation: 0 },
  ];
  return (
    <group position={[LOT_X, 0, LOT_Z]} rotation={[0, rotation ?? 0, 0]}>
      {/* 场地面 */}
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.026, 0]} receiveShadow>
        <planeGeometry args={[LOT_W, LOT_D]} />
        <meshStandardMaterial
          map={asphalt ?? undefined}
          color={asphalt ? '#ffffff' : '#242c38'}
          roughness={0.94}
        />
      </mesh>
      {/* 双排划线（前/后排各 4 条，白 box 薄条） */}
      {[-0.55, 0.55].flatMap((rowZ) =>
        STALL_X.map((sx) => (
          <mesh key={`line-${rowZ}-${sx}`} position={[sx, 0.028, rowZ]} rotation={[-Math.PI / 2, 0, 0]}>
            <planeGeometry args={[0.04, 0.5]} />
            <meshStandardMaterial color="#e8eaee" roughness={0.85} />
          </mesh>
        )),
      )}
      {/* 静态车 ×3 */}
      {cars.map((c, i) => (
        <StaticCar key={`car-${i}`} x={c.x} z={c.z} color={c.color} rotation={c.rotation} />
      ))}
    </group>
  );
}

export default ParkingLot;
