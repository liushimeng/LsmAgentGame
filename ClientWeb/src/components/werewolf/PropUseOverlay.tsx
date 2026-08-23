/**
 * PropUseOverlay.tsx — 2026-07-23 §道具特效:道具使用视觉特效叠加层。
 *
 * 监听 store 的 lastPropEvent/lastPropSeq:当一条语义新道具使用事件到来时,
 * 在屏幕中央展示一次"道具命中/落空"特效,展示 ≤4 秒且可点击关闭
 * (§20260823-02 P6,原 12 秒无关闭途径),明确表达:
 *   - 谁(from_seat / from_account)给谁(target_seat / AOE)使用了什么道具;
 *   - 道具名称 + 独特 emoji + 配色(每种道具一色);
 *   - 是否中招(hit/miss) + 效果摘要(effect_text)。
 *
 * 触发源:后端 broadcastPropUseLocked → main.go 注册 onPropUsed 回调 →
 * BroadcastRoom(game.werewolf_prop_used) → useWerewolf.ts 解析 →
 * appendPropEvent → store.lastPropEvent/lastPropSeq 更新 → 本组件 useEffect 触发。
 *
 * 设计要点(对齐 CLAUDE.md §124 DayNightOverlay 教训):
 *   - position:absolute; inset:0; pointer-events:none — 绝不阻塞页面交互;
 *   - z-index 取 --ww-z-prop-fx(1100,2026-07-24 z-token 化),高于游戏内一切层
 *     (含 sheriff-stream/idiot-reveal 的 --ww-z-critical=1000 与 DayNightOverlay
 *     的 --ww-z-fx=100);GlobalToast 挂 body 根(z=1000)不受本层影响,始终可点;
 *   - 自动消失 + setTimeout 清理,避免残留;
 *   - 同一事件去重(by seq),不重复播放。
 *
 * 配色 + 动画通过 globals.css .ww-prop-overlay-* 类 + CSS 变量(--ww-prop-r/g/b)
 * 按 prop_key 切换;本文件只负责触发与布局。
 */

import { useEffect, useState, useMemo } from 'react';
import { useWerewolfStore } from '@/store/werewolf.store';
import { useT } from '@/hooks/useT';

interface Props {
  /** 当前玩家列表(用于把 from_account/userID 解析为座位显示名)。可选。 */
  players?: Array<{ seat: number; name?: string } | null | undefined>;
}

// ── 每种道具的独特视觉身份(配色 + 运动关键词)──────────────────────────────
// 色值与 docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md 第六类 LLM 注入攻击手法对应:
//  markdown_bomb=Markdown注入(紧急红) nested_maze=套娃(幻紫)
//  char_confuse=字符欺骗(混乱青) long_swear=注意力失焦(卷轴金)
//  task_disguise=任务马甲(马戏橙) task_disguise_v3=进阶马甲(影视洋红)
//  emotion_plea=情绪操控(哀求粉)
interface PropTheme {
  /** RGB 自定义属性值(三位空格分隔,供 rgb(var(--ww-prop-r) ...) 使用)。 */
  r: number;
  g: number;
  b: number;
  /** 主 emoji。 */
  emoji: string;
  /** globals.css 内的动效类。 */
  motion: string;
  /** 道具中文名后备(若后端未下发 prop_name)。 */
  fallbackName: string;
}

