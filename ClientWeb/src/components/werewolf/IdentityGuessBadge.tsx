/**
 * IdentityGuessBadge — 玩家座位卡上的"我猜测身份"徽章 + 编辑弹出层。
 *
 * 2026-07-22 §任务2:
 *   - 活着时:右上角小徽章显示当前猜测(蓝紫虚线边),点击展开角色下拉。
 *   - 死亡后:不再显示猜测徽章(SeatCell 会同时渲染 .werewolf-seat__reveal-badge
 *     展示服务端揭示的真实身份,亮黄实线边,刻意差异化视觉)。
 *   - 观众 (my_seat<0) 不展示徽章(无猜测权限)。
 *
 * 视觉/行为约束:
 *   - 弹出层(z-index 60)高于 werewolf-action-panel(z=50),避免被
 *     投票 / 操作按钮遮挡。
 *   - ESC / 点击外部关闭,符合 §7.1 表层交互规范。
 *   - 角色枚举与 i18n key werewolf.role.* 严格对齐,新增/删角色时
 *     useIdentityGuess.GUESSABLE_ROLES 也要同步。
 */

import { useEffect, useRef, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { WerewolfRole } from '@/types/werewolf';

interface Props {
  seatIdx: number;
  mySeat: number;
  isAlive: boolean;
  /** 当前的猜测映射(来自 useIdentityGuess.guesses)。 */
  guess: WerewolfRole | null | undefined;
  /** 该座位可猜测的角色列表(已剔除 'unknown' 占位之外的非法值)。 */
  guessableRoles: WerewolfRole[];
  /** 点击选项后回调,value=null 表示清除。 */
  onChange: (seat: number, role: WerewolfRole | null) => void;
}

export function IdentityGuessBadge({
  seatIdx,
  mySeat,
  isAlive,
  guess,
  guessableRoles,
  onChange,
}: Props) {
  // React hooks 必须无条件调用,顺序在所有 early-return 之前。
  // canRender 在 hooks 之后判断;early-return 仅发生于「不渲染」分支。
  const t = useT();
  const [open, setOpen] = useState(false);
  const popoverRef = useRef<HTMLDivElement | null>(null);
  // §20260817-03 U2 — 弹层向上翻转:wrap 定位锚点 + 翻转状态。
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const [openAbove, setOpenAbove] = useState(false);

  // ESC + 外部点击关闭 — 无条件执行。
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    const onClick = (e: MouseEvent) => {
      if (popoverRef.current && !popoverRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    window.addEventListener('keydown', onKey);
    // mousedown 比 click 早一帧,避免点开时立即关闭
    window.addEventListener('mousedown', onClick);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('mousedown', onClick);
    };
  }, [open]);

  // §20260817-03 U2 — 打开后测一次弹层高度与网格边界,下方放不下则向上翻转。
  // 背景:.werewolf-table / __grid 均为 overflow-y:auto 滚动容器,最后一行座位
  // 的弹层(10+ 角色按钮,~300px)恒向下展开会被网格下边界裁掉。
  // 边界取网格 rect 与 viewport 的交集,与 EmotionAvatar 同策略。
  useEffect(() => {
    if (!open) {
      setOpenAbove(false);
      return;
    }
    const wrapEl = wrapRef.current;
    const popEl = popoverRef.current;
    if (!wrapEl || !popEl) return;
    const wRect = wrapEl.getBoundingClientRect();
    const pRect = popEl.getBoundingClientRect();
    const gridEl = wrapEl.closest('.werewolf-table') as HTMLElement | null;
    const gRect = gridEl ? gridEl.getBoundingClientRect() : null;
    const boundTop = Math.max(gRect ? gRect.top : 0, 0);
    const boundBottom = Math.min(gRect ? gRect.bottom : window.innerHeight, window.innerHeight);
    const spaceBelow = boundBottom - wRect.bottom - 6;
    const spaceAbove = wRect.top - boundTop - 6;
    setOpenAbove(pRect.height > spaceBelow && spaceAbove > spaceBelow);
  }, [open]);

  // 观众 / 自己 / 已死 → 不显示(early-return 必须放在所有 hooks 之后)。
  const canRender = !(mySeat < 0) && seatIdx !== mySeat && isAlive;
  if (!canRender) return null;

  const hasGuess = guess != null;

  return (
    <div
      ref={wrapRef}
      className="werewolf-guess-wrap"
      style={{ position: 'absolute', top: 2, right: 2, zIndex: 4 }}
    >
      <button
        type="button"
        className={`werewolf-seat__guess-badge ${hasGuess ? '' : 'is-empty'}`}
        title={t('werewolf.guess.title' as any)}
        aria-label={t('werewolf.guess.title' as any)}
        data-testid={`werewolf-guess-btn-${seatIdx}`}
        onClick={(e) => {
          e.stopPropagation();
          setOpen((v) => !v);
        }}
        style={{
          background: hasGuess ? undefined : 'rgba(60, 50, 80, 0.4)',
          color: hasGuess ? undefined : '#a398c0',
          borderStyle: hasGuess ? 'dashed' : 'dotted',
          borderColor: hasGuess ? undefined : 'rgba(160, 140, 200, 0.4)',
          cursor: 'pointer',
          pointerEvents: 'auto',
        }}
      >
        {hasGuess
          ? `🕵 ${t(`werewolf.role.${guess}` as any)}`
          : `+ ${t('werewolf.guess.label' as any)}`}
      </button>
      {open && (
        <div
          ref={popoverRef}
          className={`werewolf-guess-popover${openAbove ? ' werewolf-guess-popover--above' : ''}`}
          data-testid={`werewolf-guess-popover-${seatIdx}`}
          role="dialog"
          aria-label={t('werewolf.guess.title' as any)}
        >
          <p className="werewolf-guess-popover__title">
            {t('werewolf.guess.popoverTitle' as any, { seat: seatIdx + 1 })}
          </p>
          {guessableRoles.map((r) => (
            <button
              key={r}
              type="button"
              className={`werewolf-guess-popover__btn ${guess === r ? 'is-active' : ''}`}
              onClick={() => {
                onChange(seatIdx, r);
                setOpen(false);
              }}
              data-testid={`werewolf-guess-option-${seatIdx}-${r}`}
            >
              {t(`werewolf.role.${r}` as any)}
            </button>
          ))}
          {hasGuess && (
            <button
              type="button"
              className="werewolf-guess-popover__clear"
              onClick={() => {
                onChange(seatIdx, null);
                setOpen(false);
              }}
              data-testid={`werewolf-guess-clear-${seatIdx}`}
            >
              {t('werewolf.guess.clear' as any)}
            </button>
          )}
        </div>
      )}
    </div>
  );
}
