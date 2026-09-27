/**
 * 通讯塔（18-AA · §5.2 CommTower）
 *   格构塔（4 柱 + 6 层横撑）+ 微波板 3 面 + 航空障碍灯。布点 (-6, 10)。
 *
 * 19-Blender3D模型集成：用 <Model url={...}> 包一层，原程序化几何保留为 children fallback。
 */
import { u, worldDims, sizeTargetFor } from '../cityScale';
import { Model, blenderModelsEnabled as blenderEnabled } from '@/engine3d';
import { modelUrl } from '@/assets/models';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const TOWER_DARK = '#5a6270';
const PANEL_WHITE = '#e8e8e8';
const LIGHT_RED = '#ff3b30';

/**
 * 塔尺寸（批次 29）：取 cityScale.REAL_DIMS_M.commTower = 10.0 × 10.0（占地）× 20.5（总高）。
 * 占地由**塔基垫脚外缘**定义（= 3d_script/build_comm_tower.py 的 FOOT=0.50 ⇒ 10 m），
 * 4 角立柱间距随之为 FOOT − 0.6 m（= 该脚本 LEG_X=0.44）。
 * 修正前 fallback 的格构仅 ≈1 m 见方（与 GLB/表值差 8.8 倍），且微波板落在塔基高度
 * （位置向量裸 `0` 的旧遗留），现一并按表与脚本对齐。
 */
const TOWER = worldDims('commTower');
/** 塔基外缘半宽（表值 10.0 / 2）。 */
const FOOT = TOWER.x / 2;
/** 塔基垫脚边长（1.6 m，同 build_comm_tower.py）。 */
const PAD = u(1.6);
/** 4 角立柱中心距（4.4 m = FOOT − 0.6）。 */
const LEG_OFF = FOOT - u(0.6);
/** 4 角立柱高（= 塔身，20 m）。 */
const COLUMN_H = u(20);
/** 顶端机舱 / 障碍灯 / 微波板尺寸与层高（构图常量，同 art 脚本量级）。 */
const CABIN_H = u(0.6);
const LIGHT_R = u(0.1);
const DISH_S = u(3);
const DISH_Z = u(16);
/** GLB 尺寸/落地校验目标（dev 态）。 */
const COMM_TOWER_SIZE_TARGET = sizeTargetFor('commTower', { label: 'civic/comm_tower' });

export function CommTower() {
  const url = modelUrl('civic', 'comm_tower');
  // 批次 28 B2：通讯塔信息交互。
  const info = useObjectInfoProps('civic.comm-tower', { anchorY: 6 });
  if (!url || !blenderEnabled()) {
    return (
      <group {...info}>
        <CommTowerFallback />
      </group>
    );
  }
  return (
    <group {...info}>
      <Model url={url} sizeTarget={COMM_TOWER_SIZE_TARGET} position={[-6, 0, 10]} castShadow>
        <CommTowerFallback />
      </Model>
    </group>
  );
}

/** 程序化几何 fallback（19-Blender3D模型集成 · 02 架构设计 §5.2 降级链）。 */
function CommTowerFallback() {
  return (
    <group position={[-6, 0, 10]}>
      {/* 塔基垫脚 ×4（外缘 = ±FOOT ⇒ 定义表值占地 10×10） */}
      {[-1, 1].map((sx) =>
        [-1, 1].map((sz) => (
          <mesh
            key={`pad-${sx}-${sz}`}
            position={[sx * (FOOT - PAD / 2), PAD / 2, sz * (FOOT - PAD / 2)]}
            castShadow
          >
            <boxGeometry args={[PAD, PAD, PAD * 0.25]} />
            <meshStandardMaterial color={TOWER_DARK} roughness={0.9} metalness={0.2} />
          </mesh>
        )),
      )}
      {/* 4 角立柱（高 = 塔身；脚底 y=0 起） */}
      {[-1, 1].map((sx) =>
        [-1, 1].map((sz) => (
          <mesh
            key={`col-${sx}-${sz}`}
            position={[sx * LEG_OFF, COLUMN_H / 2, sz * LEG_OFF]}
            castShadow
          >
            <cylinderGeometry args={[u(0.06), u(0.08), COLUMN_H, 6]} />
            <meshStandardMaterial color={TOWER_DARK} metalness={0.6} roughness={0.5} />
          </mesh>
        )),
      )}
      {/* 6 层横撑（沿塔身等分：y = 塔身 × k/7，k=1..6）：4 根圆柱拼一圈，跨 2×LEG_OFF。
          批次 29 修正：原为裸世界单位 y=[2,5,8,11,14,17]（= 20~170 m，把 fallback
          包围盒撑到 170 m 高），现按塔身高度等分。 */}
      {Array.from({ length: 6 }, (_, i) => (COLUMN_H * (i + 1)) / 7).map((y) => (
        <group key={`tie-${y}`}>
          {[-1, 1].map((sx) => (
            <mesh
              key={`tie-x-${y}-${sx}`}
              position={[0, y, sx * LEG_OFF]}
              rotation={[0, 0, Math.PI / 2]}
            >
              <cylinderGeometry args={[u(0.04), u(0.04), LEG_OFF * 2, 6]} />
              <meshStandardMaterial color={TOWER_DARK} metalness={0.6} roughness={0.5} />
            </mesh>
          ))}
          {[-1, 1].map((sz) => (
            <mesh
              key={`tie-z-${y}-${sz}`}
              position={[sz * LEG_OFF, y, 0]}
              rotation={[Math.PI / 2, 0, 0]}
            >
              <cylinderGeometry args={[u(0.04), u(0.04), LEG_OFF * 2, 6]} />
              <meshStandardMaterial color={TOWER_DARK} metalness={0.6} roughness={0.5} />
            </mesh>
          ))}
        </group>
      ))}
      {/* 顶端机舱 */}
      <mesh position={[0, COLUMN_H, 0]} castShadow>
        <boxGeometry args={[CABIN_H, CABIN_H, u(1.2)]} />
        <meshStandardMaterial color={TOWER_DARK} metalness={0.5} roughness={0.5} />
      </mesh>
      {/* 微波板 3 面（塔身外缘 ±FOOT 内，高度 DISH_Z；修正前落在塔基高度） */}
      {[
        { rot: [0, 0, 0], off: [FOOT - u(0.1), DISH_Z, 0] },
        { rot: [0, 0, 0], off: [-(FOOT - u(0.1)), DISH_Z, 0] },
        { rot: [0, Math.PI / 2, 0], off: [0, DISH_Z, FOOT - u(0.1)] },
      ].map((p, i) => (
        <mesh key={`panel-${i}`} position={p.off as any} rotation={p.rot as any}>
          <boxGeometry args={[u(0.2), DISH_S, DISH_S]} />
          <meshStandardMaterial color={PANEL_WHITE} roughness={0.5} metalness={0.3} />
        </mesh>
      ))}
      {/* 航空障碍灯（emissive 红，2 只）：顶灯顶面 = 表值总高 20.5 m */}
      <mesh position={[0, TOWER.y - LIGHT_R, 0]}>
        <sphereGeometry args={[LIGHT_R, 8, 8]} />
        <meshStandardMaterial color={LIGHT_RED} emissive={LIGHT_RED} emissiveIntensity={0.9} />
      </mesh>
      <mesh position={[LEG_OFF, u(12), 0]}>
        <sphereGeometry args={[u(0.08), 8, 8]} />
        <meshStandardMaterial color={LIGHT_RED} emissive={LIGHT_RED} emissiveIntensity={0.7} />
      </mesh>
    </group>
  );
}