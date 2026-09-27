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
  /**
   * 各向异性过滤等级（repeat 模式大平面：地面/道路掠射角清晰度）。
   * 批次 28 A5：high 8 / low 4（软件光栅器上 AF 采样成本高、收益低）。
   */
  anisotropy: number;
  /**
   * 阴影贴图更新间隔（帧）。批次 28 A3：太阳移动缓慢，每 4 帧（60fps 下 ~15Hz）
   * 更新阴影视觉无感，其余帧跳过整个 shadow pass（`shadowMap.autoUpdate = false`
   * + `needsUpdate`，见 DayNightCycle）。
   */
  shadowUpdateFrames: number;
  /** 雨/雪粒子上限（WeatherFX 读取；low 档减半）。 */
  weatherParticles: number;
}

export const QUALITY_PRESETS: Record<QualityTier, QualityPreset> = {
  high: {
    dpr: [1, 1.75],
    shadowMapSize: 2048,
    envReflection: true,
    anisotropy: 8,
    shadowUpdateFrames: 4,
    weatherParticles: 2000,
  },
  low: {
    dpr: [1, 1],
    shadowMapSize: 1024,
    envReflection: false,
    anisotropy: 4,
    shadowUpdateFrames: 4,
    weatherParticles: 1000,
  },
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

/** 最近一次探测结果（detectQualityTier 写入；供非 GL 上下文同步读取）。 */
let detectedTier: QualityTier | null = null;

/**
 * 质量档判定：
 *   - URL 覆盖优先（见 urlQualityOverride）；
 *   - 否则软件渲染器 → low，真机 GPU → high。
 * 判定结果缓存进模块单例，供 knownQualityTier() 读取（贴图默认参数等
 * 无 GL 上下文的调用点，批次 28 A5）。
 */
export function detectQualityTier(gl: THREE.WebGLRenderer): QualityTier {
  const override = urlQualityOverride();
  const tier = override ?? (isSoftwareRenderer(gl) ? 'low' : 'high');
  detectedTier = tier;
  return tier;
}

/**
 * 已知质量档（无 GL 上下文同步读取，批次 28 A5）：
 * 已探测 → 探测值；未探测 → URL 覆盖 ?? high（与 detectQualityTier 默认一致）。
 * ⚠️ 游戏侧策略（行人上限 / 树投影等）读此值映射，**不得**把游戏字段塞进
 * QUALITY_PRESETS（engine3d 禁止 import 游戏私有模块，CLAUDE.md §2.1 硬约束 5）。
 */
export function knownQualityTier(): QualityTier {
  return detectedTier ?? urlQualityOverride() ?? 'high';
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