const PROP_THEMES: Record<string, PropTheme> = {
  markdown_bomb:   { r: 231, g: 76,  b: 60,  emoji: '📰', motion: 'ww-prop-motion--burst',    fallbackName: '紧急公告' },
  nested_maze:     { r: 155, g: 89,  b: 182, emoji: '🎭', motion: 'ww-prop-motion--spin',    fallbackName: '剧本迷宫' },
  char_confuse:    { r: 36,  g: 204, b: 240, emoji: '🔣', motion: 'ww-prop-motion--glitch',  fallbackName: '胡言乱语' },
  long_swear:      { r: 241, g: 196, b: 15,  emoji: '📜', motion: 'ww-prop-motion--unfurl',  fallbackName: '长篇废话' },
  task_disguise:   { r: 230, g: 126, b: 34,  emoji: '🎪', motion: 'ww-prop-motion--theater', fallbackName: '编剧委托' },
  task_disguise_v3:{ r: 219, g: 68,  b: 158, emoji: '🎬', motion: 'ww-prop-motion--cinema',  fallbackName: '编剧委托·进阶' },
  emotion_plea:    { r: 244, g: 114, b: 182, emoji: '🥺', motion: 'ww-prop-motion--heartbeat',fallbackName: '苦苦哀求' },
};

function themeFor(key: string): PropTheme {
  return PROP_THEMES[key] ?? {
    r: 160, g: 160, b: 160, emoji: '🎴', motion: 'ww-prop-motion--burst', fallbackName: '道具',
  };
}

// 判断字符串是否为 UUID 形态(8-4-4-4-12,36 位含 4 个连字符)。
// 后端对真人玩家下发 from_account 为原始 userID(UUID),不可直接展示,需回退座位号。
function looksLikeUUID(s: string): boolean {
  return /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/.test(s);
}

/** 展示时长(ms):进场 300 + 保持 3400 + 退场 300 ≈ 4秒。
 *  2026-08-23 §20260823-02 P6 — 不遮挡原则:≤4s 自动消失 + 可点击 ✕/卡片本体
 *  立即关闭(原 12 秒无关闭途径,长时间遮挡屏幕中央)。 */
const DISPLAY_MS = 4000;

