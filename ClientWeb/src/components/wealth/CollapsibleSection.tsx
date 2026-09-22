/**
 * CollapsibleSection — 侧栏面板折叠容器（13-3D城市渲染优化 · 阶段 E）。
 *
 * 契约：lag_docs/虚拟城市/已实现/13-3D城市渲染优化/04-UI布局规范-面板折叠与防溢出-v1.md §3。
 *
 * 设计要点：
 *   - 融合式标题头：调用方把面板原有标题传 `title`、右侧附加控件传 `headerExtra`，
 *     不叠加第二层标题栏（CityStatsPanel / WealthBotPanel 均有既有标题头）。
 *   - 折叠态持久化：window.localStorage '1'/'0'（对齐狼人杀 GameStatusHeader /
 *     PropPanel 的 LS_* 既有模式；shared/utils/ui-storage 是登录凭证专用加密存储，
 *     不适用于 UI 偏好）。
 *   - 动画：CSS grid-template-rows 1fr→0fr 技巧（无需魔法 max-height 值），
 *     prefers-reduced-motion 下关闭过渡（§26.3 三件套规约）。
 *   - 外层 wrapper 透传 `className`（如 wealth-citypanel / wealth-botpanel），
 *     保持 `.wealth-sidebar > .wealth-botpanel` 等既有选择器不脱节。
 */

import { useState, type ReactNode } from 'react';

interface Props {
  /** 标题内容（面板原有标题文案/图标）。 */
  title: ReactNode;
  /** 标题行右侧附加控件（如入口按钮），点击不触发折叠。 */
  headerExtra?: ReactNode;
  /** localStorage 持久化键（约定 `wealth.ui.collapsed.<section>`）。 */
  storageKey: string;
  /** 首次渲染默认折叠（无持久化记录时）。 */
  defaultCollapsed?: boolean;
  /** 透传到最外层 wrapper 的既有面板类名（兼容既有 CSS 选择器）。 */
  className?: string;
  /** 折叠体附加类名。 */
  bodyClassName?: string;
  /** data-testid 透传（巡检用）。 */
  testId?: string;
  children: ReactNode;
}

function readCollapsed(key: string, fallback: boolean): boolean {
  try {
    const v = window.localStorage.getItem(key);
    if (v === null) return fallback;
    return v === '1';
  } catch {
    return fallback;
  }
}

export function CollapsibleSection({
  title,
  headerExtra,
  storageKey,
  defaultCollapsed = false,
  className,
  bodyClassName,
  testId,
  children,
}: Props) {
  const [collapsed, setCollapsed] = useState<boolean>(() =>
    typeof window === 'undefined' ? defaultCollapsed : readCollapsed(storageKey, defaultCollapsed),
  );

  const toggle = () => {
    setCollapsed((prev) => {
      const next = !prev;
      try {
        window.localStorage.setItem(storageKey, next ? '1' : '0');
      } catch {
        // best-effort：隐私模式 / 容量满不报错（与 useMindMirror 同策略）
      }
      return next;
    });
  };

  return (
    <div
      className={`wealth-collapse${className ? ` ${className}` : ''}`}
      data-testid={testId}
    >
      <div className="wealth-collapse__header">
        <button
          type="button"
          className="wealth-collapse__toggle"
          aria-expanded={!collapsed}
          onClick={toggle}
        >
          <span className="wealth-collapse__arrow" aria-hidden="true">
            {collapsed ? '▸' : '▾'}
          </span>
          <span className="wealth-collapse__title">{title}</span>
        </button>
        {headerExtra && <div className="wealth-collapse__extra">{headerExtra}</div>}
      </div>
      <div
        className={
          `wealth-collapse__body${collapsed ? ' wealth-collapse__body--collapsed' : ''}` +
          (bodyClassName ? ` ${bodyClassName}` : '')
        }
      >
        <div className="wealth-collapse__inner">{children}</div>
      </div>
    </div>
  );
}

export default CollapsibleSection;
