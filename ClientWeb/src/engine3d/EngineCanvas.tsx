/**
 * engine3d/EngineCanvas — 通用 R3F Canvas 封装（引擎初始化唯一入口）。
 *
 * 22-3D世界升级与引擎模块化：收敛此前散落在 VirtualCityCityMap.onCreated 的
 * 渲染器初始化（ACESFilmic 色调映射 / PCFSoft 阴影 / debug renderer.info 挂载），
 * 任何 3D 游戏/程序统一走此组件，不再各自裸写 <Canvas onCreated={...}>。
 *
 * 契约：
 *   - 默认 shadows + antialias + high-performance；dpr 默认 [1, 1.75]，
 *     `?quality=low` 时未显式传 dpr 则自动降为 [1, 1]（quality.ts）。
 *   - `?debug=1`（可用 debugQueryFlag 改）时把 renderer.info 挂到
 *     window[debugGlobalName]（默认 __engine3dRenderInfo）供 CDP 实测
 *     draw call / geometry / program，一次性挂载无 runtime 开销。
 */

import type { ReactNode } from 'react';
import * as THREE from 'three';
import { Canvas } from '@react-three/fiber';
import { urlQualityOverride } from './quality';

export interface EngineCanvasProps {
  /** 初始相机（透视）。 */
  camera?: { position: [number, number, number]; fov?: number; near?: number; far?: number };
  /** dpr 区间；缺省按质量档推导（high: [1, 1.75] / low: [1, 1]）。 */
  dpr?: [number, number];
  /** 默认 true（PCFSoft 阴影贴图）。 */
  shadows?: boolean;
  /** ACESFilmic 曝光，默认 1.05。 */
  toneMappingExposure?: number;
  /** 命中此 URL 片段时挂载 renderer.info（默认 'debug=1'；传 '' 关闭）。 */
  debugQueryFlag?: string;
  /** renderer.info 挂载的 window 全局名（默认 '__engine3dRenderInfo'）。 */
  debugGlobalName?: string;
  children?: ReactNode;
}

export function EngineCanvas({
  camera = { position: [0, 2, 5], fov: 60 },
  dpr,
  shadows = true,
  toneMappingExposure = 1.05,
  debugQueryFlag = 'debug=1',
  debugGlobalName = '__engine3dRenderInfo',
  children,
}: EngineCanvasProps) {
  const effectiveDpr: [number, number] =
    dpr ?? (urlQualityOverride() === 'low' ? [1, 1] : [1, 1.75]);

  return (
    <Canvas
      shadows={shadows}
      dpr={effectiveDpr}
      camera={camera}
      gl={{ antialias: true, powerPreference: 'high-performance' }}
      onCreated={({ gl }) => {
        // 胶片色调映射（高光不过曝）+ 柔和阴影边缘
        gl.toneMapping = THREE.ACESFilmicToneMapping;
        gl.toneMappingExposure = toneMappingExposure;
        gl.shadowMap.type = THREE.PCFSoftShadowMap;
        if (
          typeof window !== 'undefined' &&
          debugQueryFlag &&
          window.location.search.includes(debugQueryFlag)
        ) {
          (window as unknown as Record<string, unknown>)[debugGlobalName] = gl.info;
          // eslint-disable-next-line no-console
          console.log(`[engine3d] renderer.info mounted on window.${debugGlobalName}`, gl.info);
        }
      }}
    >
      {children}
    </Canvas>
  );
}
