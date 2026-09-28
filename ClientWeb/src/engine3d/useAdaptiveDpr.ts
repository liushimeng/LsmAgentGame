/**
 * engine3d/useAdaptiveDpr — 绘制缓冲像素预算自适应 dpr hook。
 *
 * 2026-09-28 主界面布局满铺与分辨率自适应方案（P4）：高分屏下 WebGL 绘制缓冲
 * 随分辨率线性暴涨（4K≈8.3MP CSS × dpr1.75² ≈ 25MP/帧 → 掉帧/过热）。本 hook 按
 * 宿主容器 CSS 尺寸反解 dpr，把 `宽 × 高 × dpr²` 钉在像素预算内，分辨率越大
 * dpr 自动越低，任何分辨率下绘制像素 ≈ 预算值。
 *
 * 公式推导：
 *   约束  w × h × dpr² ≤ pixelBudget
 *   ⇒    dpr ≤ √(pixelBudget / (w·h))            …记为 byBudget
 *   取    next = max(DPR_FLOOR, min(devicePixelRatio, maxDpr, byBudget))
 *   其中 maxDpr 由调用方按质量档给出（QUALITY_PRESETS[tier].dpr[1]；
 *   `?quality=low` / 软件渲染器 → 1.0，行为与旧 dpr 区间一致）。
 *
 * 标定示例（默认预算 8.5e6 ≈ 8.5MP）：
 *   - 1080p（2.07MP）@dpr1：min(1, 1.75, √(8.5/2.07)≈2.02) = 1 → 缓冲 2.1MP；
 *   - 1080p @dpr2：min(2, 1.75, 2.02) = 1.75 → 缓冲 6.3MP（清晰度与现状一致）；
 *   - 4K（8.29MP）@dpr1：= 1 → 缓冲 8.3MP；
 *   - 4K @dpr2：min(2, 1.75, √(8.5/8.29)≈1.013) ≈ 1.01 → 缓冲 8.4MP，
 *     即 4K@2dpr 被压到 dpr≈1.01，不再像素爆炸。
 *
 * 契约：
 *   - ResizeObserver 监听宿主，回调经 rAF 合并，防 resize 风暴连续重绘；
 *   - 宿主 clientWidth/clientHeight ≤ 0（隐藏/卸载中）时跳过重算，防 dpr 算成 ∞；
 *   - 与上次计算差 ≤ 0.01 时不 setState，避免边界抖动反复触发 R3F setDpr；
 *   - mount 时立即计算一次；卸载时 disconnect + cancelAnimationFrame。
 *   - dpr 变化补偿（跨屏拖动 / 浏览器缩放的边缘缺口）：devicePixelRatio 变化但
 *     宿主 CSS 尺寸不变时（窗口拖到混合 DPI 显示器，dpr 1↔2 切换；Ctrl± 缩放），
 *     ResizeObserver 不触发。两条补偿监听：
 *     ① matchMedia('(resolution: <当前 dpr>dppx)') 的 change 监听 —— dpr 命中/离开
 *       该档分辨率即触发；触发时移除旧监听 → recompute() → 按新的
 *       window.devicePixelRatio 重建 MQL 重新订阅（dpr 是离散档，须逐档跟踪）；
 *     ② window resize → schedule()（rAF 合并，复用 ResizeObserver 路径兜底）。
 *
 * 仅依赖 React 运行时，engine3d 通用层（CLAUDE.md §2.1 硬约束 5：禁引游戏私有模块）。
 */

import { useEffect, useRef, useState } from 'react';
import type { RefObject } from 'react';

/** 默认绘制缓冲像素预算（≈8.5MP）：4K@dpr1.0 与 1080p@dpr1.75 同量级。 */
export const DEFAULT_PIXEL_BUDGET = 8.5e6;

/** dpr 下限：极小画布不再无限下压，保底渲染精度（缩到 0.85 以下收益趋零）。 */
export const DPR_FLOOR = 0.85;

/** 变化阈值：|next - last| ≤ 0.01 视为不变，不触发 setState（防边界抖动）。 */
const DPR_EPSILON = 0.01;

