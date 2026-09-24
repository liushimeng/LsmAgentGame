/**
 * 通讯塔（18-AA · §5.2 CommTower）
 *   格构塔（4 柱 + 6 层横撑）+ 微波板 3 面 + 航空障碍灯。布点 (-6, 10)。
 *
 * 19-Blender3D模型集成：用 <Model url={...}> 包一层，原程序化几何保留为 children fallback。
 */
import { u } from '../cityScale';
import { Model } from '../Model';
import { modelUrl } from '@/assets/models';

const TOWER_DARK = '#5a6270';
const PANEL_WHITE = '#e8e8e8';
const LIGHT_RED = '#ff3b30';

function blenderEnabled(): boolean {
  return typeof window === 'undefined' ||
    window.localStorage.getItem('disable-blender-models') !== '1';
}

export function CommTower() {
  const url = modelUrl('civic', 'comm_tower');
  if (!url || !blenderEnabled()) {
    return <CommTowerFallback />;
  }
  return (
    <Model url={url} position={[-6, 0, 10]} castShadow>
      <CommTowerFallback />
    </Model>
  );
}

/** 程序化几何 fallback（19-Blender3D模型集成 · 02 架构设计 §5.2 降级链）。 */
function CommTowerFallback() {
  return (
    <group position={[-6, 0, 10]}>
      {/* 4 角立柱 */}
      {[-1, 1].map((sx) =>
        [-1, 1].map((sz) => (
          <mesh
            key={`col-${sx}-${sz}`}
            position={[sx * u(0.5), u(10), sz * u(0.5)]}
            castShadow
          >
            <cylinderGeometry args={[u(0.06), u(0.08), u(20), 6]} />
            <meshStandardMaterial color={TOWER_DARK} metalness={0.6} roughness={0.5} />
          </mesh>
        )),
      )}
      {/* 6 层横撑（每 u(33) 高一道矩形环）：4 根 box 拼一圈 */}
      {[2, 5, 8, 11, 14, 17].map((y) => (
        <group key={`tie-${y}`}>
          {[-1, 1].map((sx) => (
            <mesh
              key={`tie-x-${y}-${sx}`}
              position={[sx * u(0.5), y, 0]}
              rotation={[0, 0, Math.PI / 2]}
            >
              <cylinderGeometry args={[u(0.04), u(0.04), u(1.1), 6]} />
              <meshStandardMaterial color={TOWER_DARK} metalness={0.6} roughness={0.5} />
            </mesh>
          ))}
          {[-1, 1].map((sz) => (
            <mesh
              key={`tie-z-${y}-${sz}`}
              position={[0, y, sz * u(0.5)]}
              rotation={[Math.PI / 2, 0, 0]}
            >
              <cylinderGeometry args={[u(0.04), u(0.04), u(1.1), 6]} />
              <meshStandardMaterial color={TOWER_DARK} metalness={0.6} roughness={0.5} />
            </mesh>
          ))}
        </group>
      ))}
      {/* 顶端机舱 */}
      <mesh position={[0, u(20), 0]} castShadow>
        <boxGeometry args={[u(0.6), u(0.6), u(1.2)]} />
        <meshStandardMaterial color={TOWER_DARK} metalness={0.5} roughness={0.5} />
      </mesh>
      {/* 微波板 3 面：朝向 x+/y+/z+ */}
      {[
        { rot: [0, Math.PI / 2, 0], off: [u(0.5), 0, 0] },
        { rot: [0, Math.PI, 0], off: [0, 0, u(0.5)] },
        { rot: [0, -Math.PI / 2, 0], off: [-u(0.5), 0, 0] },
      ].map((p, i) => (
        <mesh key={`panel-${i}`} position={p.off as any} rotation={p.rot as any}>
          <boxGeometry args={[u(0.05), u(1.2), u(1.5)]} />
          <meshStandardMaterial color={PANEL_WHITE} roughness={0.5} metalness={0.3} />
        </mesh>
      ))}
      {/* 航空障碍灯（emissive 红，2 只） */}
      <mesh position={[0, u(20.5), 0]}>
        <sphereGeometry args={[u(0.1), 8, 8]} />
        <meshStandardMaterial color={LIGHT_RED} emissive={LIGHT_RED} emissiveIntensity={0.9} />
      </mesh>
      <mesh position={[0, u(10.5), 0]}>
        <sphereGeometry args={[u(0.08), 8, 8]} />
        <meshStandardMaterial color={LIGHT_RED} emissive={LIGHT_RED} emissiveIntensity={0.7} />
      </mesh>
    </group>
  );
}