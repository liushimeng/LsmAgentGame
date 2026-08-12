// 2026-07-12 §13 增强 — 阵营侧滑抽屉(FactionDrawer)。
// 从右侧滑出(380px 与 30vw 取小),3 个 Tab:
//   1. 任务列表  — 根据 phase + my_role + my_seat 推导"当前待办"
//   2. Agent 信息 — 遍历 bot_contexts,复用 AgentCallTimeBadge
//   3. 玩家信息  — 遍历 players,展示座位 / 角色 / 存活 / 是否 bot
//
// 触发:GameInfoPanel 的 SideCounts 阵营块(点开)。
// 关闭:右上角 × / 点 overlay / ESC(参考 SheriffStreamPanel / ConfirmModal 浮层规约)。
// i18n 三语同步(zh-CN / en / ja)。

import React, { useEffect, useMemo, useState } from 'react';
import type {
  WerewolfGameState,
  BotContextJSON,
} from '@/types/werewolf';
import { useT } from '@/hooks/useT';
import { AgentCallTimeBadge } from '@/components/werewolf/AgentCallTimeBadge';
import { getEmotionDisplay } from './emotion';

interface FactionDrawerProps {
  open: boolean;
  onClose: () => void;
  gameState: WerewolfGameState;
  mySeat: number;
  spectator: boolean;
}

type TabKey = 'task' | 'agent' | 'player';

// ── Tab 1:任务列表 ──────────────────────────────────────────────
// 静态 map:每个 phase 给出当前主打待办(依据 round_number / phase_extra 推演),
// 主要目的是可视化"当前阶段该做什么",不做精确状态机。

interface TaskItem {
  id: string;
  label: string;
  done: boolean;
}

function buildTasks(gs: WerewolfGameState, mySeat: number, spectator: boolean, t: ReturnType<typeof useT>): TaskItem[] {
  const phase = String(gs.phase ?? 'filling');
  const role = gs.my_role;
  const isMe = (seat: number) => seat === mySeat;
  const myAlive =(mySeat >= 0 ? gs.players?.[mySeat]?.alive : undefined) ?? false;
  const tasks: TaskItem[] = [];

  const push = (id: string, label: string, done = false) => tasks.push({ id, label, done });

  switch (phase) {
    case 'filling':
      push('fill', spectator ? t('werewolf.drawer.waitingForPlayers') : '等待全部玩家入座后开始对局…');
      break;
    case 'pre_wolves':
      push('pre_speak', '首夜强制发言:每名玩家至少发言 1 轮');
      push('pre_intro', '利用发言时间初步建立信息格局');
      break;
    case 'night_wolves':
      if (role === 'werewolf' && myAlive) push('wolf_kill', '🐺 狼人阵营协商,选择今晚击杀目标');
      else push('wolf_wait', '🐺 夜晚 — 请闭眼等待');
      break;
    case 'night_seer':
      if (role === 'seer' && myAlive) push('seer_check', '🔮 预言家请查验一名玩家身份');
      else push('seer_wait', '🔮 预言家行动中 — 请闭眼等待');
      break;
    case 'night_witch':
      if (role === 'witch' && myAlive) {
        if (!gs.witch_antidote_used) push('witch_save', '🧪 是否使用解药救活今晚死者');
        if (!gs.witch_poison_used) push('witch_poison', '🧪 是否使用毒药毒杀一名玩家');
        if (gs.witch_antidote_used && gs.witch_poison_used) push('witch_done', '🧪 双药已用,跳过');
      } else {
        push('witch_wait', '🧪 女巫行动中 — 请闭眼等待');
      }
      break;
    case 'dawn':
      push('dawn', '🌅 听取天亮公告,了解昨夜死亡情况');
      break;
    case 'sheriff':
      push('sheriff', '🎖 竞选警长阶段 — 决定是否参选并投票');
      break;
    case 'speak': {
      const isMyTurn = gs.speak_turn_seat === mySeat;
      push('speak', isMyTurn ? '💬 轮到你发言,请表达观点并分析信息' : '💬 白天轮流发言 — 倾听他人观点,准备质询');
      break;
    }
    case 'vote':
      push('vote', '️🗳 投票放逐阶段 — 选出最可疑的玩家投出放逐票');
      break;
    case 'idiot_reveal':
      push('idiot', '🃏 白痴翻牌阶段 — 决定是否翻开身份免予出局');
      break;
    case 'death_lyric':
      push('lyric', '🪦 遗言阶段 —  newly 死亡玩家发表遗言');
      break;
    case 'hunter_shoot':
      if (role === 'hunter') push('hunter', '🔫 猎人开枪 — 决定是否带走一名玩家');
      else push('hunter_wait', '🔫 猎人行动中');
      break;
    case 'restart_vote':
      push('restart', '🔄 重开局投票 — 决定是否原地重开一局');
      break;
    case 'over':
      push('over', '🏁 对局已结束');
      break;
    default:
      push('generic', '按阶段提示行动');
  }

  // 通用存活状态条目(玩家视角)。
  if (!spectator && isMe(mySeat)) {
    push('alive', myAlive ? '你存活在场,继续寻找胜利之路' : '你已离场 — 以旁观者身份观战', !myAlive);
  }
  return tasks;
}

