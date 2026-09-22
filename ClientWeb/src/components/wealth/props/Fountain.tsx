/**
 * Fountain — 中央公园喷泉（16-3D城市WebGL质感与城市补全 · 阶段 T）：
 *
 * 双层圆池（石色）+ 内水盘（water_tile）+ 中心柱 + 顶碗 + 水花 Sparkles。
 * reduced-motion 时不渲染 Sparkles（drei 内部动画）。
 *
 * 契约：lag_docs/虚拟城市/已实现/16-3D城市WebGL质感与城市补全/02-架构设计 §8.1。
 */

import { Sparkles } from '@react-three/drei';
import { groundTileUrl } from '@/assets/images/wealth';
import { u } from '../cityScale';
import { useSharedTexture } from '../textureCache';

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

const STONE = '#9aa1ab';
const STONE_DARK = '#7c838d';

interface Props {
  x: number;
  z: number;
}

export function Fountain({ x, z }: Props) {
  const water = useSharedTexture(groundTileUrl('water_tile'), {
    wrap: 'repeat',
    repeat: [2, 2],
  });

  return (
    <group position={[x, 0, z]}>
      {/* 外池壁（矮圆环壁：用圆柱 + 内水盘遮盖顶面形成池感） */}
      <mesh position={[0, u(0.25), 0]} castShadow>
        <cylinderGeometry args={[u(2.2), u(2.3), u(0.5), 20]} />
        <meshStandardMaterial color={STONE} roughness={0.8} envMapIntensity={0.4} />
      </mesh>
      {/* 池沿（torus 压扁） */}
      <mesh position={[0, u(0.5), 0]} rotation={[Math.PI / 2, 0, 0]}>
        <torusGeometry args={[u(2.2), u(0.07), 8, 24]} />
        <meshStandardMaterial color={STONE_DARK} roughness={0.75} />
      </mesh>
      {/* 内水盘 */}
      <mesh position={[0, u(0.42), 0]} rotation={[-Math.PI / 2, 0, 0]}>
        <circleGeometry args={[u(2.05), 24]} />
        <meshStandardMaterial
          map={water ?? undefined}
          color={water ? '#ffffff' : '#1a3a52'}
          roughness={0.15}
          metalness={0.35}
          envMapIntensity={1.0}
        />
      </mesh>
      {/* 中心柱 */}
      <mesh position={[0, u(0.85), 0]} castShadow>
        <cylinderGeometry args={[u(0.12), u(0.16), u(0.9), 10]} />
        <meshStandardMaterial color={STONE} roughness={0.75} />
      </mesh>
      {/* 顶碗 */}
      <mesh position={[0, u(1.32), 0]} castShadow>
        <cylinderGeometry args={[u(0.38), u(0.22), u(0.12), 14]} />
        <meshStandardMaterial color={STONE_DARK} roughness={0.7} />
      </mesh>
      {/* 水花粒子（静止偏好时不渲染） */}
      {!REDUCED_MOTION && (
        <Sparkles
          position={[0, u(1.5), 0]}
          count={10}
          scale={[0.8, 0.8, 0.8]}
          size={1.6}
          speed={0.4}
          color="#bfe3ff"
          opacity={0.85}
        />
      )}
    </group>
  );
}

export default Fountain;