export function PropUseOverlay({ players }: Props) {
  const t = useT();
  const lastPropEvent = useWerewolfStore((s) => s.lastPropEvent);
  const lastPropSeq = useWerewolfStore((s) => s.lastPropSeq);
  const setPropTargetSeat = useWerewolfStore((s) => s.setPropTargetSeat);
  const [active, setActive] = useState<typeof lastPropEvent>(null);
  /** 已播放的事件 seq 哨兵,防止重复触发;手动关闭后同 seq 不复活。 */
  const [shownSeq, setShownSeq] = useState<number>(-1);

  useEffect(() => {
    if (!lastPropEvent) return;
    if (lastPropSeq === shownSeq) return; // 已播放过(含手动关闭,不复活)
    setActive(lastPropEvent);
    setShownSeq(lastPropSeq);
    // 2026-07-23 §道具特效:写入目标座位高亮(AOE 时 target_seat=-1 → 复位,不特指单座)。
    setPropTargetSeat(lastPropEvent.target_seat);
    const timer = setTimeout(() => {
      setActive(null);
      setPropTargetSeat(-1); // 展示结束,复位高亮
    }, DISPLAY_MS);
    return () => clearTimeout(timer);
  }, [lastPropEvent, lastPropSeq, shownSeq, setPropTargetSeat]);

  // §20260823-02 P6 — 手动关闭:active 置空 + 复位座位高亮;shownSeq 已在
  // 触发时写入,故同一 seq 不会因本关闭而复弹,新事件(seq 变化)正常弹出。
  const dismiss = () => {
    setActive(null);
    setPropTargetSeat(-1);
  };

  // 解析"来源座位名" + "目标座位名"。后端对 Agent 下发模型名(如"小米 mimo-v2.5"),
  // 对真人下发原始 userID(UUID)——二者都走 players 数组反查为可读昵称/编号,
  // 避免真人施法时渲染 UUID (BUG-R241-P3-01)。
  const label = useMemo(() => {
    if (!active) return null;
    // 优先用 players 反查: 真人 → 账号名(如 test_01),Agent → 配置的昵称。
    // players 不可用或未命中时,from_account 为模型名(Agent)则直接用,
    // 为 UUID(真人)则回退座位号,绝不渲染原始 UUID。
    let fromLabel = `${active.from_seat + 1}号`;
    const fp = players?.find((p) => p && p.seat === active.from_seat);
    if (fp?.name && fp.name.length > 0) {
      fromLabel = fp.name;
    } else if (active.from_account && active.from_account.length > 0 && !looksLikeUUID(active.from_account)) {
      fromLabel = active.from_account;
    }
    let toLabel: string;
    if (active.target_seat < 0) {
      toLabel = t('werewolf.propOverlay.targetAOE') || '所有玩家';
    } else if (players) {
      const tp = players.find((p) => p && p.seat === active.target_seat);
      toLabel = tp?.name && tp.name.length > 0 ? tp.name : `${active.target_seat + 1}号`;
    } else {
      toLabel = `${active.target_seat + 1}号`;
    }
    return { fromLabel, toLabel };
  }, [active, players, t]);

  if (!active || !label) return null;

  const theme = themeFor(active.prop_key);
  const propName = (active.prop_name && active.prop_name.length > 0)
    ? active.prop_name
    : theme.fallbackName;

  const rootStyle = {
    '--ww-prop-r': theme.r,
    '--ww-prop-g': theme.g,
    '--ww-prop-b': theme.b,
  } as React.CSSProperties;

  return (
    <div
      className={`ww-prop-overlay ${theme.motion}`}
      style={rootStyle}
      role="presentation"
      data-testid="werewolf-prop-overlay"
      data-prop-key={active.prop_key}
    >
      {/* 全屏暗化背板(半透明,仅弱化背景让主体突出;pointer-events:none 透传)。 */}
      <div className="ww-prop-overlay__dim" />
      {/* §20260823-02 P6 — 特效卡片 pointer-events:auto(容器仍 none,不挡座位
          点击);点卡片本体或右上角 ✕ 立即关闭。 */}
      <div
        className="ww-prop-overlay__center"
        role="button"
        tabIndex={0}
        aria-label={t('werewolf.panel.close')}
        onClick={dismiss}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            dismiss();
          }
        }}
      >
        <button
          type="button"
          className="ww-prop-overlay__close"
          aria-label={t('werewolf.panel.close')}
          title={t('werewolf.panel.close')}
          data-testid="werewolf-prop-overlay-close"
          onClick={(e) => {
            e.stopPropagation();
            dismiss();
          }}
        >
          ✕
        </button>
        {/* 扩散光环 */}
        <div className="ww-prop-overlay__ring" />
        {/* 道具 emoji + 主色圆盘 */}
        <div className="ww-prop-overlay__disc">
          <span className="ww-prop-overlay__emoji" aria-hidden="true">
            {active.prop_emoji && active.prop_emoji.length > 0 ? active.prop_emoji : theme.emoji}
          </span>
        </div>
        {/* 名称 + 方向(谁→谁) + 中招标记 */}
        <div className="ww-prop-overlay__content">
          <div className="ww-prop-overlay__name">{propName}</div>
          <div className="ww-prop-overlay__arrow">
            <span className="ww-prop-overlay__seat">{label.fromLabel}</span>
            <span className="ww-prop-overlay__arrow-glyph">→</span>
            <span className="ww-prop-overlay__seat">{label.toLabel}</span>
          </div>
          <div
            className={`ww-prop-overlay__badge ${active.hit ? 'is-hit' : 'is-miss'}`}
            data-testid="werewolf-prop-overlay-badge"
          >
            {active.hit
              ? (t('werewolf.propOverlay.badgeHit') || '✓ 中招')
              : (t('werewolf.propOverlay.badgeMiss') || '✗ 未中招')}
          </div>
          {active.effect_text && active.effect_text.length > 0 && (
            <div className="ww-prop-overlay__effect">{active.effect_text}</div>
          )}
        </div>
      </div>
    </div>
  );
}

export default PropUseOverlay;