function TaskTab({ tasks }: { tasks: TaskItem[] }) {
  const t = useT();
  return (
    <ul className="faction-drawer__task-list">
      {tasks.map((task) => (
        <li
          key={task.id}
          className={`faction-drawer__task${task.done ? ' is-done' : ' is-pending'}`}
        >
          <span className="faction-drawer__task-icon" aria-hidden>
            {task.done ? '✅' : '⏳'}
          </span>
          <span className="faction-drawer__task-label">{task.label}</span>
        </li>
      ))}
      {tasks.length === 0 && (
        <li className="faction-drawer__task faction-drawer__task--empty">
          {t('werewolf.drawer.task.empty')}
        </li>
      )}
    </ul>
  );
}

// ── Tab 2:Agent 信息 ────────────────────────────────────────────
// 遍历 bot_contexts,每 agent 一行:座位 / 模型 / 存活 / 情绪 / LLM 相位 / 性能 / 最后调用时间。
// heart_thought 仅观战者可见(BotTranscript 层协议隔离)。

function phaseBadgeLabel(phase: BotContextJSON['llm_call_phase']): string {
  switch (phase) {
    case 'calling': return '调用中';
    case 'streaming': return '生成中';
    case 'retrying': return '重试中';
    case 'quarantined': return '已禁用';
    case 'idle':
    default: return '空闲';
  }
}

function AgentTab({ bots, nowMs, spectator }: { bots: BotContextJSON[]; nowMs: number; spectator: boolean }) {
  const t = useT();
  if (bots.length === 0) {
    return <div className="faction-drawer__empty">{t('werewolf.drawer.empty')}</div>;
  }
  return (
    <div className="faction-drawer__agent-list">
      {bots.map((b) => (
        <div key={b.seat} className="faction-drawer__agent" data-testid={`faction-agent-${b.seat}`}>
          <div className="faction-drawer__agent-head">
            <span className="faction-drawer__agent-seat">#{b.seat + 1}</span>
            <span className="faction-drawer__agent-model" title={b.model}>{b.model}</span>
            <span className={`faction-drawer__agent-phase faction-drawer__agent-phase--${b.llm_call_phase ?? 'idle'}`}>
              {phaseBadgeLabel(b.llm_call_phase)}
            </span>
          </div>
          <div className="faction-drawer__agent-meta">
            <span title={t('werewolf.drawer.agent.latency')}>
              🐇 {(b.last_llm_latency_ms ?? 0) > 0 ? `${(b.last_llm_latency_ms! / 1000).toFixed(1)}s` : '—'}
            </span>
            <span title={t('werewolf.drawer.agent.avg')}>
              ∑ {((b.avg_llm_latency_ms ?? 0) / 1000).toFixed(1)}s
            </span>
            <span title={t('werewolf.drawer.agent.calls')}>↻ {b.total_llm_calls ?? 0}</span>
          </div>
          <div className="faction-drawer__agent-calltime">
            <AgentCallTimeBadge ctx={b} nowMs={nowMs} />
          </div>
          {b.emotion && (() => {
            // 2026-08-04 §重构 — 改用 emoji + label(共享模块);reason 作 hint。
            const emo = getEmotionDisplay(b.emotion);
            if (!emo) return null;
            return (
              <div
                className="faction-drawer__agent-emotion"
                title={b.emotion_reason ? `${emo.label} — ${b.emotion_reason}` : emo.label}
              >
                <span className="faction-drawer__agent-emotion-emoji">{emo.emoji}</span>
                <span className="faction-drawer__agent-emotion-label">{emo.label}</span>
                {b.emotion_reason && (
                  <span className="faction-drawer__agent-emotion-hint">— {b.emotion_reason}</span>
                )}
              </div>
            );
          })()}
          {spectator && b.heart_thought && (
            <blockquote className="faction-drawer__agent-heart">
              💭 {b.heart_thought}
            </blockquote>
          )}
        </div>
      ))}
    </div>
  );
}

// ── Tab 3:玩家信息 ──────────────────────────────────────────────
// 遍历 players:座位 / 角色(死亡或本人显示,否则 ?) / 存活 / 是否 bot。

