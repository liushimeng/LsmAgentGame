/**
 * PerfHud — 批次 28 A5：`?debug=1` 时的轻量性能浮层。
 *
 * 显示近 1s 平均 FPS + draw call + triangle，数据源 `window.__cityRenderInfo`
 * （EngineCanvas debugGlobalName 挂载的 renderer.info）。
 *
 * 硬约束：
 *   - 纯 DOM 浮层，**不进 R3F 树**（挂在 map-host 下与 WebGL Canvas 平级）；
 *   - 更新走 textContent 直写（每 500ms 一次），不 setState / 不触发 React 重渲染；
 *   - 非 debug 查询串时零渲染、零 rAF。
 */

import { useEffect, useRef } from 'react';

/** 采样窗口（近 1s 平均 FPS，与方案 §4 A5 口径一致）。 */
const SAMPLE_MS = 1000;

/** renderer.info 形状（three WebGLInfo 的 render 计数器）。 */
interface RenderInfoLike {
  render?: { calls?: number; triangles?: number };
}

export function PerfHud() {
  const ref = useRef<HTMLDivElement>(null);
  const framesRef = useRef(0);

  const enabled =
    typeof window !== 'undefined' && window.location.search.includes('debug=1');

  useEffect(() => {
    if (!enabled) return;
    let raf = 0;
    let last = performance.now();
    const tick = () => {
      framesRef.current += 1;
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    const timer = window.setInterval(() => {
      const el = ref.current;
      if (!el) return;
      const now = performance.now();
      const dt = (now - last) / 1000;
      last = now;
      const fps = dt > 0 ? framesRef.current / dt : 0;
      framesRef.current = 0;
      const info = (window as unknown as Record<string, unknown>)[
        '__cityRenderInfo'
      ] as RenderInfoLike | undefined;
      const calls = info?.render?.calls ?? -1;
      const tris = info?.render?.triangles ?? -1;
      el.textContent = `FPS ${fps.toFixed(0)} · DC ${calls} · tris ${tris}`;
    }, SAMPLE_MS);
    return () => {
      cancelAnimationFrame(raf);
      window.clearInterval(timer);
    };
  }, [enabled]);

  if (!enabled) return null;
  return (
    <div
      ref={ref}
      data-testid="virtualCity-perf-hud"
      data-perfhud="virtualCity"
      style={{
        position: 'absolute',
        // 批次 32：右下角。原先与 <FreeViewHud /> 同占左下角（left:12/bottom:12），
        // 两个状态条叠在一起 —— HUD 内容窄，PerfHud 右侧会从旁边露出来，调试时读数串行。
        right: 12,
        bottom: 12,
        zIndex: 10,
        padding: '4px 8px',
        background: 'rgba(10, 14, 20, 0.78)',
        color: '#e6edf3',
        border: '1px solid rgba(255, 255, 255, 0.28)',
        borderRadius: 6,
        fontSize: 12,
        fontFamily: 'ui-monospace, monospace',
        pointerEvents: 'none',
      }}
    />
  );
}

export default PerfHud;
