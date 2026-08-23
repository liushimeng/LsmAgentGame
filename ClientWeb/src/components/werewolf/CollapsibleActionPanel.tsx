/**
 * CollapsibleActionPanel — 狼人杀 13 人局「可折叠操作面板」统一骨架。
 *
 * 2026-08-23 §20260823-02 — 弹出式/堆叠式面板优化:
 *   白天发言阶段中栏曾同时摊开 DayControlPanel / PropPanel / SecretLetterPanel /
 *   FactionBetPanel 等多个全高面板,把座位表 WerewolfTable 挤出首屏。
 *   本骨架统一「header 整行可点 + 右侧 ≥44px 折叠按钮 + 收起态单行摘要 +
 *   localStorage 持久化 + 提交成功自动收起(collapseSignal)+ 必须操作时强制展开
 *   (forceExpand)」语义,供各操作面板复用。
 *
 * 狼人杀私有组件(CLAUDE.md §2.1 约束 1/2:不进 components/ui 等共享目录)。
 * 样式:styles/werewolf-20260823-02.css,类前缀 ww-cap-*。
 */

import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useT } from '@/hooks/useT';

export interface CollapsibleActionPanelProps {
  /** 面板 emoji。 */
  icon: string;
  /** i18n 后的标题。 */
  title: string;
  /** 收起态单行摘要(最近操作结果/状态)。 */
  summary?: React.ReactNode;
  /** 展开态 header 状态徽章(如 ☠已死亡 / 窗口已关闭)。 */
  badge?: React.ReactNode;
  /** localStorage 持久化键(ww_panel_*)。 */
  storageKey: string;
  /** 首次渲染默认(无存储时)。 */
  defaultCollapsed?: boolean;
  /** 递增计数器:变化即自动收起(提交成功后 +1),并写入 localStorage。 */
  collapseSignal?: number;
  /** 递增计数器:变化即自动展开一次(新阶段/新一天到来),并写入 localStorage。 */
  expandSignal?: number;
  /** true 时强制展开(提交失败就地改错 / 轮到我必须操作),忽略折叠态。 */
  forceExpand?: boolean;
  /** 死亡/观战等禁用态(仅样式)。 */
  disabled?: boolean;
  /** 追加到根节点的 className(如复用 werewolf-action-panel 既有样式)。 */
  className?: string;
  /** 展开态内容。 */
  children: React.ReactNode;
  testId?: string;
}

function readStored(storageKey: string): boolean | null {
  try {
    if (typeof window === 'undefined') return null;
    const v = window.localStorage.getItem(storageKey);
    return v === null ? null : v === '1';
  } catch {
    return null; // 隐身模式降级
  }
}

function writeStored(storageKey: string, collapsed: boolean): void {
  try {
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(storageKey, collapsed ? '1' : '0');
    }
  } catch { /* 隐身模式降级 */ }
}

export const CollapsibleActionPanel: React.FC<CollapsibleActionPanelProps> = ({
  icon,
  title,
  summary,
  badge,
  storageKey,
  defaultCollapsed = false,
  collapseSignal = 0,
  expandSignal = 0,
  forceExpand = false,
  disabled = false,
  className = '',
  children,
  testId,
}) => {
  const t = useT();
  const [collapsed, setCollapsed] = useState<boolean>(() => {
    const stored = readStored(storageKey);
    return stored !== null ? stored : defaultCollapsed;
  });

  // 手动展开/收起 —— 始终写 localStorage,跨阶段记住用户偏好。
  const toggle = useCallback(() => {
    setCollapsed((c) => {
      const next = !c;
      writeStored(storageKey, next);
      return next;
    });
  }, [storageKey]);

  // collapseSignal 递增 → 立即收起(视为用户意图的延伸,写 localStorage)。
  const prevCollapseSignal = useRef(collapseSignal);
  useEffect(() => {
    if (collapseSignal !== prevCollapseSignal.current) {
      prevCollapseSignal.current = collapseSignal;
      setCollapsed(true);
      writeStored(storageKey, true);
    }
  }, [collapseSignal, storageKey]);

  // expandSignal 递增 → 自动重新展开一次(新一天/新投票轮)。
  const prevExpandSignal = useRef(expandSignal);
  useEffect(() => {
    if (expandSignal !== prevExpandSignal.current) {
      prevExpandSignal.current = expandSignal;
      setCollapsed(false);
      writeStored(storageKey, false);
    }
  }, [expandSignal, storageKey]);

  // forceExpand=true → 忽略折叠态强制展开(不改写用户存储的偏好)。
  const effectiveCollapsed = forceExpand ? false : collapsed;

  const collapseLabel = t('werewolf.panel.collapse');
  const expandLabel = t('werewolf.panel.expand');

  return (
    <div
      className={`ww-cap${effectiveCollapsed ? ' is-collapsed' : ' is-open'}${disabled ? ' is-disabled' : ''}${className ? ` ${className}` : ''}`}
      data-testid={testId}
    >
      {/* header 整行可点;右侧 44px 折叠按钮(aria-expanded + i18n aria-label)。 */}
      <div
        className="ww-cap__header"
        role="button"
        tabIndex={0}
        aria-expanded={!effectiveCollapsed}
        onClick={toggle}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            toggle();
          }
        }}
      >
        <h4 className="ww-cap__title">
          {icon} {title}
          {badge && <span className="ww-cap__badge">{badge}</span>}
        </h4>
        {effectiveCollapsed && summary && (
          <span className="ww-cap__summary">{summary}</span>
        )}
        <button
          type="button"
          className="ww-cap__toggle"
          aria-expanded={!effectiveCollapsed}
          aria-label={effectiveCollapsed ? expandLabel : collapseLabel}
          title={effectiveCollapsed ? expandLabel : collapseLabel}
          // 点击按钮即切换;阻止冒泡避免 header 的 toggle 二次触发。
          onClick={(e) => {
            e.stopPropagation();
            toggle();
          }}
        >
          {effectiveCollapsed ? '▶' : '▼'}
        </button>
      </div>
      {!effectiveCollapsed && <div className="ww-cap__body">{children}</div>}
    </div>
  );
};

export default CollapsibleActionPanel;
