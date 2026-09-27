/**
 * 市政厅（18-AA · §5.2 CityHall）
 *   3 层石材体量 + 门廊 4 柱 + 钟楼（4 面钟）+ 旗杆。布点 (-9.0, -1.7)。
 *
 * 19-Blender3D模型集成：用 <Model url={...}> 包一层，原程序化几何保留为 children fallback。
 *   - modelUrl 返回 '' → 走 fallback（程序化几何，与原行为像素一致）
 *   - .glb 加载成功 → 渲染真实模型，children 不渲染
 *   - 切换开关：engine3d blenderModelsEnabled()（localStorage disable-blender-models=1）强制 fallback
 */
import { u, worldDims, sizeTargetFor, buildingHeight } from '../cityScale';
import { Model, blenderModelsEnabled as blenderEnabled } from '@/engine3d';
import { modelUrl } from '@/assets/models';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const STONE = '#a8a4a0';
const STONE_DARK = '#7a7a76';
const ROOF_DARK = '#3a3f4a';
const GOLD = '#d4a017';

/**
 * 市政厅布点（世界坐标）：主楼/模型 group 原点。
 * 批次 23 起导出为市民之声气泡锚点基准（CityVoiceBubbleLayer），
 * CityHall 本体的两处落位（Model / fallback group）同用一份，防止坐标漂移。
 */
export const CITY_HALL_POSITION = { x: -9.0, z: -1.7 } as const;

/**
 * 建筑尺寸（批次 29）：取 cityScale.REAL_DIMS_M.cityHall = 14.4 × 8.4（占地）× 20.0（总高）。
 *   - 主体轮廓 = 屋顶板（14.4 × 8.4）；主楼比屋顶每边内收 0.2 m（檐口外挑 2×0.2）；
 *   - 总高 = 钟楼尖顶顶面（主楼 3 层 + 钟塔 + 尖顶），fallback 与 GLB 同一行表值。
 * 此处硬编码的 u(14)/u(9)/u(8)/u(17) 等旧值已删除。
 */
const HALL = worldDims('cityHall');
/** 主楼 3 层高；钟塔坐 3 层顶，高 7 m。 */
const HALL_BODY_H = buildingHeight(3);
const TOWER_H = u(7);
/** 钟塔底面（= 主楼顶）/ 中心 / 顶面。 */
const TOWER_BASE_Y = buildingHeight(3);
const TOWER_CENTER_Y = TOWER_BASE_Y + TOWER_H / 2;
const TOWER_TOP_Y = TOWER_BASE_Y + TOWER_H;
/** 尖顶高度 = 表值总高 − 钟塔顶面 ⇒ 尖顶顶面恰为 20.0 m。 */
const SPIRE_H = HALL.y - TOWER_TOP_Y;
/** 檐口外挑（单侧 0.2 m）。 */
const EAVE = u(0.2);
/** GLB 尺寸/落地校验目标（dev 态；city_hall.glb 原点在基座，minY=0）。 */
const CITY_HALL_SIZE_TARGET = sizeTargetFor('cityHall', { label: 'civic/city_hall' });

/** 市民之声气泡锚点：钟楼尖顶（= 表值总高 20.0 m）再上浮 0.6，悬浮于市政厅上空。 */
export const CITY_HALL_BUBBLE_ANCHOR: [number, number, number] = [
  CITY_HALL_POSITION.x,
  HALL.y + 0.6,
  CITY_HALL_POSITION.z,
];

/** 全局开关：测试/回滚用，缺省 false = 用 .glb */
export function CityHall() {
  const url = modelUrl('civic', 'city_hall');
  // 批次 28 B2：市政厅信息交互（根组包住 GLB/fallback 双路径，零几何改动）。
  const info = useObjectInfoProps('civic.city-hall', { anchorY: 5 });
  // url 缺失或全局开关关闭 → 直接渲染原程序化几何
  if (!url || !blenderEnabled()) {
    return (
      <group {...info}>
        <CityHallFallback />
      </group>
    );
  }
  return (
    <group {...info}>
      <Model
        url={url}
        sizeTarget={CITY_HALL_SIZE_TARGET}
        position={[CITY_HALL_POSITION.x, 0, CITY_HALL_POSITION.z]}
        castShadow
        receiveShadow
      >
        <CityHallFallback />
      </Model>
    </group>
  );
}

/**
 * 保留原程序化几何作为 fallback（19-Blender3D模型集成 · 02 架构设计 §5.2 降级链）。
 *
 * 批次 29 度量衡统一：占地轮廓 / 总高 / 尖顶高度全部由 REAL_DIMS_M.cityHall 导出
 * （主楼 = 表值 − 檐口外挑；尖顶顶面恰为表值总高 20.0 m）；同时修掉两处「旧 1 单位
 * ≈ 1 m 时代」遗留的裸世界单位常量（楼层线 y=3/6、台阶 y=0.15+… 曾把 fallback
 * 包围盒抬到 60 m 高、台阶悬到 1.5 m），现一律 u() 米制。
 * 门廊立柱 / 雨棚 / 台阶 / 旗杆为外挑装饰，不入表值轮廓（见 cityScale 表头口径）。
 */
