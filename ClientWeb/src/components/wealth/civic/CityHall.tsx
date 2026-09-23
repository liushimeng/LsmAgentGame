/**
 * 市政厅（18-AA · §5.2 CityHall）
 *   3 层石材体量 + 门廊 4 柱 + 钟楼（4 面钟）+ 旗杆。布点 (-9.0, -1.7)。
 *
 * 19-Blender3D模型集成：用 <Model url={...}> 包一层，原程序化几何保留为 children fallback。
 *   - modelUrl 返回 '' → 走 fallback（程序化几何，与原行为像素一致）
 *   - .glb 加载成功 → 渲染真实模型，children 不渲染
 *   - 切换开关：localStorage.getItem('disable-blender-models') === '1' 强制 fallback
 */
import { u } from '../cityScale';
import { Model } from '../Model';
import { modelUrl } from '@/assets/models';

const STONE = '#a8a4a0';
const STONE_DARK = '#7a7a76';
const ROOF_DARK = '#3a3f4a';
const GOLD = '#d4a017';

/** 全局开关：测试/回滚用，缺省 false = 用 .glb */
function blenderEnabled(): boolean {
  return typeof window === 'undefined' ||
    window.localStorage.getItem('disable-blender-models') !== '1';
}

export function CityHall() {
  const url = modelUrl('civic', 'city_hall');
  // url 缺失或全局开关关闭 → 直接渲染原程序化几何
  if (!url || !blenderEnabled()) {
    return <CityHallFallback />;
  }
  return (
    <Model url={url} position={[-9.0, 0, -1.7]} castShadow receiveShadow>
      <CityHallFallback />
    </Model>
  );
}

/** 保留原程序化几何作为 fallback（19-Blender3D模型集成 · 02 架构设计 §5.2 降级链）。 */
function CityHallFallback() {
  return (
    <group position={[-9.0, 0, -1.7]}>
      {/* 主楼：u(14) × u(9) × u(8) */}
      <mesh position={[0, u(4.5), 0]} castShadow receiveShadow>
        <boxGeometry args={[u(14), u(9), u(8)]} />
        <meshStandardMaterial color={STONE} roughness={0.9} />
      </mesh>
      {/* 屋顶（坡顶伪装用扁顶） */}
      <mesh position={[0, u(9.1), 0]} castShadow>
        <boxGeometry args={[u(14.4), u(0.4), u(8.4)]} />
        <meshStandardMaterial color={ROOF_DARK} />
      </mesh>
      {/* 楼层水平线（3 道） */}
      {[3, 6].map((y, i) => (
        <mesh key={`floor-${i}`} position={[0, y, u(4.05)]}>
          <boxGeometry args={[u(14.1), u(0.08), u(0.05)]} />
          <meshStandardMaterial color={STONE_DARK} />
        </mesh>
      ))}
      {/* 门廊：4 立柱 + 雨棚 + 台阶 */}
      {[-u(4.5), -u(1.5), u(1.5), u(4.5)].map((dx, i) => (
        <mesh key={`porch-col-${i}`} position={[dx, u(4.5), u(5)]} castShadow>
          <cylinderGeometry args={[u(0.3), u(0.3), u(9), 8]} />
          <meshStandardMaterial color={STONE_DARK} roughness={0.9} />
        </mesh>
      ))}
      <mesh position={[0, u(9.3), u(5.4)]} castShadow>
        <boxGeometry args={[u(12), u(0.4), u(2)]} />
        <meshStandardMaterial color={STONE_DARK} />
      </mesh>
      {/* 台阶 3 级 */}
      {[0, 1, 2].map((i) => (
        <mesh key={`step-${i}`} position={[0, 0.15 + i * u(0.3), u(7 + i * 0.3)]}>
          <boxGeometry args={[u(8 - i * 0.6), u(0.3), u(0.6)]} />
          <meshStandardMaterial color={STONE_DARK} />
        </mesh>
      ))}
      {/* 钟楼：方塔 + 4 面钟 */}
      <mesh position={[0, u(13), 0]} castShadow>
        <boxGeometry args={[u(3.2), u(7), u(3.2)]} />
        <meshStandardMaterial color={STONE_DARK} roughness={0.9} />
      </mesh>
      {[
        { rot: [0, 0, 0], off: [0, u(13), u(1.62)] },
        { rot: [0, Math.PI / 2, 0], off: [u(1.62), u(13), 0] },
        { rot: [0, Math.PI, 0], off: [0, u(13), -u(1.62)] },
        { rot: [0, -Math.PI / 2, 0], off: [-u(1.62), u(13), 0] },
      ].map((p, i) => (
        <mesh key={`clock-${i}`} position={p.off as any} rotation={p.rot as any}>
          <circleGeometry args={[u(0.6), 24]} />
          <meshStandardMaterial color={GOLD} emissive={GOLD} emissiveIntensity={0.5} />
        </mesh>
      ))}
      {/* 钟塔尖顶 */}
      <mesh position={[0, u(17), 0]} castShadow>
        <coneGeometry args={[u(2), u(3), 4]} />
        <meshStandardMaterial color={ROOF_DARK} />
      </mesh>
      {/* 旗杆 + 旗（简化 box） */}
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