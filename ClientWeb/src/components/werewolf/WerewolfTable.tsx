/**
 * 狼人杀 13 人标准竞技局牌桌渲染 (历史兼容 12/7 人局)
 *
 * §126 重构 - 表格布局替代环形布局:
 *   - 13 个座位按 7 + 6 两行 CSS Grid 表格渲染,充分利用矩形屏幕空间
 *   - 每张座位卡包含:头像 + 角色卡 + 警长/白痴/verdict 徽章 + Agent 实时状态
 *   - Agent 实时状态(LLM 调用中/情绪/决策摘要/内心独白)直接渲染在座位卡内,
 *     不再依赖左侧 sidebar 的折叠面板
 *   - 「我方在底部」语义保留:自己的座位永远在表格末行,即便其他座位均匀分布
 *
 * 视觉:
 *   - 背景:dark_medieval bg.png + 月光晕
 *   - 玩家头像:角色卡 PNG(已死=灰度 + 帧);白痴立绘为占位素材
 *   - 角色身份:活着时仅自己可见自己;死后全场公开
 *   - 警长 ★ 徽章 / 白痴翻牌 🃏 徽章
 *   - HUD:阶段 + 日夜 icon + 死者名牌 + 投票数
 */

import { useMemo, useState, useEffect, useRef } from 'react';
import { useWerewolfStore } from '@/store/werewolf.store';

// 2026-07-17: 投票原因中文标签(与 NightActionPanel 共享)。
function tallyReasonLabel(reason: string): string {
  switch (reason) {
    case 'majority': return '多数决';
    case 'random_tie_break': return '平票随机';
    case 'random_all_abstain': return '全弃权随机';
    default: return reason;
  }
}

import type {
  WerewolfGameState,
  WerewolfRole,
  DeadPlayerJSON,
  BotContextJSON,
  InfluenceScoreJSON,
} from '@/types/werewolf';
import { roleImageByKey } from '@/assets/images/werewolf';
import { useT } from '@/hooks/useT';
import { phaseLabel, phaseIcon } from '@/components/werewolf/phaseLabel';
import { DayNightOverlay } from '@/components/werewolf/DayNightOverlay';
import { SheriffElectedOverlay } from '@/components/werewolf/SheriffElectedOverlay';
import { BotPhaseIndicator } from '@/components/werewolf/BotPhaseIndicator';
import { formatK } from '@/shared/utils/format';
// §128 对话即思考重构:WerewolfThinkingHeader 已删除(信息合并到 WerewolfStatusBar)。
import { AgentCallTimeBadge } from '@/components/werewolf/AgentCallTimeBadge';
import { IdentityGuessBadge } from '@/components/werewolf/IdentityGuessBadge';
import { EmotionAvatar } from '@/components/werewolf/EmotionAvatar';
import { modelStyleOf } from '@/components/werewolf/modelStyle';

interface WerewolfTableProps {
  gameState: WerewolfGameState;
  mySeat: number;
  /** 2026-07-22 §任务2 — 用户身份猜测持久化 hook(由父 GamePage 注入)。
   *  - guesses: seat -> role|null
   *  - guessableRoles: 可猜测角色列表
   *  - onChange: 写入回调
   *  透传给 SeatCell 内的 <IdentityGuessBadge />;未传则不渲染猜测 UI。
   */
  identityGuess?: {
    guesses: Record<number, WerewolfRole | null>;
    guessableRoles: WerewolfRole[];
    onChange: (seat: number, role: WerewolfRole | null) => void;
  };
  /** 观战者模式:true 时所有玩家身份强制隐藏(显示为「未知」),不依赖后端剥离(纵深防御)。
   *  对齐 BUG-R204-SEC-01 修复要求:观战者只能看到存活/死亡状态、发言、投票行为。 */
  spectator?: boolean;
}

const NIGHT_PHASES = new Set(['night_wolves', 'night_seer', 'night_witch']);

// ─────────────────────────────────────────────────────────────────────────
// 2026-08-05 §Agent聊天显示优化 — 座位卡「最后一次发言」气泡辅助函数。
//
// 数据源 BotContextJSON.last_speech* 4 字段是**已经广播成功的公开发言**
// (公屏上本就看得到),因此无需 spectator 守卫;与 §119/§128 协议层隔离的
// heart_thought(仅观战者可见、绝不进公屏)语义完全不同,不要混用。
//
// 2026-08-05 §02 — 气泡升级为**双源**:bot_contexts 只覆盖 bot 座位,真人玩家
// 永远拿不到气泡;因此补 WerewolfPlayerJSON.last_speech*(座位级、人机统一)
// 作为真人源与兜底源。两源同为公开发言,spectator 守卫结论不变(都不需要)。
// ─────────────────────────────────────────────────────────────────────────

/** 发言来源 → emoji 徽章。未知 kind 回落 💬。 */
function speechKindIcon(kind?: string): string {
  switch (kind) {
    case 'speak': return '💬';
    case 'emotion_speak': return '🎭';
    case 'interject': return '💢';
    case 'whisper': return '🔒';
    case 'last_words': return '💀';
    default: return '💬';
  }
}

/** 发言来源 → 中文标签(与 SeatCell 内其余内联中文风格一致)。 */
function speechKindLabel(kind?: string): string {
  switch (kind) {
    case 'speak': return '发言';
    case 'emotion_speak': return '表情发言';
    case 'interject': return '插话';
    case 'whisper': return '私聊';
    case 'last_words': return '遗言';
    default: return '发言';
  }
}