function PlayerTab({ gs, mySeat, spectator }: { gs: WerewolfGameState; mySeat: number; spectator: boolean }) {
  const t = useT();
  const seatIsBot = useMemo(() => {
    const s = new Set<number>();
    for (const b of gs.bot_contexts ?? []) s.add(b.seat);
    return s;
  }, [gs.bot_contexts]);

  const players = gs.players ?? [];
  if (players.length === 0) {
    return <div className="faction-drawer__empty">{t('werewolf.drawer.empty')}</div>;
  }
  return (
    <div className="faction-drawer__player-list">
      {players.map((p) => {
        // §135: 移除 `|| !p.alive` —— 普通死亡出局身份牌不翻开,一律以服务端
        // role_revealed 为准(终局/白痴翻牌/狼自爆/猎人开枪 4 类才为 true)。
        // spectator 纵深防御:观战者永不看到身份,即便后端误发。
        const reveal = !spectator && (p.role_revealed || p.seat === mySeat);
        const roleLabel = reveal && p.role ? t(`werewolf.role.${p.role}` as any) : '???';
        const isBot = seatIsBot.has(p.seat);
        return (
          <div key={p.seat} className="faction-drawer__player" data-testid={`faction-player-${p.seat}`}>
            <span className="faction-drawer__player-seat">#{p.seat + 1}</span>
            <span className={`faction-drawer__player-role${reveal ? '' : ' is-hidden'}`}>{roleLabel}</span>
            <span className={`faction-drawer__player-alive ${p.alive ? 'is-alive' : 'is-dead'}`}>
              {p.alive ? '🟢' : '🔴'}
            </span>
            <span className="faction-drawer__player-type">
              {isBot ? t('werewolf.drawer.player.bot') : t('werewolf.drawer.player.human')}
            </span>
            {p.agent_name && (
              <span className="faction-drawer__player-agent" title={p.agent_name}>{p.agent_name}</span>
            )}
          </div>
        );
      })}
    </div>
  );
}

// ── 抽屉主组件 ──────────────────────────────────────────────────

export const FactionDrawer: React.FC<FactionDrawerProps> = ({
  open,
  onClose,
  gameState,
  mySeat,
  spectator,
}) => {
  const t = useT();
  const [tab, setTab] = useState<TabKey>('task');
  // 父级 1s tick 由 WerewolfTable 提供,但抽屉是独立挂载(在 GameInfoPanel),
  // 故抽屉自己维护一个轻量的 1s setInterval 渲染 Agent 调用时间倒计时。
  const [nowMs, setNowMs] = useState<number>(Date.now());

  useEffect(() => {
    if (!open) return;
    setNowMs(Date.now());
    const id = setInterval(() => setNowMs(Date.now()), 1000);
    return () => clearInterval(id);
  }, [open]);

  // ESC 关闭 + 入口 nowMs 同步。
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  const tasks = useMemo(
    () => buildTasks(gameState, mySeat, spectator, t),
    [gameState, mySeat, spectator, t],
  );

  if (!open) return null;

  const tabs: { key: TabKey; label: string; emoji: string }[] = [
    { key: 'task', label: t('werewolf.drawer.tab.task'), emoji: '📋' },
    { key: 'agent', label: t('werewolf.drawer.tab.agent'), emoji: '🤖' },
    { key: 'player', label: t('werewolf.drawer.tab.player'), emoji: '👥' },
  ];

  return (
    <div className="faction-drawer" data-testid="faction-drawer" role="dialog" aria-modal="true" aria-label={t('werewolf.drawer.title')}>
      <div className="faction-drawer__overlay" onClick={onClose} />
      <aside className="faction-drawer__panel">
        <header className="faction-drawer__header">
          <h3 className="faction-drawer__title">{t('werewolf.drawer.title')}</h3>
          <button
            type="button"
            className="faction-drawer__close"
            onClick={onClose}
            aria-label={t('werewolf.drawer.close')}
          >
            ×
          </button>
        </header>
        <nav className="faction-drawer__tabs">
          {tabs.map((tb) => (
            <button
              key={tb.key}
              type="button"
              className={`faction-drawer__tab${tab === tb.key ? ' is-active' : ''}`}
              onClick={() => setTab(tb.key)}
              data-testid={`faction-drawer-tab-${tb.key}`}
            >
              <span aria-hidden>{tb.emoji}</span> {tb.label}
            </button>
          ))}
        </nav>
        <div className="faction-drawer__body">
          {tab === 'task' && <TaskTab tasks={tasks} />}
          {tab === 'agent' && <AgentTab bots={gameState.bot_contexts ?? []} nowMs={nowMs} spectator={spectator} />}
          {tab === 'player' && <PlayerTab gs={gameState} mySeat={mySeat} spectator={spectator} />}
        </div>
      </aside>
    </div>
  );
};

export default FactionDrawer;
