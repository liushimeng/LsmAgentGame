/**
 * Pedestrian — 2.5D 街道行人（14-3D城市渲染深化 · 阶段 I 漫步改造）：
 *
 * 简化几何（缺失贴图）：小圆 + 主色（warm/cool 双调色板）。
 * 加载 props/pedestrian/<variant>_pedestrian.png 后：Billboard 朝相机 sprite。
 *
 * 漫步：useFrame 内沿城区小环线（圆心 = x,z，半径 pathR）慢速行走，
 * 轻微上下浮动模拟步态；prefers-reduced-motion 下固定原点不动（不再微抖）。
 *
 * 2026-09-21 高度系统：行人总高 0.36 → 0.17（真实 1.7m，见 cityScale.ts），
 * Billboard 平面与圆形兜底同步缩放。
 */

import { useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { Billboard } from '@react-three/drei';
import { propUrl } from '@/assets/images/virtualCity';
import { useSharedTexture } from '../textureCache';

type PedestrianVariant = 'warm' | 'cool';

interface Props {
  /** 漫步环线圆心 x。 */
  x: number;
  /** 漫步环线圆心 z。 */
  z: number;
  variant?: PedestrianVariant;
  /** 起始相位（决定环上初始角度与步态错相）。 */
  seed?: number;
  /** 环线半径（世界单位，默认 1.5；契约 02 §6）。 */
  pathR?: number;
}

const COLORS: Record<PedestrianVariant, string> = {
  warm: '#c8a888',
  cool: '#8ea4be',
};

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/** 环线行走角速度（rad/s，慢速漫步）。 */
const WALK_SPEED = 0.12;

export function Pedestrian({ x, z, variant = 'warm', seed = 0, pathR = 1.5 }: Props) {
  const tex = useSharedTexture(propUrl('pedestrian', variant));
  const groupRef = useRef<THREE.Group>(null);
  const angleRef = useRef(seed * 0.7);

  // 初始落位（环上 seed 角度）
  const start = useMemo(() => {
    const a = angleRef.current;
    return [x + Math.cos(a) * pathR, z + Math.sin(a) * pathR] as [number, number];
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [x, z, pathR]);

  useFrame((_state, delta) => {
    const g = groupRef.current;
    if (!g) return;
    if (REDUCED_MOTION) {
      g.position.x = x;
      g.position.z = z;
      g.position.y = 0;
      return;
    }
    angleRef.current += delta * WALK_SPEED;
    const a = angleRef.current;
    g.position.x = x + Math.cos(a) * pathR;
    g.position.z = z + Math.sin(a) * pathR;
    // 步态上下浮动 0.005
    g.position.y = Math.sin(a * 8) * 0.005;
  });

  return (
    <group ref={groupRef} position={[start[0], 0, start[1]]}>
      {tex ? (
        <Billboard position={[0, 0.085, 0]}>
          <mesh>
            <planeGeometry args={[0.085, 0.17]} />
            <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
          </mesh>
        </Billboard>
      ) : (
        // 缺失贴图：圆片兜底
        <Billboard position={[0, 0.085, 0]}>
          <mesh>
            <circleGeometry args={[0.04, 12]} />
            <meshBasicMaterial color={COLORS[variant]} />
          </mesh>
        </Billboard>
      )}
    </group>
  );
}