function CityHallFallback() {
  const bodyW = HALL.x - EAVE * 2;   // 14.0 m（表值轮廓 − 双侧檐口）
  const bodyD = HALL.z - EAVE * 2;   // 8.0 m
  return (
    <group position={[CITY_HALL_POSITION.x, 0, CITY_HALL_POSITION.z]}>
      {/* 主楼：14.0 × 9.0(3 层) × 8.0 m */}
      <mesh position={[0, HALL_BODY_H / 2, 0]} castShadow receiveShadow>
        <boxGeometry args={[bodyW, HALL_BODY_H, bodyD]} />
        <meshStandardMaterial color={STONE} roughness={0.9} />
      </mesh>
      {/* 屋顶（坡顶伪装用扁顶）= 表值占地轮廓 14.4 × 8.4 */}
      <mesh position={[0, HALL_BODY_H + EAVE, 0]} castShadow>
        <boxGeometry args={[HALL.x, u(0.4), HALL.z]} />
        <meshStandardMaterial color={ROOF_DARK} />
      </mesh>
      {/* 楼层水平线（1/2 层高，正面 z = 主体半深 + 檐口） */}
      {[buildingHeight(1), buildingHeight(2)].map((y, i) => (
        <mesh key={`floor-${i}`} position={[0, y, bodyD / 2 + EAVE]}>
          <boxGeometry args={[bodyW + u(0.1), u(0.08), u(0.05)]} />
          <meshStandardMaterial color={STONE_DARK} />
        </mesh>
      ))}
      {/* 门廊：4 立柱 + 雨棚 + 台阶（高度随主楼层高，不超总高） */}
      {[-u(4.5), -u(1.5), u(1.5), u(4.5)].map((dx, i) => (
        <mesh key={`porch-col-${i}`} position={[dx, HALL_BODY_H / 2, u(5)]} castShadow>
          <cylinderGeometry args={[u(0.3), u(0.3), HALL_BODY_H, 8]} />
          <meshStandardMaterial color={STONE_DARK} roughness={0.9} />
        </mesh>
      ))}
      <mesh position={[0, HALL_BODY_H + u(0.3), u(5.4)]} castShadow>
        <boxGeometry args={[u(12), u(0.4), u(2)]} />
        <meshStandardMaterial color={STONE_DARK} />
      </mesh>
      {/* 台阶 3 级 */}
      {[0, 1, 2].map((i) => (
        <mesh key={`step-${i}`} position={[0, u(0.15) + i * u(0.3), u(7 + i * 0.3)]}>
          <boxGeometry args={[u(8 - i * 0.6), u(0.3), u(0.6)]} />
          <meshStandardMaterial color={STONE_DARK} />
        </mesh>
      ))}
      {/* 钟楼：方塔 + 4 面钟（塔底 = 主楼顶 9 m，塔顶 = 16 m） */}
      <mesh position={[0, TOWER_CENTER_Y, 0]} castShadow>
        <boxGeometry args={[u(3.2), TOWER_H, u(3.2)]} />
        <meshStandardMaterial color={STONE_DARK} roughness={0.9} />
      </mesh>
      {[
        { rot: [0, 0, 0], off: [0, TOWER_CENTER_Y, u(1.62)] },
        { rot: [0, Math.PI / 2, 0], off: [u(1.62), TOWER_CENTER_Y, 0] },
        { rot: [0, Math.PI, 0], off: [0, TOWER_CENTER_Y, -u(1.62)] },
        { rot: [0, -Math.PI / 2, 0], off: [-u(1.62), TOWER_CENTER_Y, 0] },
      ].map((p, i) => (
        <mesh key={`clock-${i}`} position={p.off as any} rotation={p.rot as any}>
          <circleGeometry args={[u(0.6), 24]} />
          <meshStandardMaterial color={GOLD} emissive={GOLD} emissiveIntensity={0.5} />
        </mesh>
      ))}
      {/* 钟塔尖顶：底面接钟塔顶面（16 m），顶面 = 表值总高 20.0 m */}
      <mesh position={[0, TOWER_TOP_Y + SPIRE_H / 2, 0]} castShadow>
        <coneGeometry args={[u(2), SPIRE_H, 4]} />
        <meshStandardMaterial color={ROOF_DARK} />
      </mesh>
      {/* 旗杆 + 旗（简化 box；外挑装饰，不入轮廓） */}
      <mesh position={[-u(8), u(6), u(6)]}>
        <cylinderGeometry args={[u(0.06), u(0.06), u(12), 6]} />
        <meshStandardMaterial color="#5a6270" metalness={0.5} />
      </mesh>
      <mesh position={[-u(7.4), u(11), u(6)]} castShadow>
        <boxGeometry args={[u(1.4), u(0.9), u(0.02)]} />
        <meshStandardMaterial color={GOLD} />
      </mesh>
    </group>
  );
}