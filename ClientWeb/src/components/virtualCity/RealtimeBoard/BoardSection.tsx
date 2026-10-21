/**
 * BoardSection — 批次 69 看板维度面板的**统一折叠外壳**。
 *
 * 需求 §五.1：每个维度面板外层包一个可折叠区块 ——
 *   - 折叠态：只显示一行标题条（≤32px 高），面板体不渲染；
 *   - 展开态：标题条 + 面板体。
 *
 * 折叠状态由**父组件（VirtualCityDashboard）**按维度 id 记忆（Record<id, boolean>），
 * 组件本身是受控组件（collapsed / onToggle）—— 这样切换一级 Tab 再切回来时
 * 折叠状态不丢（若把 useState 放在本组件内，卸载即丢）。
 */

import type { ReactNode } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

export interface BoardSectionProps {
  /** 标题条文案（通常 = 维度标题）。 */
  title: string;
  /** 当前是否折叠。 */
  collapsed: boolean;
  /** 点击标题条 / chevron 时的切换回调。 */
  onToggle: () => void;
  /** 面板体（折叠时不渲染）。 */
  children: ReactNode;
}

export function BoardSection({ title, collapsed, onToggle, children }: BoardSectionProps) {
  const t = useT();
  const label = collapsed
    ? t('virtualCity.dash.expand' as TKey)
    : t('virtualCity.dash.collapse' as TKey);

  return (
    <section
      className={
        'virtualCity-dash__section' + (collapsed ? ' virtualCity-dash__section--collapsed' : '')
      }
      data-testid="virtualCity-dash-section"
    >
      <button
        type="button"
        className="virtualCity-dash__section-head"
        onClick={onToggle}
        aria-expanded={!collapsed}
        aria-label={`${title} — ${label}`}
        title={label}
        data-testid="virtualCity-dash-section-toggle"
      >
        <span className="virtualCity-dash__chevron" aria-hidden="true">
          {collapsed ? '▸' : '▾'}
        </span>
        <span className="virtualCity-dash__section-title">{title}</span>
      </button>
      {!collapsed && (
        <div className="virtualCity-dash__section-body" data-testid="virtualCity-dash-section-body">
          {children}
        </div>
      )}
    </section>
  );
}

export default BoardSection;
