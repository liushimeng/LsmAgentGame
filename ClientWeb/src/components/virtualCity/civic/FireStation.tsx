/**
 * 消防站（18-AA · §5.2 FireStation）
 *   红白条外墙 + 2 个车库门 + 滑杆塔 + 红色消防车 1 辆。布点 (8, -12)。
 *   批次 41 D1：站内消防车改用 <Vehicle> 静止摆放（truck + 红涂装 + 车顶警灯）。
 *
 * 19-Blender3D模型集成：用 <Model url={...}> 包一层，原程序化几何保留为 children fallback。
 */
import { u, worldDims, sizeTargetFor } from '../cityScale';
import { Model, blenderModelsEnabled as blenderEnabled } from '@/engine3d';
import { modelUrl } from '@/assets/models';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';
import { Vehicle } from '../props/Vehicle';

const RED = '#c0392b';
const WHITE = '#e8e3dc';
const DOOR_DARK = '#3a4250';

/**
 * 建筑尺寸（批次 29）：取 cityScale.REAL_DIMS_M.fireStation = 18.1 × 8.0（占地）× 8.5（总高）。
 * 主体轮廓 = 顶部白色腰线（18.1 宽，比主屋每边外挑 0.05）；主屋高 7.0 m 为构图常量
 * （4.4 m 车库门 + 门楣），总高由滑杆塔顶警灯顶面接表 8.5 m。
 */
const FIRE = worldDims('fireStation');
/** 腰线外挑（单侧 0.05 m）。 */
const BELT_EAVE = u(0.05);
/** 主屋高 / 车库门高（构图常量，均 ≤ 表值总高）。 */
const BODY_H = u(7);
const DOOR_H = u(4.4);
/** 滑杆塔高（独立小塔）。 */
const SLIDE_TOWER_H = u(8);
/** 站顶警灯半径。 */
const BEACON_R = u(0.15);
/** GLB 尺寸/落地校验目标（dev 态）。 */
const FIRE_SIZE_TARGET = sizeTargetFor('fireStation', { label: 'civic/fire_station' });

export function FireStation() {
  const url = modelUrl('civic', 'fire_station');
  // 批次 28 B2：消防站信息交互。
  const info = useObjectInfoProps('civic.fire-station', { anchorY: 5 });
  return (
    <group {...info}>
      {!url || !blenderEnabled() ? (
        <FireStationFallback />
      ) : (
        <Model
          url={url}
          sizeTarget={FIRE_SIZE_TARGET}
          position={[8, 0, -12]}
          castShadow
          receiveShadow
        >
          <FireStationFallback />
        </Model>
      )}
      {/* 批次 41 D1：站内消防车 + 车顶警灯 —— 主渲染路径（<Model> 的 sibling；
          批次 41 art 线已重导出站 GLB 并移除烘焙 2-box 假车，无双重渲染）。
          站局部坐标系与 fallback 同源（外层 group position=[8,0,-12]）。
          from===to、speed=0：组件内 len 兜底 1、角度 0，静态摆放不占路网车流；
          无 GLB 分支同样渲染（Vehicle 自行走几何/sprite fallback 简版，可接受）。 */}
      <group position={[8, 0, -12]}>
        <Vehicle from={[-u(6), u(5.5)]} to={[-u(6), u(5.5)]} variant="truck" speed={0} />
        {/* 车顶警灯 ×2（驾驶室上方；材质名约定 'Beacon'，供未来 GLB 化后接入批次 41
            B2 的昼夜调制）。y = 车顶 0.36u（VEHICLE_GROUND_Y 0.02 + 车高 0.34）+ 半高。 */}
        {[-u(2.2), -u(3.3)].map((bx, j) => (
          <mesh key={`fire-beacon-${j}`} position={[bx, u(3.75), u(5.5)]}>
            <boxGeometry args={[u(0.06), u(0.03), u(0.03)]} />
            <meshStandardMaterial color="#ff3b30" emissive="#ff3b30" emissiveIntensity={0.9} />
          </mesh>
        ))}
      </group>
    </group>
  );
}

/** 程序化几何 fallback（19-Blender3D模型集成 · 02 架构设计 §5.2 降级链）。 */
function FireStationFallback() {
  return (
    <group position={[8, 0, -12]}>
      {/* 主屋：占地 = 表值轮廓 − 腰线外挑，高 = 构图常量 7 m（车库层） */}
      <mesh position={[0, BODY_H / 2, 0]} castShadow receiveShadow>
        <boxGeometry args={[FIRE.x - BELT_EAVE * 2, BODY_H, FIRE.z]} />
        <meshStandardMaterial color={RED} roughness={0.85} />
      </mesh>
      {/* 顶部白色腰线（= 表值轮廓 18.1 宽） */}
      <mesh position={[0, u(5.5), FIRE.z / 2 + BELT_EAVE]}>
        <boxGeometry args={[FIRE.x, u(0.8), u(0.05)]} />
        <meshStandardMaterial color={WHITE} />
      </mesh>
      {/* 车库门 ×2：面向 +z */}
      {[-u(4), u(4)].map((dx, i) => (
        <group key={`door-${i}`} position={[dx, 0, FIRE.z / 2 + BELT_EAVE]}>
          <mesh position={[0, DOOR_H / 2, 0.01]} castShadow>
            <boxGeometry args={[u(5), DOOR_H, u(0.05)]} />
            <meshStandardMaterial color={DOOR_DARK} roughness={0.7} />
          </mesh>
          {/* 4 道横纹装饰 */}
          {[u(1), u(2), u(3), u(3.8)].map((yy, j) => (
            <mesh key={`stripe-${i}-${j}`} position={[0, yy, 0.02]}>
              <boxGeometry args={[u(5), u(0.06), u(0.02)]} />
              <meshStandardMaterial color={RED} />
            </mesh>
          ))}
        </group>
      ))}
      {/* 滑杆塔（独立小塔）：box + 杆 */}
      <mesh position={[u(7), SLIDE_TOWER_H / 2, -u(3.5)]} castShadow>
        <boxGeometry args={[u(0.8), SLIDE_TOWER_H, u(0.8)]} />
        <meshStandardMaterial color={RED} />
      </mesh>
      <mesh position={[u(7), SLIDE_TOWER_H / 2, -u(3.2)]}>
        <cylinderGeometry args={[u(0.06), u(0.06), SLIDE_TOWER_H, 6]} />
        <meshStandardMaterial color="#3a3f4a" metalness={0.6} />
      </mesh>
      {/* 站顶警灯（蓝白闪烁感，单色静态）：顶面 = 表值总高 8.5 m */}
      <mesh position={[u(7), FIRE.y - BEACON_R, -u(3.5)]}>
        <sphereGeometry args={[BEACON_R, 8, 8]} />
        <meshStandardMaterial color="#5a86b8" emissive="#5a86b8" emissiveIntensity={0.5} />
      </mesh>
      {/* 批次 41 D1：消防车已移至 FireStation() 主渲染路径（站 GLB 重导出后无烘焙车，
          fallback 内不再放车，避免双渲染） */}
    </group>
  );
}