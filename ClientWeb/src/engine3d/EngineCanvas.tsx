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
 *   - 像素预算自适应 dpr（2026-09-28 方案 P4，useAdaptiveDpr）：
 *     调用方显式传 `dpr` prop → 原样透传（旧行为完全保留）；
 *     否则按质量档上界 QUALITY_PRESETS[knownQualityTier()].dpr[1]（low 档 /
 *     `?quality=low` 时即 1.0，与旧行为一致）+ 绘制缓冲像素预算
 *     `pixelBudget`（默认 8.5MP）算出数字 dpr —— 任何分辨率下
 *     宽×高×dpr² 钉在预算内（1080p 保 1.75 高清，4K@2dpr 自动降到 ≈1.01），
 *     高分屏不再绘制像素爆炸。宿主尺寸变化经 ResizeObserver + rAF 合并重算。
 *     <Canvas> 外包一层 100%×100% 宿主 div（hostRef），三个消费方
 *     （VirtualCityCityMap / LobbyScene / GameScene）宿主均为满尺寸容器，零改动。
 */

import type { ReactNode } from 'react';
import * as THREE from 'three';
import { Canvas } from '@react-three/fiber';
import { QUALITY_PRESETS, knownQualityTier } from './quality';
import { DEFAULT_PIXEL_BUDGET, useAdaptiveDpr } from './useAdaptiveDpr';

export interface EngineCanvasProps {
  /** 初始相机（透视）。 */
  camera?: { position: [number, number, number]; fov?: number; near?: number; far?: number };
  /** dpr 区间；缺省按质量档推导（high: [1, 1.75] / low: [1, 1]）。 */
  dpr?: [number, number];
  /**
   * 绘制缓冲像素预算（默认 8.5e6 ≈ 8.5MP），仅未显式传 dpr 时生效：
   * 数字 dpr = max(0.85, min(devicePixelRatio, 质量档上界, √(预算/(w·h))))。
   */
  pixelBudget?: number;
  /** 默认 true（PCFSoft 阴影贴图）。 */
  shadows?: boolean;
  /** ACESFilmic 曝光，默认 1.05。 */
  toneMappingExposure?: number;
  /** 命中此 URL 片段时挂载 renderer.info（默认 'debug=1'；传 '' 关闭）。 */
  debugQueryFlag?: string;
  /** renderer.info 挂载的 window 全局名（默认 '__engine3dRenderInfo'）。 */
  debugGlobalName?: string;
  /**
   * scene / camera 挂载的 window 全局名（默认 `${debugGlobalName}Scene`；传 '' 关闭）。
   *
   * 批次 43 新增：r3f v9 不在 canvas 上暴露 store（`canvas.__r3f` 为 undefined），
   * CDP 侧拿不到 three 场景就无法做「材质名 / 包围盒 / 坐标」数值审计 ——
   * 而批次 42 遗留 L2 正是因全页截图偏暗导致视觉判读受阻、改用数值审计替代。
   * 把 scene 一并挂到同一 `?debug=1` 闸门下（**仅调试态**，生产不挂），
   * 补齐这条审计通路。`{ scene, camera }` 同挂，便于做屏幕坐标↔世界坐标换算。
   */
  debugSceneGlobalName?: string;
  children?: ReactNode;
}

export function EngineCanvas({
  camera = { position: [0, 2, 5], fov: 60 },
  dpr,
  pixelBudget = DEFAULT_PIXEL_BUDGET,
  shadows = true,
  toneMappingExposure = 1.05,
  debugQueryFlag = 'debug=1',
  debugGlobalName = '__engine3dRenderInfo',
  debugSceneGlobalName,
  children,
}: EngineCanvasProps) {
  // 自适应 dpr 的质量档上界：low 档 / ?quality=low → 1.0（未探测时 knownQualityTier
  // 返回 URL 覆盖 ?? 'high'，与旧 `urlQualityOverride() === 'low' ? [1,1] : [1,1.75]` 一致）。
  const tierMaxDpr = QUALITY_PRESETS[knownQualityTier()].dpr[1];
  const { hostRef, dpr: adaptiveDpr } = useAdaptiveDpr(tierMaxDpr, pixelBudget);
  const effectiveDpr = dpr ?? adaptiveDpr;

  return (
    <div ref={hostRef} style={{ width: '100%', height: '100%', position: 'relative' }}>
      <Canvas
        shadows={shadows}
        dpr={effectiveDpr}
        camera={camera}
        gl={{ antialias: true, powerPreference: 'high-performance' }}
        onCreated={({ gl, scene, camera }) => {
          // 胶片色调映射（高光不过曝）+ 柔和阴影边缘
          gl.toneMapping = THREE.ACESFilmicToneMapping;
          gl.toneMappingExposure = toneMappingExposure;
          gl.shadowMap.type = THREE.PCFSoftShadowMap;
          if (
            typeof window !== 'undefined' &&
            debugQueryFlag &&
            window.location.search.includes(debugQueryFlag)
          ) {
            const w = window as unknown as Record<string, unknown>;
            w[debugGlobalName] = gl.info;
            // 批次 43：同闸门下挂 scene/camera，供 CDP 侧做材质名 / 包围盒 / 坐标审计
            // （r3f v9 的 canvas.__r3f 为 undefined，无其它公开通路拿到场景）。
            const sceneName = debugSceneGlobalName ?? `${debugGlobalName}Scene`;
            if (sceneName) {
              w[sceneName] = { scene, camera, gl };
            }
            // eslint-disable-next-line no-console
            console.log(`[engine3d] renderer.info mounted on window.${debugGlobalName}`, gl.info);
          }
        }}
      >
        {children}
      </Canvas>
    </div>
  );
}
