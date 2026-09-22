/**
 * WaterPlane — 水面（14-3D城市渲染深化 · 阶段 I）：
 *
 * 城市运河 / 物流港港池共用。water_tile.png RepeatWrapping，useFrame 内
 * map.offset.y 滚动模拟流动；prefers-reduced-motion 下静止。
 * 贴图缺失 → 纯色 #1a3a52（§9 降级）。
 *
 * 契约：lag_docs/虚拟城市/已实现/14-3D城市渲染深化/02-架构设计 §3.1。
 */

import { useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { groundTileUrl } from '@/assets/images/wealth';
import { useSharedTexture } from '../textureCache';

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/** 水面纯色兜底（深青蓝）。 */
const WATER_FALLBACK = '#1a3a52';
/** 水流速度（uv 单位/秒）。 */
const FLOW_SPEED = 0.02;

interface Props {
  /** 水体中心世界坐标。 */
  x: number;
  z: number;
  /** 世界单位尺寸。 */
  w: number;
  d: number;
  /** 绕 Y 旋转（默认 0）。 */
  rotation?: number;
}

export function WaterPlane({ x, z, w, d, rotation = 0 }: Props) {
  // 贴图 key 按 repeat 区分（不同尺寸水体各自平铺密度）；均走共享缓存
  const tex = useSharedTexture(groundTileUrl('water_tile'), {
    wrap: 'repeat',
    repeat: [Math.max(1, Math.round(w / 2)), Math.max(1, Math.round(d / 2))],
  });
  const matRef = useRef<THREE.MeshStandardMaterial>(null);

  useFrame((_state, delta) => {
    if (REDUCED_MOTION) return;
    const map = matRef.current?.map;
    if (map) map.offset.y = (map.offset.y + delta * FLOW_SPEED) % 1;
  });

  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* 水面（y=0.028：高于地面 0.02 / plaza 0.025，低于 curb 顶） */}
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.028, 0]} receiveShadow>
        <planeGeometry args={[w, d]} />
        <meshStandardMaterial
          ref={matRef}
          map={tex ?? undefined}
          color={tex ? '#ffffff' : WATER_FALLBACK}
          roughness={0.15}
          metalness={0.35}
        />
      </mesh>
    </group>
  );
}
