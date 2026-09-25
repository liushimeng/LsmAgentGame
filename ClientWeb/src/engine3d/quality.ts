/**
 * engine3d/quality — 渲染质量档与功能开关（引擎层唯一事实来源）。
 *
 * 22-3D世界升级与引擎模块化（tmpPlan/虚拟城市-2.5D升级3D世界与引擎模块化方案-20260925.md §2.2）：
 *   - isSoftwareRenderer 自 EnvBinder 提取为公共函数（软件光栅器检测）。
 *   - QualityTier 质量档：软件渲染器 / ?quality=low → low（dpr 1.0、shadow map 1024）。
 *   - blenderModelsEnabled() 收敛 8 处 localStorage.getItem('disable-blender-models') 直读
 *     （CLAUDE.md §27.5 feature flag 契约不变，仅统一入口）。
 */

import type * as THREE from 'three';

/**
 * 软件渲染器检测（无独立 GPU 的环境：CI 无头浏览器 / 远程桌面 / 集显省电模式）。
 * SwiftShader 等软件光栅器上，长寿命 PMREM RenderTarget 会在运行数十秒后被
 * 采样成纯白（2026-09-22 视觉验收实测：整城泛白）——此类环境应走 low 质量档。
 */
export function isSoftwareRenderer(gl: THREE.WebGLRenderer): boolean {
  try {
    const ctx = gl.getContext();
    const ext = ctx.getExtension('WEBGL_debug_renderer_info');
    if (!ext) return false;
    const renderer = String(ctx.getParameter(ext.UNMASKED_RENDERER_WEBGL) ?? '');
    return /swiftshader|software|llvmpipe|basic render/i.test(renderer);
  } catch {
    return false;
  }
}

/** 渲染质量档。 */
export type QualityTier = 'high' | 'low';

/** 各质量档的渲染参数预设。 */
export interface QualityPreset {
  /** Canvas dpr 区间。 */
  dpr: [number, number];
  /** 主方向光 shadow map 边长。 */
  shadowMapSize: number;
  /** 是否启用 PMREM 环境反射（low 档跳过，规避软件渲染器白屏）。 */
  envReflection: boolean;
}

export const QUALITY_PRESETS: Record<QualityTier, QualityPreset> = {
  high: { dpr: [1, 1.75], shadowMapSize: 2048, envReflection: true },
  low: { dpr: [1, 1], shadowMapSize: 1024, envReflection: false },
};

/**
 * 同步读取 URL 质量档覆盖（不需要 GL 上下文，可在 Canvas 创建前调用）：
 * `?quality=low` / `?quality=high`，未指定返回 null。
 */
export function urlQualityOverride(): QualityTier | null {
  if (typeof window === 'undefined') return null;
  const q = window.location.search;
  if (q.includes('quality=low')) return 'low';
  if (q.includes('quality=high')) return 'high';
  return null;
}

/**
 * 质量档判定：
 *   - URL 覆盖优先（见 urlQualityOverride）；
 *   - 否则软件渲染器 → low，真机 GPU → high。
 */
export function detectQualityTier(gl: THREE.WebGLRenderer): QualityTier {
  const override = urlQualityOverride();
  if (override) return override;
  return isSoftwareRenderer(gl) ? 'low' : 'high';
}

/**
 * Blender .glb 模型总开关（CLAUDE.md §27.5）：
 * localStorage `disable-blender-models === '1'` → 强制全 fallback 程序化几何（测试/回滚用）。
 * 此前 8 个组件各自直读 localStorage，此处收敛为唯一入口。
 */
export function blenderModelsEnabled(): boolean {
  if (typeof window === 'undefined') return true;
  try {
    return window.localStorage.getItem('disable-blender-models') !== '1';
  } catch {
    return true;
  }
}
