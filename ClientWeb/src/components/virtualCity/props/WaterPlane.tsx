/**
 * WaterPlane — 水面（14-3D城市渲染深化 · 阶段 I + 18-X PBR 双层滚动法线）：
 *
 * 18-X：主水面 + 细波层 mesh 双层叠加；两层法线贴图均由 useSharedPBR 加载
 *   pbr/synth/water_n（无粗糙度贴图 → 水面材质粗糙度保持常量 0.08）。
 * 关键（最易踩的坑）：useSharedPBR / useSharedTexture 返回的是**进程级共享**贴图，
 *   offset / repeat 也是共享的。两个水面 / 颜色层 / 法线层若共用同一份贴图，
 *   任何一处改 offset 都会让所有引用方同步偏移；同尺寸水面还会「同相位」闪烁。
 *   修复：每个独立层面 `tex.clone()` 派生独立 offset 实例，组件卸载时 dispose 克隆。
 *
 * prefers-reduced-motion 下两层法线与颜色层全部静止。
 * 贴图缺失 → 纯色 #1a3a52 兜底（02 §9 降级链）。
 *
 * 契约：lag_docs/虚拟城市/已实现/14-3D渲染深化/02-架构设计 §3.1 +
 *       lag_docs/虚拟城市/已实现/18-3D城市PBR材质与真实城市冲刺/02-架构设计 §2.4。
 */

import { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { groundTileUrl, pbrNormalUrl } from '@/assets/images/virtualCity';
import { useSharedPBR } from '../textureCache';

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/** 水面纯色兜底（深青蓝）。 */
const WATER_FALLBACK = '#1a3a52';
/** 颜色层水流速度（uv 单位/秒；保留 14 批次手感）。 */
const COLOR_FLOW_SPEED = 0.02;
/** 法线 n1 主波速率。 */
const N1_DX = 0.012, N1_DY = 0.006;
/** 法线 n2 细波速率（反向）。 */
const N2_DX = -0.008, N2_DY = 0.015;

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
  // 18-X PBR：颜色贴图 + 法线（synth/water 无粗糙度图）。wrap/repeat 共用。
  const shared = useSharedPBR(
    groundTileUrl('water_tile'),
    pbrNormalUrl('synth', 'water'),
    '',
    {
      wrap: 'repeat',
      repeat: [Math.max(1, Math.round(w / 2)), Math.max(1, Math.round(d / 2))],
      normalScale: [0.35, 0.35],
    },
  );

  // 克隆派生独立 offset —— 进程级共享纹理绝对不能直接改 offset（02 §2.4 警示）。
  const mapScroll = useMemo(() => {
    if (!shared.map) return null;
    const t = shared.map.clone();
    t.wrapS = t.wrapT = THREE.RepeatWrapping;
    t.needsUpdate = true;
    return t;
  }, [shared.map]);

  const n1 = useMemo(() => {
    if (!shared.normalMap) return null;
    const t = shared.normalMap.clone();
    t.wrapS = t.wrapT = THREE.RepeatWrapping;
    t.needsUpdate = true;
    return t;
  }, [shared.normalMap]);

  const n2 = useMemo(() => {
    if (!n1) return null;
    const t = n1.clone();
    // 细波层不同 repeat 比例，与主波错相
    t.repeat.set(1.7, 1.3);
    return t;
  }, [n1]);

  // 克隆纹理随组件卸载释放（claude → useSharedTexture 不会 dispose 共享源；克隆体归我们管）。
  useEffect(() => () => { mapScroll?.dispose(); n1?.dispose(); n2?.dispose(); }, [mapScroll, n1, n2]);

  // 18-X 法线贴图自带的 normalScale（在 matProps 里），克隆体共享同一 Vector2 —— 安全。
  const mainScale = shared.matProps.normalScale;

  useFrame((_state, delta) => {
    if (REDUCED_MOTION) return;
    if (mapScroll) mapScroll.offset.y = (mapScroll.offset.y + delta * COLOR_FLOW_SPEED) % 1;
    if (n1) {
      n1.offset.x += delta * N1_DX;
      n1.offset.y += delta * N1_DY;
    }
    if (n2) {
      n2.offset.x += delta * N2_DX;
      n2.offset.y += delta * N2_DY;
    }
  });

  // 无 PBR 时水面退化为单层静态（02 §3 行 7 降级）；mainMat 的 n1 为 null 时 spread 不会传 normalMap。
  const mainMatProps: Record<string, unknown> = {
    map: mapScroll ?? undefined,
    color: mapScroll ? '#ffffff' : WATER_FALLBACK,
    transparent: true,
    opacity: 0.92,
    roughness: 0.08,
    metalness: 0.35,
    envMapIntensity: 0.9,
  };
  if (n1) {
    mainMatProps.normalMap = n1;
    mainMatProps.normalScale = mainScale;
  }

  return (
    <group position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* 主水面 y=0.028（高于地面 0.02 / plaza 0.025，低于 curb 顶） */}
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.028, 0]} receiveShadow>
        <planeGeometry args={[w, d]} />
        <meshStandardMaterial {...mainMatProps} />
      </mesh>
      {/* 细波层 y=0.029（02 §2.4 第二层：仅法线 + 半透明 #9fd4ea，无 map，depthWrite false）。
          仅当 n2 存在时渲染，否则避免「一片平面挡视」的退化。 */}
      {n2 && (
        <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0.029, 0]}>
          <planeGeometry args={[w, d]} />
          <meshStandardMaterial
            color="#9fd4ea"
            normalMap={n2}
            normalScale={mainScale}
            transparent
            opacity={0.35}
            depthWrite={false}
            blending={THREE.NormalBlending}
          />
        </mesh>
      )}
    </group>
  );
}