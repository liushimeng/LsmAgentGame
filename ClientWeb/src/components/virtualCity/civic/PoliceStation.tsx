/**
 * 警局（18-AA · §5.2 PoliceStation）
 *   蓝白外墙 + 门厅雨棚 + 警灯柱 + 巡逻车 1 辆。布点 (-5.8, -8.5)。
 *
 * 19-Blender3D模型集成：用 <Model url={...}> 包一层，原程序化几何保留为 children fallback。
 */
import { u, worldDims, sizeTargetFor } from '../cityScale';
import { Model, blenderModelsEnabled as blenderEnabled } from '@/engine3d';
import { modelUrl } from '@/assets/models';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const BLUE = '#2a4e7a';
const WHITE = '#e8e3dc';
const CAR_BLACK = '#2a2e36';

/**
 * 建筑尺寸（批次 29）：取 cityScale.REAL_DIMS_M.policeStation = 12.1 × 7.0（占地）× 6.6（总高）。
 * 主体轮廓 = 顶部白色腰线（12.1 宽，比主屋每边外挑 0.05）；主屋高取表值总高 6.6 m
 * （原 6.0 m 与表值差 0.6 m）；门厅立柱改为「撑到雨棚底」（原 7 m 高会穿出屋顶，
 * 是无 `u()` 时代的遗留，曾把 fallback 包围盒撑到 7.0 m）。
 */
const POLICE = worldDims('policeStation');
/** 腰线外挑（单侧 0.05 m）。 */
const BELT_EAVE = u(0.05);
/** 门厅雨棚高度（门厅立柱顶 = 此值）。 */
const CANOPY_Y = u(5.4);
/** GLB 尺寸/落地校验目标（dev 态）。 */
const POLICE_SIZE_TARGET = sizeTargetFor('policeStation', { label: 'civic/police_station' });

export function PoliceStation() {
  const url = modelUrl('civic', 'police_station');
  // 批次 28 B2：警察局信息交互。
  const info = useObjectInfoProps('civic.police-station', { anchorY: 5 });
  if (!url || !blenderEnabled()) {
    return (
      <group {...info}>
        <PoliceStationFallback />
      </group>
    );
  }
  return (
    <group {...info}>
      <Model
        url={url}
        sizeTarget={POLICE_SIZE_TARGET}
        position={[-5.8, 0, -8.5]}
        castShadow
        receiveShadow
      >
        <PoliceStationFallback />
      </Model>
    </group>
  );
}

/** 程序化几何 fallback（19-Blender3D模型集成 · 02 架构设计 §5.2 降级链）。 */
function PoliceStationFallback() {
  return (
    <group position={[-5.8, 0, -8.5]}>
      {/* 主屋：占地 = 表值轮廓（腰线外挑 2×0.05），高 = 表值总高 6.6 m */}
      <mesh position={[0, POLICE.y / 2, 0]} castShadow receiveShadow>
        <boxGeometry args={[POLICE.x - BELT_EAVE * 2, POLICE.y, POLICE.z]} />
        <meshStandardMaterial color={BLUE} roughness={0.85} />
      </mesh>
      {/* 顶部白色腰线（= 表值轮廓 12.1 宽） */}
      <mesh position={[0, CANOPY_Y, POLICE.z / 2 + BELT_EAVE]}>
        <boxGeometry args={[POLICE.x, u(0.6), u(0.05)]} />
        <meshStandardMaterial color={WHITE} />
      </mesh>
      {/* 门厅雨棚 */}
      <mesh position={[0, CANOPY_Y, u(4.4)]} castShadow>
        <boxGeometry args={[u(3.5), u(0.12), u(1.2)]} />
        <meshStandardMaterial color="#4a6b95" />
      </mesh>
      <mesh position={[0, CANOPY_Y, u(4.6)]} castShadow>
        <boxGeometry args={[u(3.5), u(0.15), u(1.2)]} />
        <meshStandardMaterial color={WHITE} />
      </mesh>
      {/* 门厅立柱 ×2（高 = 雨棚底 → 不穿出屋顶） */}
      {[-u(1.4), u(1.4)].map((dx, i) => (
        <mesh key={`col-${i}`} position={[dx, CANOPY_Y / 2, u(4.6)]}>
          <cylinderGeometry args={[u(0.1), u(0.1), CANOPY_Y, 6]} />
          <meshStandardMaterial color={WHITE} />
        </mesh>
      ))}
      {/* 警灯柱 + 警灯（红蓝闪烁感，单色） */}
      <mesh position={[-u(5), u(2.5), u(4.5)]}>
        <cylinderGeometry args={[u(0.08), u(0.08), u(5), 6]} />
        <meshStandardMaterial color="#5a6270" metalness={0.5} roughness={0.5} />
      </mesh>
      <mesh position={[-u(5), u(5.2), u(4.5)]}>
        <boxGeometry args={[u(0.6), u(0.2), u(0.3)]} />
        <meshStandardMaterial color="#ff3b30" emissive="#ff3b30" emissiveIntensity={0.7} />
      </mesh>
      <mesh position={[-u(5), u(5.5), u(4.5)]}>
        <boxGeometry args={[u(0.6), u(0.2), u(0.3)]} />
        <meshStandardMaterial color="#5a86b8" emissive="#5a86b8" emissiveIntensity={0.7} />
      </mesh>
      {/* 巡逻车（黑色蓝条 box 简化） */}
      <mesh position={[u(4), u(0.6), u(4.5)]} castShadow>
        <boxGeometry args={[u(2.2), u(1), u(0.95)]} />
        <meshStandardMaterial color={CAR_BLACK} roughness={0.4} metalness={0.4} />
      </mesh>
      <mesh position={[u(4), u(1.4), u(4.5)]} castShadow>
        <boxGeometry args={[u(1.3), u(0.7), u(0.9)]} />
        <meshStandardMaterial color={CAR_BLACK} roughness={0.4} metalness={0.4} />
      </mesh>
      <mesh position={[u(3.2), u(0.7), u(4.5)]}>
        <boxGeometry args={[u(0.5), u(0.15), u(0.9)]} />
        <meshStandardMaterial color="#5a86b8" />
      </mesh>
    </group>
  );
}