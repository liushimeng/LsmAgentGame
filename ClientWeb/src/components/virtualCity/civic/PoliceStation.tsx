/**
 * 警局（18-AA · §5.2 PoliceStation）
 *   蓝白外墙 + 门厅雨棚 + 警灯柱 + 巡逻车 1 辆。布点 (-5.8, -8.5)。
 *   批次 41 D1：站前巡逻车改用 <Vehicle> 静止摆放（sedan_silver 白涂装 + 车顶灯条）。
 *
 * 19-Blender3D模型集成：用 <Model url={...}> 包一层，原程序化几何保留为 children fallback。
 */
import { u, worldDims, sizeTargetFor } from '../cityScale';
import { Model, blenderModelsEnabled as blenderEnabled } from '@/engine3d';
import { modelUrl } from '@/assets/models';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';
import { Vehicle } from '../props/Vehicle';

const BLUE = '#2a4e7a';
const WHITE = '#e8e3dc';

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
  return (
    <group {...info}>
      {!url || !blenderEnabled() ? (
        <PoliceStationFallback />
      ) : (
        <Model
          url={url}
          sizeTarget={POLICE_SIZE_TARGET}
          position={[-5.8, 0, -8.5]}
          castShadow
          receiveShadow
        >
          <PoliceStationFallback />
        </Model>
      )}
      {/* 批次 41 D1：站前巡逻车 + 车顶灯条 —— 主渲染路径（<Model> 的 sibling；
          批次 41 art 线已重导出站 GLB 并移除烘焙 2-box 假车，无双重渲染）。
          站局部坐标系与 fallback 同源（外层 group position=[-5.8,0,-8.5]）。
          from===to、speed=0：组件内 len 兜底 1、角度 0，静态摆放不占路网车流；
          无 GLB 分支同样渲染（Vehicle 自行走几何/sprite fallback 简版，可接受）。 */}
      <group position={[-5.8, 0, -8.5]}>
        <Vehicle from={[u(4), u(4.5)]} to={[u(4), u(4.5)]} variant="sedan" glbName="sedan_silver" speed={0} />
        {/* 车顶蓝白灯条（材质名约定 'Beacon'，供未来 GLB 化后接入批次 41 B2 昼夜调制）：
            0.10 长 × 0.03 高 × 0.025 厚 u，长轴沿车宽 Z；y = 车顶 0.165u + 半高 0.015。 */}
        <mesh position={[u(4), u(1.8), u(4.5)]}>
          <boxGeometry args={[u(0.025), u(0.03), u(0.1)]} />
          <meshStandardMaterial color="#3d6fe8" emissive="#3d6fe8" emissiveIntensity={0.9} />
        </mesh>
      </group>
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
      {/* 批次 41 D1：巡逻车已移至 PoliceStation() 主渲染路径（站 GLB 重导出后无烘焙车，
          fallback 内不再放车，避免双渲染） */}
    </group>
  );
}