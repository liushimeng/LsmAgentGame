/**
 * Pedestrian — 2.5D 街道行人（P1-C）：
 *
 * 简化几何（缺失贴图）：小圆 + 主色（warm/cool 双调色板）。
 * 加载 props/pedestrian/<variant>_pedestrian.png 后：Billboard 朝相机 sprite。
 *
 * 微抖动（useFrame 内 sin 时间偏移）让行人看起来在原地等待（不是真正走动，
 * 但避免视觉静止的"贴纸感"）。prefers-reduced-motion 下静止。
 */

import { useEffect, useRef, useState } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { Billboard } from '@react-three/drei';
import { propUrl } from '@/assets/images/wealth';

type PedestrianVariant = 'warm' | 'cool';

interface Props {
  x: number;
  z: number;
  variant?: PedestrianVariant;
  /** 用于 stagger 微抖动相位。 */
  seed?: number;
}

const COLORS: Record<PedestrianVariant, string> = {
  warm: '#c8a888',
  cool: '#8ea4be',
};

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

export function Pedestrian({ x, z, variant = 'warm', seed = 0 }: Props) {
  const url = propUrl('pedestrian', variant);
  const [tex, setTex] = useState<THREE.Texture | null>(null);
  const groupRef = useRef<THREE.Group>(null);
  const timeOffset = useRef(seed * 0.7);

  useEffect(() => {
    if (!url) {
      setTex(null);
      return;
    }
    let disposed = false;
    const loader = new THREE.TextureLoader();
    loader.load(
      url,
      (loaded) => {
        if (disposed) {
          loaded.dispose();
          return;
        }
        loaded.colorSpace = THREE.SRGBColorSpace;
        loaded.magFilter = THREE.LinearFilter;
        loaded.minFilter = THREE.LinearMipmapLinearFilter;
        setTex(prev => {
          if (prev) prev.dispose();
          return loaded;
        });
      },
      undefined,
      () => setTex(null),
    );
    return () => {
      disposed = true;
    };
  }, [url]);

  useFrame((_state, delta) => {
    const g = groupRef.current;
    if (!g) return;
    if (REDUCED_MOTION) return;
    timeOffset.current += delta;
    // 微抖动：± 0.02 单位的横向位移 + 上下浮动 0.005
    const t = timeOffset.current;
    g.position.x = x + Math.sin(t * 2.0) * 0.02;
    g.position.z = z + Math.cos(t * 1.7) * 0.02;
    g.position.y = 0;
  });

  return (
    <group ref={groupRef} position={[x, 0, z]}>
      {tex ? (
        <Billboard position={[0, 0.18, 0]}>
          <mesh>
            <planeGeometry args={[0.18, 0.36]} />
            <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
          </mesh>
        </Billboard>
      ) : (
        // 缺失贴图：圆片兜底
        <Billboard position={[0, 0.18, 0]}>
          <mesh>
            <circleGeometry args={[0.08, 12]} />
            <meshBasicMaterial color={COLORS[variant]} />
          </mesh>
        </Billboard>
      )}
    </group>
  );
}