/** 相对时间:`刚刚` / `12s前` / `3分前` / `1时前`。nowMs 缺失时返回空串。 */
function relativeTimeLabel(atMs?: number, nowMs?: number): string {
  if (!atMs || !nowMs) return '';
  const diff = Math.max(0, nowMs - atMs);
  const sec = Math.floor(diff / 1000);
  if (sec < 1) return '刚刚';
  if (sec < 60) return `${sec}s前`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}分前`;
  return `${Math.floor(min / 60)}时前`;
}

/** 新发言高亮窗口(ms)—— 与 werewolf-speech.css 的 .is-fresh 动画时长一致。 */
const SPEECH_FRESH_MS = 3000;
/** 父级 1s tick 的发言侧启动窗口(ms):5 分钟内有发言就保持相对时间自更新。 */
const SPEECH_TICK_WINDOW_MS = 5 * 60 * 1000;

// 2026-08-04 §重构 — emotion metadata 抽到 utils/werewolfEmotion.ts,共享引用。
// 配色保持原 §优化-20260730-01 方案(深底浅字,WCAG ≥ 4.5:1)。
// 2026-08-04 §表情特效 — 座位卡 emotion 徽章已删除,getEmotionMeta 的消费方
// 迁移到 EmotionAvatar(描边色/emoji 降级) + FactionDrawer/HistoryDrawer。

// §优化-20260730-01 — 根据视口宽度自适应选择列数,使每张座位卡在所有断点下
// 都有合理宽度,不重叠。
// §优化-20260802-01 — 列数收敛为 2(严格 2 列/行,≤600px 落 1 列)。
// 返回二维数组 rows,每行 cols 个座位(最后一行可能 < cols)。
function buildGridOrder(total: number, mySeat: number, cols: number): number[][] {
  const N = Math.max(1, Math.min(6, cols | 0));
  if (mySeat < 0) {
    // spectator:按 0..total-1 自然顺序填 N 列多行。
    const rows: number[][] = [];
    for (let i = 0; i < total; i += N) {
      rows.push(Array.from({ length: Math.min(N, total - i) }, (_, k) => i + k));
    }
    return rows;
  }
  // 玩家:旋转使 mySeat 排在最后一行末位;其余按 (mySeat+1) 起顺时针排列。
  const all: number[] = [];
  for (let i = 0; i < total; i++) all.push((mySeat + 1 + i) % total);
  const rows: number[][] = [];
  // 最后一行仅放 mySeat,前 (total-1) 个按每行 N 个分配。
  const headCount = total - 1;
  for (let i = 0; i < headCount; i += N) {
    rows.push(all.slice(i, i + N));
  }
  rows.push([mySeat]);
  return rows;
}

// §优化-20260802-01 — 严格 2 列/行:用户反馈 3+ 列下座位卡内 5 行 Agent 指标
// (情绪/延迟/调用/token/模型名)挤压重叠、显示不全;2 列让每张卡 ≥ ~560px
// (1280 视口),5 行结构完整舒展。仅极窄屏(≤600)落 1 列竖排。
// 组件挂载时调用一次,resize 时由 React effect 触发重渲染。
// §2026-08-06 — 宽度来源改为 board-container(通过 ResizeObserver),
// 侧栏折叠后 container 变宽,自动增加每行列数让 13 人局展示更多角色。
//   ≤360px  → 1 列(极窄手机竖屏)
//   ≤700px  → 2 列(典型手机/窄屏)
//   ≤1100px → 3 列(1280 视口、单侧栏折叠后常见宽度)
//   >1100px → 4 列(两侧栏全折叠后的大宽域)
function calcColsForViewport(width: number): number {
  if (width <= 360) return 1;
  if (width <= 700) return 2;
  if (width <= 1100) return 3;
  return 4;
}
function calcCompactForViewport(width: number): boolean {
  // §2026-08-06 — container 宽 ≤480 时启用紧凑模式(1-2 列下卡片仍窄)。
  // 原 768 窗口宽判断改为 480 container 宽,因为侧栏折叠后 board-container
  // 在 1280 视口下约 700px,不应触发 compact。
  return width <= 480;
}

// 单座位卡组件 — 把 §123 verdict 徽章 + §119 内心独白 + §120 LLM 状态 +
// §124 情绪徽章 + Agent 决策摘要全部内嵌,替代原左侧折叠面板的零散信息。
//
// 2026-07-10 §重构 — Agent 思考指示器升级为多态 BotPhaseIndicator,接收父级
// nowMs prop 实现倒计时实时刷新;不再依赖 gameState.bot_contexts 单帧同步,
// 解决"13 bot 全部空转 LLM 时前端看不到开始信号"的死锁问题。
interface SeatCellProps {
  seatIdx: number;
  player: WerewolfGameState['players'][number];
  botCtx?: BotContextJSON;
  isMySeat: boolean;
  isDead: boolean;
  revealed: boolean;
  isIdiotRevealed: boolean;
  deadInfo?: DeadPlayerJSON;
  /** §128 — 是否最后一行(用于增强「我方在底部」的视觉强调) */
  isLastRow?: boolean;
  /** §重构 — 父组件 1s setInterval 提供的当前 unix ms,
   *  让 BotPhaseIndicator 倒计时实时刷新。null 表示父级无活跃指示器,
   *  子组件应跳过 BotPhaseIndicator 渲染。 */
  nowMs?: number;
  /** 2026-07-22 §任务2 — 用户猜测 + 可猜测角色列表(由父级 useIdentityGuess 注入)。
   *  可选:未传则 SeatCell 不渲染猜测 UI(观众/无 hook 时降级)。 */
  identityGuess?: {
    guesses: Record<number, WerewolfRole | null>;
    guessableRoles: WerewolfRole[];
    onChange: (seat: number, role: WerewolfRole | null) => void;
  };
  /**
   * 2026-07-23 §道具特效:当前被道具击中的目标座位号。
   * -1 = 无高亮;>= 0 = 该座位需脉冲高亮。由父 WerewolfTable 从 store 注入。
   */
  propTargetSeat?: number;
  /**
   * 观战者模式:true 时强制隐藏身份(显示「未知」),纵深防御。
   * 对齐 BUG-R204-SEC-01:观战者只应看到存活/死亡状态、发言、投票行为。
   *
   * BUG-R233-P2-01 (2026-08-02):spectator=true + gameStatus="over" 时**例外**
   * 放行终局全员亮牌,与 §135 RolePubliclyRevealed() clause ① + §129 冷却期
   * 复盘语义对齐。其它状态(spectator=true + gameStatus="playing")保持
   * R204 纵深防御不变。
   */
  spectator?: boolean;
  /**
   * BUG-R233-P2-01 (2026-08-02): 父级 WerewolfTable 注入当前 gameState.status
   * 字符串('playing' | 'over' | 其它)。非 'over' 时 spectator 仍然隐藏身份。
   */
  gameStatus?: string;
  /**
   * §优化-20260730-01 — 紧凑模式:true 时座位卡内 Agent 5 行结构折叠为 3 行,
   * 仅保留核心(座位号/角色/情绪+模型);1280px 以下自动开启。
   * 真人玩家永远只显示 3 行,与本参数无关。
   */
  compact?: boolean;
  /**
   * 2026-08-08 §20260808-02 — 遗言阶段当前发言座位高亮。
   * phase==='death_lyric' 且 seatIdx === phase_extra.death_lyric.current_seat 时
   * 追加 werewolf-seat--last-words-speaking(紫灰光晕 + 🕯 角标,CSS box-shadow
   * 不受 night brightness 滤镜衰减 —— §26.2 反模式 4)。由父级 WerewolfTable
   * 从 gameState 透传,SeatCell 不直接消费 gameState。
   */
  isLastWordsSpeaking?: boolean;
  /**
   * §20260811-02 U1 — 本座位的发言影响力分数。
   * 全员可见(分数只由公开信息计算,不含角色信息);undefined = 尚未计算
   * (第 1 天首次投票结算前),此时不渲染徽章。
   */
  influence?: InfluenceScoreJSON;
}

/** §20260811-02 U1 — 影响力分档(⭐高 / ◉中 / ○低),与 CSS 类名 is-* 对应。 */
function influenceTier(total: number): 'high' | 'mid' | 'low' {
  if (total >= 60) return 'high';
  if (total >= 30) return 'mid';
  return 'low';
}

/** §20260811-02 U1 — 影响力分档图标。 */
function influenceIcon(total: number): string {
  if (total >= 60) return '⭐';
  if (total >= 30) return '◉';
  return '○';
}

const SeatCell: React.FC<SeatCellProps> = ({
  seatIdx, player, botCtx, isMySeat, isDead, revealed,
  isIdiotRevealed, deadInfo, isLastRow, nowMs, identityGuess,
  propTargetSeat = -1,
  gameStatus = 'playing',
  spectator = false,
  compact = false,
  isLastWordsSpeaking = false,
  influence,
}) => {
  const t = useT();
  // §R178-OBS1: 区分「空座位(无人)」与「死亡玩家」 — 后端 view.go:357 对空座位
  // 输出 alive=false,但该座位既无 user_id 也无角色,渲染为「已死亡」是误导。
  // 修正:仅在 user_id 非空时显示死亡遮罩;空座位显空状态。
  const isEmptySeat = !player.user_id;
  const showDead = isDead && !isEmptySeat;
  // BUG-R204-SEC-01 纵深防御: 观战者模式下强制不揭示身份,即便后端误发 role/role_revealed 也显示「未知」。
  // 观战者只应看到存活/死亡状态、发言、投票行为,绝不看到角色名称与阵营。
  //
  // BUG-R233-P2-01 (2026-08-02): 终局例外 — §135 RolePubliclyRevealed() clause ①
  // 在 Status="over" 时全员亮牌(单一事实来源),让观战者能在 §129 冷却期复盘
  // BotTranscript / 整局流程。修复:对局进行中保持 R204 纵深防御(spectator?false)
  // 不动,只对终局(已 status==='over')放行——与 HistoryDrawer 同源,确保观战者
  // 视图与历史抽屉的「终局全员亮牌」语义一致。
  const isGameOver = gameStatus === 'over';
  const displayRevealed = spectator ? (isGameOver && revealed) : revealed;
  // R185 中文化: 角色枚举 → 中文文案(走 werewolf.role.* i18n); 未揭示占位符用「未知」。
  const roleText = player.role ? t(`werewolf.role.${player.role}` as any) : '?';
  const unknownRole = t('werewolf.role.unknown' as any);
  const isLLMCalling = botCtx?.llm_call_in_progress === true;
  const isQuarantined = botCtx?.quarantined === true;
  const decision = botCtx?.last_decision_summary || '';
  const verdict = deadInfo?.verdict;
  // §重构 — 决定是否渲染 BotPhaseIndicator:有活跃 phase 或被 quarantine。
  const phase = botCtx?.llm_call_phase ?? 'idle';
  const hasPhaseIndicator =
    isLLMCalling ||
    phase === 'calling' ||
    phase === 'streaming' ||
    phase === 'retrying' ||
    phase === 'quarantined' ||
    isQuarantined;
  // 2026-07-23 §道具特效:当前座位是道具目标时,加脉冲高亮类。
  const isPropTarget = propTargetSeat >= 0 && seatIdx === propTargetSeat;
  // §优化-20260730-01-P1-3 — 提取 Agent 关键指标供横版布局 5 行结构使用。
  const lastLatencySec = botCtx?.last_llm_latency_ms !== undefined
    ? (botCtx.last_llm_latency_ms / 1000).toFixed(1) : null;
  const avgLatencySec = botCtx?.avg_llm_latency_ms !== undefined
    ? (botCtx.avg_llm_latency_ms / 1000).toFixed(1) : null;
  const totalCalls = botCtx?.total_llm_calls;
  // 决策摘要横版显示 18 字截断;保留 title 显示完整。
  const decisionShort = decision && decision.length > 18
    ? decision.slice(0, 18) + '…' : decision;
  // 2026-07-30 §统计增强 — Token 统计（含缓存命中检测）。
  const totalInTokens = botCtx?.total_input_tokens;
  const totalOutTokens = botCtx?.total_output_tokens;
  const lastInTokens = botCtx?.last_input_tokens;
  const hasTokenStats = (totalInTokens !== undefined && totalInTokens > 0)
    || (totalOutTokens !== undefined && totalOutTokens > 0)
    || (lastInTokens !== undefined && lastInTokens === 0 && totalOutTokens !== undefined);
  const isCacheHit = lastInTokens === 0 && totalOutTokens !== undefined && totalOutTokens > 0;

  // 2026-08-05 §Agent聊天显示优化 — 发言气泡派生值。
  // last_speech 是公开数据(公屏已可见),不加 spectator 守卫;
  // whisper 后端只记 kind 不记原文,此时渲染 (私聊) 占位而非空白气泡。
  //
  // 2026-08-05 §02 — 气泡改为**双源合并**,让真人玩家座位卡也有气泡:
  //   · Agent 源 botCtx.last_speech*  → 全功能(kind 徽章 / 私聊占位 / streaming 光标);
  //   · 真人源 player.last_speech*    → 座位级字段,人机统一路径,无 kind(按 speak 💬 渲染)、无光标。
  // 两源同时存在时**取时间戳较新者**,避免 Agent 帧与座位帧节奏不一致时互相打架
  // (例如 bot_contexts 先于 players[] 到达,或反之)。仅一源时直接用该源。
  const agentSpeechAt = botCtx?.last_speech_at;
  const agentHasSpeech = !!botCtx?.last_speech || (botCtx?.last_speech_kind === 'whisper' && !!agentSpeechAt);
  const humanSpeechAt = player?.last_speech_at;
  const humanHasSpeech = !!player?.last_speech;
  // 择新:两源都有则比时间戳(缺失时间戳视作 0,让有时间戳的一方胜出)。
  const useAgentSource = agentHasSpeech
    && (!humanHasSpeech || (agentSpeechAt ?? 0) >= (humanSpeechAt ?? 0));

  const speechKind = useAgentSource ? botCtx?.last_speech_kind : undefined;
  const speechText = (useAgentSource ? botCtx?.last_speech : player?.last_speech) || '';
  const speechAt = useAgentSource ? agentSpeechAt : humanSpeechAt;
  const isWhisperSpeech = speechKind === 'whisper';
  const hasSpeech = !!speechText || (isWhisperSpeech && !!speechAt);
  // 父级 tick 在「全 idle 且 5 分钟内无发言」时停摆(nowMs=undefined),
  // 此时用本次渲染时刻兜底:相对时间不再自增,但不会退化成空白。
  const effectiveNow = nowMs ?? Date.now();
  const isFreshSpeech = hasSpeech && !!speechAt && effectiveNow - speechAt < SPEECH_FRESH_MS;
  // 2026-08-05 §02 — streaming 光标仅对 Agent 源有意义(真人不调 LLM)。
  const isStreamingSpeech = useAgentSource && phase === 'streaming';
  const speechRelTime = relativeTimeLabel(speechAt, effectiveNow);

  return (
    <div
      className={[
        'werewolf-seat',
        isMySeat ? 'is-self' : '',
        showDead ? 'is-dead' : '',
        isEmptySeat ? 'is-empty' : '',
        player.is_sheriff ? 'is-sheriff' : '',
        (hasPhaseIndicator && !isDead) ? 'is-llm-calling' : '',
        isQuarantined ? 'is-quarantined' : '',
        isLastRow ? 'is-last-row' : '',
        isPropTarget ? 'is-prop-target' : '',
        isLastWordsSpeaking ? 'werewolf-seat--last-words-speaking' : '',
      ].filter(Boolean).join(' ')}
      data-testid={`werewolf-seat-${seatIdx}`}
    >
      {/* §优化-20260730-01 — 横版布局:左侧 avatar 64×88,右侧 info 5 行结构。
          之前 1:1 垂直堆叠严重浪费 13 人局 4 列网格空间,现改为 row 方向
          充分利用每张座位卡 ~320px 宽度,信息密度 ×1.5。 */}
      <div className="werewolf-seat__avatar">
        {displayRevealed ? (
          <img src={roleImageByKey[player.role!]} alt={player.role} />
        ) : isEmptySeat ? (
          <div className="werewolf-seat__silhouette werewolf-seat__silhouette--empty" title="空座位">·</div>
        ) : (
          // 2026-08-04 §表情特效(设计 20260804-02):未揭示非空座位 → Agent 拟人化
          // 表情头像 + CSS 动态特效;真人玩家无 botCtx 也渲染(neutral 默认 + 呼吸)。
          <EmotionAvatar
            emotionKey={botCtx?.emotion}
            effect={botCtx?.emotion_effect}
            intensity={botCtx?.emotion_intensity}
            caption={botCtx?.emotion_caption}
            reason={botCtx?.emotion_reason}
            fxStartedAtMs={botCtx?.emotion_fx_started_at_ms}
            fxDurationMs={botCtx?.emotion_fx_duration_ms}
            updatedAtMs={botCtx?.emotion_updated_at}
            history={botCtx?.emotion_history}
            seatIdx={seatIdx}
          />
        )}
        {showDead && <div className="werewolf-seat__dead-mask">✝</div>}
        {showDead && verdict && (
          <div className={`werewolf-seat__verdict-badge werewolf-seat__verdict-badge--${verdict}`}
               title={verdict === 'execution' ? '处决' : '死亡'}>
            {verdict === 'execution' ? '⚖️' : '💀'}
          </div>
        )}
      </div>
      <div className="werewolf-seat__info">
        {/* 第 1 行:座位号 + 警长/我 + 情绪 emoji(大号 18px)— 顶行一眼可见 */}
        <div className="werewolf-seat__header">
          <span className="werewolf-seat__num">#{seatIdx + 1}</span>
          {isMySeat && <span className="werewolf-seat__self-badge">(我)</span>}
          {player.is_sheriff && <span className="werewolf-seat__sheriff-badge" title="警长">★</span>}
          {isIdiotRevealed && <span className="werewolf-seat__idiot-badge" title="白痴翻牌">🃏</span>}
          {/* §20260811-02 U1 — 发言影响力徽章。全员可见(分数只由公开信息计算,
              不含角色信息,§135 无关)。⭐≥60 / ◉30~59 / ○<30。 */}
          {influence && !isEmptySeat && (
            <span
              className={`werewolf-seat__influence is-${influenceTier(influence.total)}`}
              data-testid={`werewolf-influence-${seatIdx}`}
              title={t('werewolf.influence.tooltip')
                .replace('{total}', String(influence.total))
                .replace('{persuasion}', String(influence.persuasion))
                .replace('{attention}', String(influence.attention))
                .replace('{presence}', String(influence.presence))
                .replace('{survival}', String(influence.survival))
                .replace('{insight}', String(influence.insight ?? 0))}
            >
              {influenceIcon(influence.total)}{influence.total}
            </span>
          )}
        </div>
        {/* 第 2 行:角色身份 — 观战者/未揭示走"未知";死亡揭示走 is-revealed-truth */}
        <div className={`werewolf-seat__role ${displayRevealed ? 'is-revealed-truth' : ''}`}>
          {isEmptySeat ? '空' : (displayRevealed ? roleText : unknownRole)}
        </div>
        {/* 2026-08-04 §表情特效 — 座位卡 emotion 徽章(emoji+label 条)已删除,
            信息并入 EmotionAvatar 头像 tooltip;FactionDrawer/HistoryDrawer
            的 emoji 展示保留不变。 */}
        {/* 2026-08-05 §Agent聊天显示优化 — 第 3 行【主区】发言气泡。
            13px 2 行钳位 + title 挂全文;新发言 3s 金色高亮(.is-fresh);
            LLM streaming 时左侧 ▍ 闪烁光标(.is-streaming)。
            数据是**已广播的公开发言**,不需 spectator 守卫(§135 无关);
            §119/§128 的 heart_thought 依旧**不**在座位卡渲染。 */}
        {hasSpeech && (
          <div
            className={[
              'werewolf-seat__speech',
              isFreshSpeech ? 'is-fresh' : '',
              isStreamingSpeech ? 'is-streaming' : '',
            ].filter(Boolean).join(' ')}
            data-testid={`seat-speech-${seatIdx}`}
            title={speechText || speechKindLabel(speechKind)}
          >
            <div className="werewolf-seat__speech-body">
              <span className="werewolf-seat__speech-kind" aria-hidden="true">
                {speechKindIcon(speechKind)}
              </span>
              {isStreamingSpeech && (
                <span className="werewolf-seat__speech-cursor" aria-hidden="true">▍</span>
              )}
              {speechText ? (
                <span className="werewolf-seat__speech-text">{speechText}</span>
              ) : (
                // whisper:后端只记事件不记原文(私聊原文仅收发双方可见)。
                <span className="werewolf-seat__speech-text is-placeholder">(私聊)</span>
              )}
            </div>
            <div className="werewolf-seat__speech-meta">
              {speechRelTime && <span>{speechRelTime}</span>}
              <span>{speechKindLabel(speechKind)}</span>
            </div>
          </div>
        )}
        {/* 第 4 行:当前动作(决策摘要 18 字截断) — 替代原 24 字垂直块
            §优化-20260730-01 — compact 模式仅对 Agent 显示;真人玩家永远显示此行。 */}
        {!compact && decisionShort && (
          <span className="werewolf-seat__action" title={decision}>
            📤 {decisionShort}
          </span>
        )}
        {/* 2026-08-05 §Agent聊天显示优化 — 第 5 行【次区】指标合并单行。
            原「第 4 行 latency/calls/api」+「第 4.5 行 token」合并为同一行,
            数据一条不少(🐇 last · µ avg · ×calls · 📊 api · 🔤in/out · ⚡ 缓存),
            title tooltip 也合并保留;视觉权重由 CSS 降级为 10px muted。 */}
        {!compact && (lastLatencySec || avgLatencySec || totalCalls || botCtx?.api_call_count || hasTokenStats) && (
          <div className="werewolf-seat__metrics"
               title={[
                 lastLatencySec
                   ? `最后 ${lastLatencySec}s · 平均 ${avgLatencySec || '—'}s · 已调 ${totalCalls || 0} 次 · API ${botCtx?.api_call_count ?? 0} 次`
                   : '',
                 hasTokenStats
                   ? `Token 统计: 输入 ${totalInTokens ?? 0} · 输出 ${totalOutTokens ?? 0} · 总计 ${(totalInTokens ?? 0) + (totalOutTokens ?? 0)}`
                   : '',
               ].filter(Boolean).join('\n') || undefined}>
            {lastLatencySec && <span className="werewolf-seat__metric">🐇{lastLatencySec}s</span>}
            {avgLatencySec && <span className="werewolf-seat__metric">µ{avgLatencySec}s</span>}
            {totalCalls !== undefined && <span className="werewolf-seat__metric">×{totalCalls}</span>}
            {botCtx?.api_call_count !== undefined && (
              <span className="werewolf-seat__metric" title={`API 调用 ${botCtx.api_call_count} 次 (成功 ${botCtx.api_success_count ?? 0} / 失败 ${botCtx.api_fail_count ?? 0})`}>
                📊{botCtx.api_call_count}
              </span>
            )}
            {hasTokenStats && (
              <>
                <span className="werewolf-seat__token">🔤in:{formatK(totalInTokens)}</span>
                <span className="werewolf-seat__token">out:{formatK(totalOutTokens)}</span>
                {isCacheHit && <span className="werewolf-seat__cachebadge" title={t('werewolf.seat.cacheHit')}>⚡</span>}
              </>
            )}
          </div>
        )}
        {/* 第 5 行:模型名 + 最后调用时间 — 复用 AgentCallTimeBadge(nowMs)
            §优化-20260730-01 — compact 模式下 emotion 显示在模型名前;真人玩家仍保留 model 名。 */}
        <div className="werewolf-seat__model-row">
          {player.agent_name && (() => {
            // §20260811-08 U5 — 模型风格标识符。emoji 承担主要区分度
            // (不受 .is-night brightness(0.4) 衰减),色相走 box-shadow 光晕
            // 而非低透明度背景(§26.2 反模式 2/4)。
            const ms = modelStyleOf(player.agent_name);
            const school = t(`werewolf.modelstyle.${ms.schoolKey}` as any);
            return (
              <span
                className="werewolf-seat__model ww-model-style"
                style={{ ['--ww-model-glow' as any]: ms.glow }}
                title={`${player.agent_name} · ${school}`}
              >
                <span className="ww-model-style__emoji" aria-hidden="true">{ms.emoji}</span>
                {player.agent_name}
              </span>
            );
          })()}
          {!compact && botCtx && nowMs !== undefined && (
            <AgentCallTimeBadge ctx={botCtx} nowMs={nowMs} />
          )}
        </div>
        {/* LLM 5 态指示器 — 替代原 decision 行的位置,行高更紧凑
            §优化-20260730-01 — compact 模式仍保留指示器(高优先级 LLM 状态必须可见)。 */}
        {hasPhaseIndicator && !isDead && botCtx && nowMs !== undefined && (
          <BotPhaseIndicator
            bot={botCtx}
            nowMs={nowMs}
            testIdSuffix={String(seatIdx)}
          />
        )}
        {/* 兼容旧测试 ID — 部分 e2e 仍依赖 seat-calling-{seat}(隐藏元素,仅保留 testid) */}
        {isLLMCalling && phase === 'idle' && (
          <span
            data-testid={`seat-calling-${seatIdx}`}
            style={{ display: 'none' }}
            aria-hidden="true"
          />
        )}
      </div>
      {/* BUG-R204-SEC-01: 观战者模式下不暴露身份徽章(§135)
          Death+revealed 在卡片外层浮层显示。 */}
      {showDead && displayRevealed && !spectator && (
        <span
          className="werewolf-seat__reveal-badge"
          data-testid={`werewolf-reveal-badge-${seatIdx}`}
          title={t('werewolf.guess.revealedTruth' as any)}
        >
          ☠ {roleText}
        </span>
      )}
      {/* §128 对话即思考重构:SeatCell 内嵌 heart_thought 已删除(违反 §119 协议层隔离)。
          仅保留 FactionDrawer spectator 守卫版的 heart_thought 显示。 */}
      {/* 2026-07-22 §任务2 — 玩家身份猜测徽章 + 弹出层。
       *  - 仅活着的非自己座位显示(IdentityGuessBadge 内部已守护);
       *  - 死亡后由 .werewolf-seat__reveal-badge 接管"真实身份"展示,
       *    猜测徽章在 Death 路径会自己隐藏。 */}
      {identityGuess && !isEmptySeat && !isDead && (
        <IdentityGuessBadge
          seatIdx={seatIdx}
          mySeat={isMySeat ? seatIdx : -1}
          isAlive={!isDead}
          guess={identityGuess.guesses[seatIdx] ?? null}
          guessableRoles={identityGuess.guessableRoles}
          onChange={identityGuess.onChange}
        />
      )}
    </div>
  );
};

export function WerewolfTable({ gameState, mySeat, identityGuess, spectator = false }: WerewolfTableProps) {
  const t = useT();
  const players = gameState.players ?? [];
  const total = gameState.max_seat ?? 13;
  const phase = String(gameState.phase ?? 'filling');
  const isNight = NIGHT_PHASES.has(phase);

  // §126 §123 死亡信息:三源合并构建 seat → DeadPlayerJSON 映射。
  const deadInfoMap = useMemo(() => {
    const map = new Map<number, DeadPlayerJSON>();
    const all = gameState.all_dead_list_verbose ?? [];
    for (const d of all) if (d && !map.has(d.seat)) map.set(d.seat, d);
    const verbose = gameState.last_night_deaths_verbose ?? [];
    for (const d of verbose) map.set(d.seat, d);
    const extraList = (gameState as any).phase_extra?.dead_list as DeadPlayerJSON[] | undefined;
    if (Array.isArray(extraList)) {
      for (const d of extraList) map.set(d.seat, d);
    }
    return map;
  }, [
    gameState.all_dead_list_verbose,
    gameState.last_night_deaths_verbose,
    (gameState as any).phase_extra?.dead_list,
  ]);

  // 死亡名单(按座位排序),用于底部死亡名牌面板。
  const deadList = useMemo(
    () => Array.from(deadInfoMap.values()).sort((a, b) => a.seat - b.seat),
    [deadInfoMap],
  );
  const phaseLabelStr = phaseLabel(t, phase) ?? phase;
  const phaseEmoji = phaseIcon(phase);
  const showDay = phase !== 'filling' && phase !== 'over';
  // 2026-07-23 §道具特效:读取当前道具目标座位,透传给 SeatCell 做脉冲高亮。
  const propTargetSeat = useWerewolfStore((s) => s.propTargetSeat);
  // 2026-08-08 §20260808-02 — 遗言阶段当前发言座位(缺陷 A)。仅 death_lyric
  // 阶段有意义,其它阶段恒为 -1(无座位匹配,SeatCell 修饰类不追加)。
  const lastWordsCurrentSeat =
    phase === 'death_lyric'
      ? (gameState.phase_extra?.death_lyric?.current_seat ?? -1)
      : -1;

  // §130 — 座位按每行 4 列均匀分配,自己固定在最后一行末位。
  // §优化-20260730-01 — 视口断点监听:窗口 resize 时重算列数与 compact 模式。
  // §2026-08-06 — 改为 ResizeObserver 监听 .board-container 宽度:
  //   当左右侧栏折叠/展开时,container 宽度变化触发 cols 重算,
  //   让 13 人局座位网格自适应展示更多角色,而非固定 window.innerWidth。
  const gridRef = useRef<HTMLDivElement | null>(null);
  const [containerWidth, setContainerWidth] = useState(() =>
    typeof window !== 'undefined' ? window.innerWidth : 1280,
  );
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const el = gridRef.current;
    if (!el) {
      // 降级:grid 元素未挂载时用 window.innerWidth
      const onResize = () => setContainerWidth(window.innerWidth);
      window.addEventListener('resize', onResize);
      return () => window.removeEventListener('resize', onResize);
    }
    const ro = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const w = entry.contentBoxSize?.[0]?.inlineSize ?? entry.contentRect.width;
        if (w > 0) setContainerWidth(w);
      }
    });
    ro.observe(el);
    // 也监听 window resize(视口变化时 board-container 宽不一定立刻变,
    // 但 ResizeObserver 覆盖不到 grid 未挂载时的初始化)
    const onResize = () => {
      if (gridRef.current) setContainerWidth(gridRef.current.clientWidth);
    };
    window.addEventListener('resize', onResize);
    return () => {
      ro.disconnect();
      window.removeEventListener('resize', onResize);
    };
  }, []);
  const cols = useMemo(
    () => calcColsForViewport(containerWidth),
    [containerWidth],
  );
  const compact = useMemo(
    () => calcCompactForViewport(containerWidth),
    [containerWidth],
  );
  const rows = useMemo(
    () => buildGridOrder(total, mySeat, cols),
    [total, mySeat, cols],
  );

  // 把 bot_contexts 按 seat 索引化,便于座位卡直接查表。
  const botCtxMap = useMemo(() => {
    const map = new Map<number, BotContextJSON>();
    const list = gameState.bot_contexts ?? [];
    for (const b of list) map.set(b.seat, b);
    return map;
  }, [gameState.bot_contexts]);

  const idiotRevealed = useMemo(
    () => new Set<number>(gameState.idiot_revealed_seats ?? []),
    [gameState.idiot_revealed_seats],
  );

  // §20260811-02 U1 — 影响力分数按 seat 索引化(同 botCtxMap 模式)。
  const influenceMap = useMemo(() => {
    const map = new Map<number, InfluenceScoreJSON>();
    for (const s of gameState.influence_scores ?? []) map.set(s.seat, s);
    return map;
  }, [gameState.influence_scores]);

  // §重构 — 全局 Bot 思考指示器 1s tick。仅在任一 bot 处于活跃 phase
  // (calling / streaming / retrying / quarantined) 时启动 setInterval,
  // 全 idle 时立即停止以节省 CPU。仅一个 tick 实例,所有 SeatCell 共享
  // nowMs prop,避免每个座位卡各自开 setInterval 的 13x 性能开销。
  //
  // 2026-08-05 §Agent聊天显示优化 — tick 启动条件扩展:除「有活跃 LLM phase」外,
  // 「5 分钟内有任一 bot 发过言」也保持 tick 运行,让座位卡发言气泡的相对时间
  // (12s前 / 3分前)与 .is-fresh 3s 高亮能够自然推进。**不新增第二个定时器**,
  // 仍然只有这一个 1s interval,所有 SeatCell 共享同一份 nowMs。
  //
  // 2026-08-05 §02 — 气泡双源后,真人玩家的 players[].last_speech_at 同样纳入
  // 启动条件,否则纯真人发言时 tick 不启动、相对时间与 3s 高亮全部僵死。
  // 依旧只有这一个 interval。
  const hasActivePhase = useMemo(() => {
    const now = Date.now();
    for (const p of gameState.players ?? []) {
      if (p.last_speech_at && now - p.last_speech_at < SPEECH_TICK_WINDOW_MS) {
        return true;
      }
    }
    const list = gameState.bot_contexts ?? [];
    for (const b of list) {
      const phase = b.llm_call_phase ?? 'idle';
      if (
        b.llm_call_in_progress ||
        phase === 'calling' ||
        phase === 'streaming' ||
        phase === 'retrying' ||
        phase === 'quarantined' ||
        b.quarantined
      ) {
        return true;
      }
      // 近期发言窗口:相对时间需要持续刷新。
      if (b.last_speech_at && now - b.last_speech_at < SPEECH_TICK_WINDOW_MS) {
        return true;
      }
    }
    return false;
  }, [gameState.bot_contexts, gameState.players]);

  const [nowMs, setNowMs] = useState<number | undefined>(undefined);
  useEffect(() => {
    if (!hasActivePhase) {
      setNowMs(undefined);
      return;
    }
    setNowMs(Date.now());
    const id = setInterval(() => setNowMs(Date.now()), 1000);
    return () => clearInterval(id);
  }, [hasActivePhase]);

  // §128 对话即思考重构:allBots / WerewolfThinkingHeader 已删除,
// hasActivePhase 直接消费 gameState.bot_contexts。

  // §130 — 表格固定每行 4 个座位,无论 total 是 7/12/13。

  return (
    <div className={`werewolf-table ${isNight ? 'is-night' : 'is-day'}`}>
      <div className="werewolf-table__bg" />
      <DayNightOverlay phase={phase} />
      <SheriffElectedOverlay sheriffSeat={gameState.sheriff_seat} />

      {/* §127-bugfix: 棋牌区顶部标题已合并到页面主标题 werewolf-main-header,
          避免用户滚动时标题重复出现。此处仅保留阶段横幅。 */}

      {/* 顶部 phase 横幅 */}
      <div className="werewolf-table__phase">
        <span className="werewolf-table__phase-icon" aria-hidden="true">{phaseEmoji}</span>
        <span className="werewolf-table__phase-label">{phaseLabelStr}</span>
        {showDay && (
          <span className="werewolf-table__phase-day">
            {' · '}{t('werewolf.day' as any)} {gameState.day ?? 1}
          </span>
        )}
      </div>

      {/* 2026-07-22 §UX-法官布局: JudgePanel 已从桌面区移除 —— 其内容
          (最近宣告/历史/一举一动)由顶部 JudgeActionBar + HistoryDrawer
          「⚖️ 法官」tab 承载,不再挤压 13 人座位格,消除与游戏界面重合。 */}

      {/* §128 对话即思考重构:WerewolfThinkingHeader 已删除,信息由 WerewolfStatusBar 聚合显示。 */}

      {/* §130 主表格区 — CSS Grid 座位卡,Agent 实时状态内嵌到座位卡。
       *  §优化-20260802-01 — 严格 2 列/行:13 人 → 7 行 (2×6+1) / 12 人 → 6 行 / 7 人 → 4 行。
       * §优化-20260730-01 — JS 视口断点同时把 --ww-grid-cols / --ww-grid-gap
       *   写到 inline style,覆盖静态媒体查询,确保断点切换无延迟。 */}
      <div
        ref={gridRef}
        className="werewolf-table__grid"
        data-testid="werewolf-table-grid"
        style={{
          // CSS custom properties;CSS 变量作为 inline style 注入优先级高于媒体查询规则。
          ['--ww-grid-cols' as any]: String(cols),
          ['--ww-grid-gap' as any]: `${Math.max(6, 18 - (cols - 2) * 4)}px`,
        }}
      >
        {rows.map((row, rowIdx) =>
          row.map((seatIdx) => {
            const p = players[seatIdx];
            if (!p) return null;
            return (
              <SeatCell
                key={seatIdx}
                seatIdx={seatIdx}
                player={p}
                botCtx={botCtxMap.get(seatIdx)}
                isMySeat={seatIdx === mySeat}
                isDead={!p.alive}
                revealed={!!(p.role_revealed && p.role)}
                isIdiotRevealed={p.role === 'idiot' && idiotRevealed.has(seatIdx)}
                deadInfo={deadInfoMap.get(seatIdx)}
                isLastRow={rowIdx === rows.length - 1}
                nowMs={nowMs}
                identityGuess={identityGuess}
                propTargetSeat={propTargetSeat}
                spectator={spectator}
                compact={compact}
                influence={influenceMap.get(seatIdx)}
                gameStatus={gameState.status}
                isLastWordsSpeaking={seatIdx === lastWordsCurrentSeat}
              />
            );
          }),
        )}
      </div>

      {/* §123 死亡名牌面板
          BUG-R204-SEC-01: 观战者不显示死亡玩家角色名,仅显示座位号 + 死因标记。 */}
      {deadList.length > 0 && (
        <div className="werewolf-table__dead-list">
          {deadList.map((d) => (
            <span key={d.seat} className={`werewolf-table__dead-entry werewolf-table__dead-entry--${d.verdict || 'death'}`}>
              <span className="werewolf-table__dead-seat">#{d.seat + 1}</span>
              <span className="werewolf-table__dead-role">{spectator ? t('werewolf.role.unknown' as any) : (d.role ? t(`werewolf.role.${d.role}` as any) : '?')}</span>
              {d.verdict && (
                <span className="werewolf-table__dead-verdict">
                  {d.verdict === 'execution' ? '⚖️' : '💀'}
                </span>
              )}
            </span>
          ))}
        </div>
      )}

      {/* 中央事件日志 */}
      <div className="werewolf-table__center-info">
        {gameState.phase === 'night_wolves' && gameState.my_role === 'werewolf' && (() => {
          const wv = gameState.wolf_vote_view;
          // BUG-R195-P0 (2026-07-23): wv.tally.final 可能为 -1 或 undefined,?. 兜底(§44 教训)。
          if (wv && !wv.voting && wv.tally && typeof wv.tally.final === 'number' && wv.tally.final >= 0) {
            return (
              <div className="hint">
                🐺 投票结果: 击杀 <b>#{wv.tally.final + 1}</b>
                <span className="vote-reason">({tallyReasonLabel(wv.tally.reason)})</span>
              </div>
            );
          }
          return (
            <div className="hint">
              🐺 狼人投票中 {wv ? `${wv.votes_cast}/${wv.total_wolves}` : ''}
            </div>
          );
        })()}
        {gameState.phase === 'night_seer' && gameState.my_role === 'seer' && (
          <div className="hint">🔮 预言家请查验一名玩家</div>
        )}
        {gameState.phase === 'night_witch' && gameState.my_role === 'witch' && (
          <div className="hint">🧪 女巫请决定是否用药</div>
        )}
      </div>
    </div>
  );
}