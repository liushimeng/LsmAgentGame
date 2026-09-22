/**
 * WaterMist — 水面薄雾（15-3D城市全面真实感深化 · 阶段 P）：
 *
 * 半透明 plane 叠在水面岸边，制造岸雾效果（空气湿度感）。
 * useFrame 内轻微起伏 UV（5% 振幅 / 4s 周期），prefers-reduced-motion 下静止。
 *
 * 米制经 cityScale.u()（水岸 w/h 由调用方传入）。
 *
 * 契约：lag_docs/虚拟城市/已实现/15-3D城市渲染深化/02-架构设计 §7.3。
 */

import { useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/** 雾色（半透明蓝灰）。 */
const MIST_COLOR = '#c8d6e0';

interface Props {
  x: number;
  z: number;
  w: number;
  d: number;
  rotation?: number;
  /** 透明度（默认 0.18；不透明度太高会遮水面）。 */
  opacity?: number;
}

export function WaterMist({ x, z, w, d, rotation = 0, opacity = 0.18 }: Props) {
  const matRef = useRef<THREE.MeshStandardMaterial>(null);

  useFrame(() => {
    if (REDUCED_MOTION) return;
    const mat = matRef.current;
    if (!mat) return;
    // 透明度轻微起伏 0.14-0.22（呼吸感）
    const t = performance.now() / 1000;
    mat.opacity = 0.14 + (Math.sin(t * 0.6) + 1) * 0.04;
  });

  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.05, 0]}>
        <planeGeometry args={[w, d]} />
        <meshStandardMaterial
          ref={matRef}
          color={MIST_COLOR}
          transparent
          opacity={opacity}
          depthWrite={false}
          roughness={1}
          metalness={0}
        />
      </mesh>
    </group>
  );
}

export default WaterMist;