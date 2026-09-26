/**
 * VirtualCityMinimap — 左上角 Canvas2D 小地图（**不开第二个 r3f Canvas**，性能考虑）。
 *
 * 绘制：120×120 世界 → 132px（v2.12：40→80；批次 20：80→120，32 城区）；
 *   - 32 城区色块（透明度 0.35）；SCALE ≥1.4 px/单位时叠加区名首字，
 *     批次 20 后 SCALE=132/120=1.1 <1.4 → 只画色块，悬停以 canvas title 显示区名；
 *   - agent 点（职业色 4px 圆，与主地图 districtSeatOffset 同一落位公式）；
 *   - 相机视野框（主 Canvas viewRef 回传 target/distance 推算白框）；
 *   - rAF 与主 Canvas 同频重绘。
 * 交互：点击城区 → onSelectDistrict（主地图平滑聚焦 + 面板联动）。
 */

import { useEffect, useRef, useState } from 'react';
import { districtSeatOffset } from './AgentToken';
import { WORLD_SIZE, GROUND_TILE, type VirtualCityCameraView } from './VirtualCityCityMap';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  VIRTUAL_CITY_DISTRICTS,
  districtCenter,
  professionColor,
  type VirtualCityDistrictId,
  type VirtualCityGameState,
} from '@/types/virtualCity';

const SIZE = 132;                      // CSS 像素（触控目标 ≥44px 满足）
const WORLD = WORLD_SIZE;              // 世界 120×120（与主场景同源常量；批次 20）
const SCALE = SIZE / WORLD;            // 批次 20 后 = 132/120 ≈ 1.1 px / 世界单位
/** 区名首字渲染门槛（px/世界单位）：低于此值色块过密，只画色块 + canvas title 悬停。 */
const SHOW_LABEL = SCALE >= 1.4;
/** 城区底板边长（世界单位）——与 DistrictBlock / 主场景地面 GROUND_TILE 同源。 */
const DISTRICT_SIZE = GROUND_TILE;
/** 城区命中半径（点击容差）。 */
const DISTRICT_HIT_RADIUS = DISTRICT_SIZE / 2;

function worldToPx(x: number, z: number): { px: number; py: number } {
  return { px: (x + WORLD / 2) * SCALE, py: (z + WORLD / 2) * SCALE };
}

interface Props {
  gameState: VirtualCityGameState | null;
  viewRef: React.MutableRefObject<VirtualCityCameraView>;
  selectedDistrict: VirtualCityDistrictId | null;
  onSelectDistrict: (id: VirtualCityDistrictId) => void;
}

