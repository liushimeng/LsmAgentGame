/**
 * EmotionAvatar — 狼人杀座位卡 Agent 拟人化表情头像 (2026-08-04 §表情特效)
 *
 * 设计文档: docs/Agent拟人化和表情特效-解决和设计方案-20260804-02.md §6。
 *
 * 层次(自底向上):
 *   1. 底层 <img>: emotionImageByKey[emotionKey] ?? neutral;
 *      onError 回退 neutral,再失败回退 emoji 徽章 DOM(getEmotionMeta 的 emoji)。
 *      素材未生成(空聚合表)时直接渲染 emoji 徽章降级。
 *   2. 特效层 .ww-fx--{effect}.ww-fx--{intensity}: 纯 CSS keyframes,
 *      pointer-events:none(§124 硬约束);活跃判定
 *      nowMs - fxStartedAtMs < fxDurationMs,仅在活跃窗口挂 250ms setInterval
 *      自更新 nowMs,到期自动卸载进入余韵态(零定时器)。
 *   3. caption 气泡 .ww-fx-caption: 特效活跃且 caption 非空时渲染
 *      (渐显 → 常驻 → 随特效结束渐隐)。
 *   4. 余韵态 .ww-fx--breathe: 特效到期或无 fx 字段时 — 头像常驻 +
 *      2.8s 呼吸动画 + 情绪色描边(--emotion-color inline 注入,取 bgDark 色板)。
 *
 * 兼容: 根节点保留 data-testid="seat-emotion-{seatIdx}" 与
 * title="{label} | {reason}"(旧测试选择器与审计 tooltip 不破)。
 */

import { useEffect, useRef, useState } from 'react';
import { emotionImageByKey } from '@/assets/images/werewolf/emotions';
import { getEmotionMeta } from './emotion';

interface EmotionAvatarProps {
  /** 情绪 key(confident/excited/.../tired);缺省/非法 → neutral 视觉。 */
  emotionKey?: string;
  /** 特效种类(pulse/shake/sweat/rage/tears/spin_question/glow/drowsy)。 */
  effect?: string;
  /** 特效强度(low/mid/high)。 */
  intensity?: string;
  /** ≤20 字表情文字气泡。 */
  caption?: string;
  /** 情绪切换原因(emotion_reason,审计 tooltip 用)。 */
  reason?: string;
  /** 特效开始 unix ms。 */
  fxStartedAtMs?: number;
  /** 特效持续 ms。 */
  fxDurationMs?: number;
  /** 座位号(0-indexed),用于 data-testid。 */
  seatIdx: number;
  /** 2026-08-05 — 情绪最近一次切换的 unix 毫秒时间戳(供 hover popover 显示相对时间)。 */
  updatedAtMs?: number;
  /** 2026-08-05 — 最近 5 次情绪切换历史(供 hover popover 展示「情绪曲线」)。 */
  history?: Array<{ emotion: string; reason: string; at_ms: number }>;
}

/** 特效活跃窗口判定。started/duration 任一缺失视为不活跃(纯余韵态)。 */
function isFxActive(nowMs: number, startedAtMs?: number, durationMs?: number): boolean {
  if (!startedAtMs || startedAtMs <= 0) return false;
  const dur = durationMs && durationMs > 0 ? durationMs : 12000;
  return nowMs - startedAtMs < dur;
}

