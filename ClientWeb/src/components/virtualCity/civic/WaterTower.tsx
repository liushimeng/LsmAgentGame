/**
 * 水塔（18-AA · §5.2 WaterTower）
 *   锥形支架 + 罐体 + 检修梯 + 字样带。布点 (-22, 2)。
 *
 * 19-Blender3D模型集成：用 <Model url={...}> 包一层，原程序化几何保留为 children fallback。
 */
import { u } from '../cityScale';
import { Model } from '../Model';
import { modelUrl } from '@/assets/models';

const TANK_GREEN = '#5a7a4e';
const TANK_LIGHT = '#7a9a6e';
const TANK_DARK = '#3a5240';

function blenderEnabled(): boolean {
  return typeof window === 'undefined' ||
    window.localStorage.getItem('disable-blender-models') !== '1';
}

export function WaterTower() {
  const url = modelUrl('civic', 'water_tower');
  if (!url || !blenderEnabled()) {
    return <WaterTowerFallback />;
  }
  return (
    <Model url={url} position={[-22, 0, 2]} castShadow>
      <WaterTowerFallback />
    </Model>
  );
}

/** 程序化几何 fallback（19-Blender3D模型集成 · 02 架构设计 §5.2 降级链）。 */
function WaterTowerFallback() {
  return (
    <group position={[-22, 0, 2]}>
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
      {/* 罐体：球形储水罐 + 顶盖 */}
      <mesh position={[0, u(11), 0]} castShadow>
        <sphereGeometry args={[u(1.8), 16, 12]} />
        <meshStandardMaterial color={TANK_GREEN} roughness={0.7} metalness={0.2} />
      </mesh>
      {/* 罐顶小圆盖 */}
      <mesh position={[0, u(13), 0]} castShadow>
        <cylinderGeometry args={[u(0.4), u(0.5), u(0.3), 12]} />
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
      {[2, 3, 4, 5, 6, 7].map((y) => (
        <mesh key={`rung-${y}`} position={[u(1.82), y, 0]} rotation={[Math.PI / 2, 0, 0]}>
          <cylinderGeometry args={[u(0.025), u(0.025), u(0.14), 6]} />
          <meshStandardMaterial color="#5a6270" />
        </mesh>
      ))}
    </group>
  );
}