export function VirtualCityMinimap({ gameState, viewRef, selectedDistrict, onSelectDistrict }: Props) {
  const t = useT();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const stateRef = useRef(gameState);
  stateRef.current = gameState;
  const selectedRef = useRef(selectedDistrict);
  selectedRef.current = selectedDistrict;

  // 16 · 阶段 V：折叠态（localStorage 持久化；默认展开）。rAF 循环在折叠时
  // 自然停摆（canvasRef 为 null → effect 早退）。
  const [expanded, setExpanded] = useState<boolean>(() => {
    try {
      return localStorage.getItem('virtualCity.ui.minimap') !== '0';
    } catch {
      return true;
    }
  });
  const toggleExpanded = () => {
    setExpanded((prev) => {
      const next = !prev;
      try {
        localStorage.setItem('virtualCity.ui.minimap', next ? '1' : '0');
      } catch {
        // 隐私模式等存储失败：仅内存态生效
      }
      return next;
    });
  };

  // rAF 重绘循环（读 ref，避免 React 渲染节流）。
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    canvas.width = SIZE * dpr;
    canvas.height = SIZE * dpr;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    let raf = 0;

    const draw = () => {
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, SIZE, SIZE);
      ctx.fillStyle = '#0b0f16';
      ctx.fillRect(0, 0, SIZE, SIZE);

      // 城区色块（DISTRICT_SIZE 8×8 世界 → 8×SCALE px）+ 区名首字。
      for (const d of VIRTUAL_CITY_DISTRICTS) {
        const { px, py } = worldToPx(d.x - DISTRICT_HIT_RADIUS, d.z - DISTRICT_HIT_RADIUS);
        const size = DISTRICT_SIZE * SCALE;
        ctx.globalAlpha = 0.35;
        ctx.fillStyle = d.color;
        ctx.fillRect(px, py, size, size);
        ctx.globalAlpha = d.id === selectedRef.current ? 1 : 0.55;
        ctx.strokeStyle = d.id === selectedRef.current ? '#d4a017' : '#4b5563';
        ctx.lineWidth = d.id === selectedRef.current ? 2 : 1;
        ctx.strokeRect(px, py, size, size);
        // 批次 20：32 区后 SCALE≈1.1 <1.4 → 隐藏区名首字（只画色块，悬停看 title）。
        if (SHOW_LABEL) {
          ctx.globalAlpha = 1;
          ctx.fillStyle = '#e5e7eb';
          ctx.font = 'bold 11px system-ui, sans-serif';
          ctx.textAlign = 'center';
          ctx.textBaseline = 'middle';
          ctx.fillText(d.nameZh.charAt(0), px + size / 2, py + size / 2);
        }
      }

      // agent 点（职业色 4px 圆；同区多 token 环形落位与主地图一致）。
      const gs = stateRef.current;
      if (gs) {
        const byDistrict = new Map<string, number[]>();
        gs.players.forEach((p, i) => {
          if (!p.alive) return;
          const list = byDistrict.get(p.district) ?? [];
          list.push(i);
          byDistrict.set(p.district, list);
        });
        for (const [distId, idxs] of byDistrict) {
          const c = districtCenter(distId);
          idxs.forEach((playerIdx, i) => {
            const p = gs.players[playerIdx];
            const { dx, dz } = districtSeatOffset(i, idxs.length);
            const { px, py } = worldToPx(c.x + dx, c.z + dz);
            ctx.beginPath();
            ctx.arc(px, py, p.seat === gs.my_seat ? 4 : 3, 0, Math.PI * 2);
            ctx.fillStyle = professionColor(p.profession.id);
            ctx.fill();
            if (p.seat === gs.my_seat) {
              ctx.strokeStyle = '#d4a017';
              ctx.lineWidth = 2;
              ctx.stroke();
            }
          });
        }
      }

      // 相机视野框（target + distance；~0.6 投影系数近似透视范围）。
      const v = viewRef.current;
      const center = worldToPx(v.x, v.z);
      const half = Math.max(6, (v.dist * 0.6 * SCALE) / 2);
      ctx.strokeStyle = 'rgba(255,255,255,0.85)';
      ctx.lineWidth = 1;
      ctx.strokeRect(center.px - half, center.py - half, half * 2, half * 2);

      raf = requestAnimationFrame(draw);
    };
    raf = requestAnimationFrame(draw);
    return () => cancelAnimationFrame(raf);
  }, [viewRef]);

  // 命中测试：canvas 像素 → 世界坐标 → 城区底板（批次 20：32 区）。
  const districtAt = (e: React.MouseEvent<HTMLCanvasElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const px = e.clientX - rect.left;
    const py = e.clientY - rect.top;
    const wx = px / SCALE - WORLD / 2;
    const wz = py / SCALE - WORLD / 2;
    return VIRTUAL_CITY_DISTRICTS.find(
      (d) =>
        Math.abs(wx - d.x) <= DISTRICT_HIT_RADIUS &&
        Math.abs(wz - d.z) <= DISTRICT_HIT_RADIUS,
    );
  };

  // 点击 → 命中城区 → 联动主地图聚焦 + 选中态。
  const handleClick = (e: React.MouseEvent<HTMLCanvasElement>) => {
    const hit = districtAt(e);
    if (hit) onSelectDistrict(hit.id);
  };

  // 悬停：SHOW_LABEL 关闭时（32 区 SCALE<1.4）以 canvas title 显示区名。
  const handleMouseMove = (e: React.MouseEvent<HTMLCanvasElement>) => {
    if (SHOW_LABEL) return;
    const hit = districtAt(e);
    e.currentTarget.title = hit ? hit.nameZh : '';
  };

  // 16 · 阶段 V：折叠态渲染 pill 按钮（占位同位，点击展开）
  if (!expanded) {
    return (
      <button
        type="button"
        className="virtualCity-minimap__pill"
        onClick={toggleExpanded}
        aria-label={t('virtualCity.minimap.aria' as TKey)}
        title={t('virtualCity.minimap.aria' as TKey)}
      >
        🗺 地图
      </button>
    );
  }

  return (
    <div className="virtualCity-minimap__wrap">
      <canvas
        ref={canvasRef}
        className="virtualCity-minimap"
        width={SIZE}
        height={SIZE}
        onClick={handleClick}
        onMouseMove={handleMouseMove}
        aria-label={t('virtualCity.minimap.aria' as TKey)}
      />
      {/* 批次 26：静态罗盘角标（小地图固定朝向 上=北=−Z，方案 §1.1 罗盘契约）。
          置于关闭钮左侧避免遮挡；样式 §26：显式白字 + ≥45% 不透明底。 */}
      <span className="virtualCity-minimap__compass" aria-hidden="true" title="北（上）">
        北↑
      </span>
      <button
        type="button"
        className="virtualCity-minimap__close"
        onClick={toggleExpanded}
        aria-label="收起小地图"
        title="收起小地图"
      >
        ⤫
      </button>
    </div>
  );
}