export function EmotionAvatar({
  emotionKey,
  effect,
  intensity,
  caption,
  reason,
  fxStartedAtMs,
  fxDurationMs,
  seatIdx,
  updatedAtMs,
  history,
}: EmotionAvatarProps) {
  const meta = getEmotionMeta(emotionKey);
  const label = meta?.label ?? '';
  const emoji = meta?.emoji ?? '🙂';
  // 情绪色描边取深底色板(WCAG 已保障);无情绪时不描边。
  const emotionColor = meta?.bgDark;

  // 2026-08-05 — hover 态:鼠标悬停头像时展开 popover,展示当前情绪 emoji+label、
  // 切换原因、相对时间、最近情绪曲线(若有)。CSS :hover 兜底(不依赖 JS 状态即可
  // 显示基本信息),JS hover 状态仅用于延迟关闭(让鼠标移到 popover 上时不闪断)。
  const [hovered, setHovered] = useState(false);
  const [nowMs, setNowMs] = useState(() => Date.now());
  // §20260811-06 U2 — EmotionAvatar 密集布局碰撞修复。
  // popover 默认绝对定位可能被裁剪到 viewport 外(13 人局最上 2 行头像 hover 时
  // 向上展开被裁掉),或 popover 横跨相邻座位。avatarRef + popoverRef 用于测
  // getBoundingClientRect(),动态决定 placement(openAbove / openLeft / openRight)
  // 与水平偏移,保证 popover 永远完整可见在 viewport 内。
  const avatarRef = useRef<HTMLDivElement | null>(null);
  const popoverRef = useRef<HTMLDivElement | null>(null);
  const [placement, setPlacement] = useState<{
    vertical: 'above' | 'below';
    horizontal: 'center' | 'left' | 'right';
  }>({ vertical: 'below', horizontal: 'center' });

  // 素材降级链: 具体情绪 PNG → neutral PNG → emoji 徽章 DOM。
  const imgSrc =
    (emotionKey && emotionImageByKey[emotionKey]) ||
    emotionImageByKey['neutral'] ||
    '';
  const [imgFailed, setImgFailed] = useState(false);
  useEffect(() => {
    // emotion 切换后重置失败标记,给新 PNG 一次加载机会。
    setImgFailed(false);
  }, [imgSrc]);
  const showEmojiFallback = !imgSrc || imgFailed;

  // 特效活跃窗口自更新:仅在活跃时挂 250ms tick,到期自动卸载(余韵态零定时器)。
  // 同时复用到 hover popover 的相对时间显示。
  const active = isFxActive(nowMs, fxStartedAtMs, fxDurationMs);
  useEffect(() => {
    if (!active && !hovered) return;
    const id = setInterval(() => setNowMs(Date.now()), 250);
    return () => clearInterval(id);
  }, [active, hovered, fxStartedAtMs, fxDurationMs]);

  // §20260811-06 U2 — 当 popover 打开时,测一次 avatar 位置 + popover 尺寸,
  // 决定 placement。popover 真实渲染后才可测,所以 useLayoutEffect(同步测,避免抖动)。
  //
  // §20260817-03 U2 — 翻转边界从 viewport 改为座位网格滚动容器:
  // .werewolf-table / .werewolf-table__grid 均为 overflow-y:auto(2026-07-26
  // 聊天滚动修复引入),overflow-y:auto 连带把 overflow-x 的 visible 计算为 auto,
  // 超出网格 padding box 的 popover 会被裁剪。旧逻辑用 viewport 顶边判定
  // (aRect.top - pRect.height >= 8),第 1 行座位头像在 viewport 中上方空间充足
  // → 判定向上展开 → 被网格上边界裁掉。现改为:取网格 bounding rect 与
  // viewport 的交集为有效边界,**默认向下**,仅当下方放不下且上方更宽时向上。
  useEffect(() => {
    if (!hovered) return;
    const avatarEl = avatarRef.current;
    const popoverEl = popoverRef.current;
    if (!avatarEl || !popoverEl) return;
    const aRect = avatarEl.getBoundingClientRect();
    const pRect = popoverEl.getBoundingClientRect();
    const gridEl = avatarEl.closest('.werewolf-table') as HTMLElement | null;
    const gRect = gridEl ? gridEl.getBoundingClientRect() : null;
    const boundTop = Math.max(gRect ? gRect.top : 0, 0);
    const boundBottom = Math.min(gRect ? gRect.bottom : window.innerHeight, window.innerHeight);
    const boundLeft = Math.max(gRect ? gRect.left : 0, 0);
    const boundRight = Math.min(gRect ? gRect.right : window.innerWidth, window.innerWidth);
    // 垂直方向:默认向下;下方剩余空间不足且上方更宽 → 翻转到向上。
    const spaceBelow = boundBottom - aRect.bottom - 6;
    const spaceAbove = aRect.top - boundTop - 6;
    const openAbove = pRect.height > spaceBelow && spaceAbove > spaceBelow;
    // 水平方向:居中展开(popover 宽 ~280),越出有效边界则贴左/贴右。
    const centeredLeft = aRect.left + aRect.width / 2 - pRect.width / 2;
    const openLeft = centeredLeft < boundLeft + 8;
    const openRight = centeredLeft + pRect.width > boundRight - 8;
    setPlacement({
      vertical: openAbove ? 'above' : 'below',
      horizontal: openLeft ? 'left' : openRight ? 'right' : 'center',
    });
  }, [hovered, history, reason, label, emotionKey]);

  const fxClass = [
    'ww-fx',
    active && effect ? `ww-fx--${effect}` : '',
    active && intensity ? `ww-fx--${intensity}` : '',
    !active ? 'ww-fx--breathe' : '',
  ].filter(Boolean).join(' ');

  // 2026-08-05 — hover popover 内容拼装:相对时间 + 情绪历史倒序(最新在前)。
  const relTime = updatedAtMs ? relTimeShort(updatedAtMs, nowMs) : '';
  const hist = history && history.length > 0
    ? [...history].sort((a, b) => b.at_ms - a.at_ms).slice(0, 5)
    : [];

  return (
    <div
      ref={avatarRef}
      className="ww-emotion-avatar"
      data-testid={`seat-emotion-${seatIdx}`}
      title={label ? `${label} | ${reason || ''}` : undefined}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      {showEmojiFallback ? (
        <span className="ww-emotion-avatar__emoji" aria-hidden="true">{emoji}</span>
      ) : (
        <img
          className="ww-emotion-avatar__img"
          src={imgSrc}
          alt={label || 'emotion'}
          onError={() => setImgFailed(true)}
          draggable={false}
        />
      )}
      <div
        className={fxClass}
        style={emotionColor ? ({ ['--emotion-color' as any]: emotionColor }) : undefined}
        aria-hidden="true"
      >
        {active && (effect === 'sweat' || effect === 'tears') && (
          <>
            <span className="ww-fx-particle ww-fx-particle--1" />
            <span className="ww-fx-particle ww-fx-particle--2" />
            <span className="ww-fx-particle ww-fx-particle--3" />
          </>
        )}
        {active && effect === 'spin_question' && (
          <>
            <span className="ww-fx-particle ww-fx-particle--q1">?</span>
            <span className="ww-fx-particle ww-fx-particle--q2">?</span>
          </>
        )}
        {active && effect === 'drowsy' && (
          <>
            <span className="ww-fx-particle ww-fx-particle--z1">Z</span>
            <span className="ww-fx-particle ww-fx-particle--z2">z</span>
            <span className="ww-fx-particle ww-fx-particle--z3">z</span>
          </>
        )}
      </div>
      {active && caption && (
        <div className="ww-fx-caption">{caption}</div>
      )}
      {/* 2026-08-05 — hover 情绪信息 popover。CSS .ww-emotion-avatar:hover 兜底
          (JS 状态仅驱动关闭延迟);此处渲染 DOM,用 data-open 属性控制显隐,
          让鼠标短暂离开头像进入 popover 时不闪断。
          §20260811-06 U2 — 垂直/水平 placement 由 JS 动态决定(openAbove/openLeft/openRight),
          保证 13 人局 2 列网格 hover 时 popover 永远完整可见在 viewport 内。 */}
      <div
        ref={popoverRef}
        className={[
          'ww-emotion-popover',
          hovered ? 'is-open' : '',
          `ww-emotion-popover--${placement.vertical}`,
          `ww-emotion-popover--${placement.horizontal}`,
        ].filter(Boolean).join(' ')}
        role="tooltip"
        aria-hidden={!hovered}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      >
        <div className="ww-emotion-popover__head">
          <span className="ww-emotion-popover__emoji" aria-hidden="true">{emoji}</span>
          <span className="ww-emotion-popover__label">{label || emotionKey || '—'}</span>
        </div>
        {reason && <div className="ww-emotion-popover__reason">{reason}</div>}
        {relTime && <div className="ww-emotion-popover__time">🕑 {relTime}</div>}
        {hist.length > 0 && (
          <ul className="ww-emotion-popover__history">
            {hist.map((h, i) => {
              const hm = getEmotionMeta(h.emotion);
              return (
                <li key={`${h.at_ms}-${i}`} className="ww-emotion-popover__history-item">
                  <span aria-hidden="true">{hm?.emoji ?? '🙂'}</span>
                  <span className="ww-emotion-popover__history-label">{hm?.label ?? h.emotion}</span>
                  <span className="ww-emotion-popover__history-time">{relTimeShort(h.at_ms, nowMs)}</span>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}

/** 相对时间简短文案(用于 popover 的情绪切换时间/历史)。 */
function relTimeShort(atMs: number, nowMs: number): string {
  const diffSec = Math.max(0, Math.round((nowMs - atMs) / 1000));
  if (diffSec < 5) return '刚刚';
  if (diffSec < 60) return `${diffSec} 秒前`;
  const min = Math.floor(diffSec / 60);
  if (min < 60) return `${min} 分钟前`;
  const hr = Math.floor(min / 60);
  return `${hr} 小时前`;
}
