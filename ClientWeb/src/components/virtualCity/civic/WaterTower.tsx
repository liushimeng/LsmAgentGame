/**
 * 水塔（18-AA · §5.2 WaterTower）
 *   锥形支架 + 罐体 + 检修梯 + 字样带。布点 (-22, 2)。
 *
 * 批次 90 昼夜发光：从裸 <Model> 迁到 <CivicGlbPiece>（范式同 GasStation）；
 * litMaterials 罐顶障碍灯 WaterTower_Obstruction 2.0（材质名 includes 匹配 *_Mat）。
 * 降级链由 CivicGlbPiece 内置；原程序化几何保留为 fallback（本地坐标，外层持有 position）。
 */
import { u, worldDims } from '../cityScale';
import { CivicGlbPiece } from './CivicGlb';

const TANK_GREEN = '#5a7a4e';
const TANK_LIGHT = '#7a9a6e';
const TANK_DARK = '#3a5240';

/**
 * 水塔尺寸（批次 29）：取 cityScale.REAL_DIMS_M.waterTower = ⌀3.60（占地）× 13.3（总高）。
 * 罐体球半径 = 表值 ⌀/2（3.6 m ⇒ ⌀ 恰好 3.60）；罐顶圆盖顶面 = 表值总高 13.3 m。
 */
const TOWER = worldDims('waterTower');
const TANK_R = TOWER.x / 2;
/** 罐顶圆盖（厚 0.3 m）——顶面接总高。 */
const CAP_H = u(0.3);
/** 夜间自发光（CivicGlbPiece 昼夜调制）：罐顶障碍灯 2.0（WaterTower_Obstruction_Mat）。 */
const LIT = {
  WaterTower_Obstruction: 2.0,
};

export function WaterTower() {
  return (
    <CivicGlbPiece
      glbName="water_tower"
      dimsKey="waterTower"
      infoId="civic.water-tower"
      anchorY={5}
      position={[36, 0, -12]}
      litMaterials={LIT}
      fallback={<WaterTowerFallback />}
    />
  );
}

/** 程序化几何 fallback（19-Blender3D模型集成 · 02 架构设计 §5.2 降级链）。
 * 批次 90：本地坐标（原根组 position 改由 CivicGlbPiece 外层持有，几何零改动）。 */
function WaterTowerFallback() {
  return (
    <group>
      {/* 锥形支架：4 斜腿 + 顶部圈梁 */}
      {[-1, 1].map((sx) =>
        [-1, 1].map((sz) => (
          <mesh
            key={`leg-${sx}-${sz}`}
            position={[sx * u(0.7), u(4), sz * u(0.7)]}
            rotation={[0, Math.PI / 4 + (sx * sz > 0 ? 0 : Math.PI / 2), sx > 0 ? -0.15 : 0.15]}
            castShadow
          >
            <cylinderGeometry args={[u(0.1), u(0.15), u(8), 6]} />
            <meshStandardMaterial color={TANK_DARK} roughness={0.7} />
          </mesh>
        )),
      )}
      {/* 罐体：球形储水罐（半径 = 表值 ⌀/2 ⇒ 占地恰为 3.60 × 3.60） */}
      <mesh position={[0, u(11), 0]} castShadow>
        <sphereGeometry args={[TANK_R, 16, 12]} />
        <meshStandardMaterial color={TANK_GREEN} roughness={0.7} metalness={0.2} />
      </mesh>
      {/* 罐顶小圆盖：顶面 = 表值总高 13.3 m */}
      <mesh position={[0, TOWER.y - CAP_H / 2, 0]} castShadow>
        <cylinderGeometry args={[u(0.4), u(0.5), CAP_H, 12]} />
        <meshStandardMaterial color={TANK_DARK} roughness={0.7} />
      </mesh>
      {/* 字样带：浅色环带 */}
      <mesh position={[0, u(11.5), 0]}>
        <torusGeometry args={[u(1.82), u(0.15), 8, 24]} />
        <meshStandardMaterial color={TANK_LIGHT} roughness={0.7} />
      </mesh>
      {/* 检修梯：前侧 2 竖 + 6 横 */}
      <mesh position={[u(1.85), u(6), 0]}>
        <cylinderGeometry args={[u(0.04), u(0.04), u(8), 6]} />
        <meshStandardMaterial color="#5a6270" metalness={0.5} roughness={0.5} />
      </mesh>
      <mesh position={[u(1.78), u(6), 0]}>
        <cylinderGeometry args={[u(0.04), u(0.04), u(8), 6]} />
        <meshStandardMaterial color="#5a6270" metalness={0.5} roughness={0.5} />
      </mesh>
      {/* 批次 30 P0-1：横档 y 原为裸数字 2..7（世界单位 ⇒ 20~70 m 高空，包围盒 Y 超表值
          5.3×）——与 CommTower.tsx 同类缺陷，经 u(y) 归一到米制（横档 2~7 m 沿梯）。 */}
      {[2, 3, 4, 5, 6, 7].map((y) => (
        <mesh key={`rung-${y}`} position={[u(1.82), u(y), 0]} rotation={[Math.PI / 2, 0, 0]}>
          <cylinderGeometry args={[u(0.025), u(0.025), u(0.14), 6]} />
          <meshStandardMaterial color="#5a6270" />
        </mesh>
      ))}
    </group>
  );
}