export interface AdaptiveDpr {
  /** 挂到画布宿主容器（须为满尺寸块；EngineCanvas 用它包 <Canvas>）。 */
  hostRef: RefObject<HTMLDivElement>;
  /** 计算出的数字 dpr，直接传给 <Canvas dpr={dpr}>（R3F 响应式 setDpr）。 */
  dpr: number;
}

/**
 * 像素预算自适应 dpr。
 * @param maxDpr     质量档 dpr 上界（high 档 1.75 / low 档 1.0）。
 * @param pixelBudget 绘制缓冲像素预算，默认 DEFAULT_PIXEL_BUDGET。
 */
export function useAdaptiveDpr(
  maxDpr: number,
  pixelBudget: number = DEFAULT_PIXEL_BUDGET,
): AdaptiveDpr {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const [dpr, setDpr] = useState(1);
  // 上次计算值放 ref：observer 回调里比对不依赖 state 闭包，也不触发重订阅。
  const lastDprRef = useRef(1);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    let rafId = 0;

    const recompute = () => {
      const w = host.clientWidth;
      const h = host.clientHeight;
      // 隐藏/卸载中（display:none / 尚未布局）→ 跳过，防 w·h=0 使 byBudget=∞。
      if (w <= 0 || h <= 0) return;
      // dpr² × w × h ≤ pixelBudget  ⇒  dpr ≤ √(pixelBudget / (w·h))
      const byBudget = Math.sqrt(pixelBudget / (w * h));
      const next = Math.max(
        DPR_FLOOR,
        Math.min(window.devicePixelRatio || 1, maxDpr, byBudget),
      );
      if (Math.abs(next - lastDprRef.current) > DPR_EPSILON) {
        lastDprRef.current = next;
        setDpr(next);
      }
    };

    // rAF 合并：resize 风暴期间只保留最后一帧的计算。
    const schedule = () => {
      if (rafId) cancelAnimationFrame(rafId);
      rafId = requestAnimationFrame(() => {
        rafId = 0;
        recompute();
      });
    };

    recompute(); // mount 立即算一次（此时宿主已完成布局）。
    const observer = new ResizeObserver(schedule);
    observer.observe(host);

    // ── dpr 变化补偿 ──────────────────────────────────────────────
    // 场景：窗口拖到混合 DPI 显示器（dpr 1↔2 切换）、浏览器缩放（Ctrl±）——
    // devicePixelRatio 变了但宿主 CSS 尺寸不变，ResizeObserver 不触发，
    // 纯靠它 dpr 永不重算。用 matchMedia('(resolution: Xdppx)') 跟踪「当前
    // dpr 这一档」：dpr 变化使查询求值翻转即触发 change。
    let mql: MediaQueryList | null = null;
    // function 声明提升，解决 onResolutionChange ↔ subscribeResolution 互相引用。
    function subscribeResolution() {
      // SSR / 无 matchMedia 环境守卫（useEffect 本就不在服务端运行，双保险）。
      if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;
      mql = window.matchMedia(`(resolution: ${window.devicePixelRatio || 1}dppx)`);
      mql.addEventListener('change', onResolutionChange);
    }
    function onResolutionChange() {
      // 先移除旧监听，避免重复触发；recompute 后再按新 dpr 重建订阅
      // （dpr 是离散档：1 / 1.25 / 1.5 / 2 …，必须逐档重新订阅）。
      if (mql) {
        mql.removeEventListener('change', onResolutionChange);
        mql = null;
      }
      recompute();
      subscribeResolution();
    }
    subscribeResolution(); // mount 初始订阅一次

    // window resize 兜底（rAF 合并，复用 ResizeObserver 的 schedule 路径）；
    // 与 ResizeObserver 互补：observer 不报的边角（如滚动条出现/消失）也覆盖到。
    window.addEventListener('resize', schedule);

    return () => {
      observer.disconnect();
      if (rafId) cancelAnimationFrame(rafId);
      if (mql) mql.removeEventListener('change', onResolutionChange);
      window.removeEventListener('resize', schedule);
    };
  }, [maxDpr, pixelBudget]);

  return { hostRef, dpr };
}
