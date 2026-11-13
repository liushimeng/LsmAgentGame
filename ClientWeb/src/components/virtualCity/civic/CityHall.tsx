/**
 * 市政厅（18-AA · §5.2 CityHall）
 *   3 层石材体量 + 门廊 4 柱 + 钟楼（4 面钟）+ 旗杆。布点 (-9.0, -1.7)。
 *
 * 批次 90 昼夜发光：从裸 <Model> 迁到 <CivicGlbPiece>（范式同 GasStation）。
 *   - 降级链（blenderModelsEnabled → modelUrl → 加载中/失败 ⇒ fallback）由 CivicGlbPiece
 *     内置；原程序化几何保留为 fallback，改本地坐标（position 统一由外层持有）
 *   - litMaterials 昼夜调制：钟面 CityHall_Clock 0.6 / 门灯 CityHall_Lantern 1.6
 *     （材质名 includes 匹配 GLB 的 CityHall_Clock_Mat / CityHall_Lantern_Mat）
 */
import { u, worldDims, buildingHeight } from '../cityScale';
import { CivicGlbPiece } from './CivicGlb';

const STONE = '#a8a4a0';
const STONE_DARK = '#7a7a76';
const ROOF_DARK = '#3a3f4a';
const GOLD = '#d4a017';

/**
 * 市政厅布点（世界坐标）：主楼/模型 group 原点。
 * 批次 23 起导出为市民之声气泡锚点基准（CityVoiceBubbleLayer），
 * CityHall 本体（CivicGlbPiece position / fallback）同用一份，防止坐标漂移。
 */
export const CITY_HALL_POSITION = { x: 0, z: 36 } as const;

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
/** 夜间自发光（CivicGlbPiece 昼夜调制，材质名 includes 匹配 GLB 的 *_Mat）：钟面 0.6 / 门灯 1.6。 */
const LIT = {
  CityHall_Clock: 0.6,
  CityHall_Lantern: 1.6,
};

/** 市民之声气泡锚点：钟楼尖顶（= 表值总高 20.0 m）再上浮 0.6，悬浮于市政厅上空。 */
export const CITY_HALL_BUBBLE_ANCHOR: [number, number, number] = [
  CITY_HALL_POSITION.x,
  HALL.y + 0.6,
  CITY_HALL_POSITION.z,
];

/** 全局开关：测试/回滚用，缺省 false = 用 .glb */
export function CityHall() {
  return (
    <CivicGlbPiece
      glbName="city_hall"
      dimsKey="cityHall"
      infoId="civic.city-hall"
      anchorY={5}
      position={[CITY_HALL_POSITION.x, 0, CITY_HALL_POSITION.z]}
      litMaterials={LIT}
      fallback={<CityHallFallback />}
    />
  );
}

/**
 * 保留原程序化几何作为 fallback（19-Blender3D模型集成 · 02 架构设计 §5.2 降级链）。
 * 批次 90：本地坐标（原根组 position 改由 CivicGlbPiece 外层持有，几何零改动）。
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
    <group>
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