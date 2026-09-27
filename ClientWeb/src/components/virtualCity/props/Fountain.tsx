/**
 * Fountain — 中央公园喷泉（16-3D城市WebGL质感与城市补全 · 阶段 T）：
 *
 * 双层圆池（石色）+ 内水盘（water_tile）+ 中心柱 + 顶碗 + 水花 Sparkles。
 * reduced-motion 时不渲染 Sparkles（drei 内部动画）。
 *
 * 批次 28 二轮（DC 攻坚）：5 mesh → 2 mesh + Sparkles（engine3d/geoMerge，
 * 几何全等）：石色实体件（外池壁/池沿/中心柱/顶碗）顶点色合并为单 mesh；
 * 内水盘带 water_tile 贴图需独立材质，保留独立 mesh；Sparkles 不动。
 * 批次 28 二轮取舍：实体件粗糙度 0.7–0.8 统一为 0.75、金属度恒 0；外池壁
 * envMapIntensity 0.4 → 默认 1.0（4 件中 3 件本为默认）。
 * 批次 28 二轮取舍：caster 裁剪——地标不投影（shadow pass 实测 1044 DC 超阈，
 * 全地标 castShadow=false；原池壁/中心柱/顶碗的投影随批取消）。
 *
 * 契约：lag_docs/虚拟城市/已实现/16-3D城市WebGL质感与城市补全/02-架构设计 §8.1。
 */

import { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { Sparkles } from '@react-three/drei';
import { groundTileUrl } from '@/assets/images/virtualCity';
import { u } from '../cityScale';
import { type MergePart, cylPart, mergeParts, useSharedTexture } from '@/engine3d';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

const STONE = '#9aa1ab';
const STONE_DARK = '#7c838d';

/** 石色实体件合并统一粗糙度（批次 28 二轮取舍：原 0.7–0.8 取中）。 */
const SOLID_ROUGH = 0.75;

interface Props {
  x: number;
  z: number;
}

export function Fountain({ x, z }: Props) {
  const water = useSharedTexture(groundTileUrl('water_tile'), {
    wrap: 'repeat',
    repeat: [2, 2],
  });

  // 石色实体件 → 单顶点色几何（部件几何/位置/颜色与原逐件 JSX 全等；
  // 池沿 torus 的 rotation=[π/2,0,0] 经四元数矩阵等价搬移）
  const solidsGeo = useMemo(() => {
    const parts: MergePart[] = [
      // 外池壁（矮圆环壁：用圆柱 + 内水盘遮盖顶面形成池感）
      cylPart(u(2.2), u(2.3), u(0.5), 20, 0, u(0.25), 0, STONE),
      // 池沿（torus 压扁，平躺环面）
      {
        geo: new THREE.TorusGeometry(u(2.2), u(0.07), 8, 24),
        matrix: new THREE.Matrix4().compose(
          new THREE.Vector3(0, u(0.5), 0),
          new THREE.Quaternion().setFromEuler(new THREE.Euler(Math.PI / 2, 0, 0)),
          new THREE.Vector3(1, 1, 1),
        ),
        color: STONE_DARK,
      },
      // 中心柱
      cylPart(u(0.12), u(0.16), u(0.9), 10, 0, u(0.85), 0, STONE),
      // 顶碗
      cylPart(u(0.38), u(0.22), u(0.12), 14, 0, u(1.32), 0, STONE_DARK),
    ];
    return mergeParts(parts);
  }, []);
  useEffect(() => () => solidsGeo.dispose(), [solidsGeo]);

  const info = useObjectInfoProps('landmark.fountain', { anchorY: 1.2 });
  return (
    <group {...info} position={[x, 0, z]}>
      {/* 石色实体件合并（顶点色逐件保留；批次 28 二轮 caster 裁剪 → 不投影） */}
      <mesh geometry={solidsGeo}>
        <meshStandardMaterial vertexColors roughness={SOLID_ROUGH} metalness={0} />
      </mesh>
      {/* 内水盘（water_tile 贴图材质独立，几何不动） */}
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
