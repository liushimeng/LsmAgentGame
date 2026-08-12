/**
 * 狼人杀 GameInfoPanel —— §129 全面适配 0.6fr 窄列的子控件重构。
 *
 * §129:§128 已建立 5 个 info-block 结构,本节把所有子控件(RulesButton /
 * 退出按钮 / 阵营计数格子 / 阶段名称 / 统计数字等)全部统一到窄列宽度
 * (约 240px) 的排版约束下,不留任何溢出 / 被截断 / 错位问题:
 *
 *   ┌────────────────────────────┐  ← 240px
 *   │ 🆔 身份                     │
 *   │  ┌─────┐  ┌─────────┐     │
 *   │  │ D1  │  │ 预言家   │     │  ← 左: Day+座位 / 右: 角色 pill
 *   │  │ #3  │  │         │     │
 *   │  └─────┘  └─────────┘     │
 *   ├────────────────────────────┤
 *   │ ⏱ 阶段                     │
 *   │    发言阶段                │  ← 大字居中,太长自动换行
 *   ├────────────────────────────┤
 *   │ ⚖ 阵营                     │
 *   │  ┌────┐ ┌────┐ ┌────┐    │
 *   │  │ ✨ │ │ 👥 │ │ 🐺 │    │  ← 3 列,emoji + 数字,无文字标签
 *   │  │  2 │ │  1 │ │  3 │    │
 *   │  └────┘ └────┘ └────┘    │
 *   ├────────────────────────────┤
 *   │ 📊 进度                     │
 *   │  9/13 存活    ⚔ 进行中    │
 *   ├────────────────────────────┤
 *   │ 📖 规则     🚪 退出房间   │  ← 两个并排小按钮,各占 50%
 *   └────────────────────────────┘
 *
 * 所有子控件(font-size / padding / min-width / overflow)均按 240px 设计,
 * 避免被截断、避免溢出、避免错位。
 *
 * 遵循 §27 ConfirmModal + §39 RulesViewer。
 */

import { useT } from '@/hooks/useT';
import { RulesButton } from '@/components/rules/RulesButton';
import { ConfirmModal } from '@/components/ui/ConfirmModal';
import type { WerewolfGameState } from '@/types/werewolf';
import { useState } from 'react';
import { phaseLabel } from '@/components/werewolf/phaseLabel';
import { FactionDrawer } from '@/components/werewolf/FactionDrawer';
import { wsClient } from '@/services/ws';

interface Props {
  gameState: WerewolfGameState | null;
  mySeat: number;
  spectator: boolean;
  onLeave: () => void;
}

export function GameInfoPanel({ gameState, mySeat, spectator, onLeave }: Props) {
  const t = useT();
  const [confirmLeave, setConfirmLeave] = useState(false);
  // §13 阵营侧滑抽屉 — 点阵营块弹出任务/Agent/玩家信息。
  const [drawerOpen, setDrawerOpen] = useState(false);

  const roleName = gameState?.my_role
    ? t(`werewolf.role.${gameState.my_role}` as any)
    : '—';

  const aliveCount = gameState?.players?.filter((p) => p?.alive).length ?? 0;
  const totalCount = gameState?.players?.length ?? 0;
  const isOver = gameState?.status === 'over';

  // 2026-07-24 优化:UI 暂停按钮。仅房主(mySeat===0 且非观战者)可见。
  // 房主不在 1 号位时此按钮不渲染,避免误操作(后端会再校验 IsOwner)。
  const isOwner = !spectator && mySeat === 0;
  const isPaused = !!gameState?.paused;
  const sendPause = (pause: boolean) => {
    if (!gameState?.room_id) return;
    wsClient.send('game.werewolf_pause', {
      room_id: gameState.room_id,
      pause,
      reason: pause ? 'owner_paused' : '',
    });
  };

  return (
    <div className="game-info-panel werewolf-info-panel werewolf-info-panel--v2">
      {/* §129 块 1 — 身份: Day+座位 / 角色 */}
      <div className="info-block info-block--identity">
        <div className="info-block__label">🆔 身份</div>
        <div className="info-block__row">
          <span className="info-stat info-stat--dual" data-testid="werewolf-info-day">
            <span className="info-stat__num">D{gameState?.day ?? 1}</span>
            <span className="info-stat__sub">天数</span>
            <span className="info-stat__divider" aria-hidden="true">|</span>
            <span className="info-stat__num">
              {spectator || mySeat < 0 ? '👁' : `#${mySeat + 1}`}
            </span>
            <span className="info-stat__sub">{spectator ? '观战' : '座位'}</span>
          </span>
          <span className="info-role-pill" data-testid="werewolf-info-role">
            {spectator ? '👁 观战者' : roleName}
          </span>
        </div>
        {/* 2026-08-11 BUG-ROLE-MISMATCH-P0 — 自选角色未生效就地提示。
            与全局 toast 互补:toast 弹一次即消失,这里常驻身份块下方,
            玩家任何时候看身份卡都能确认「这不是我选的角色」的原因。 */}
        {!spectator && gameState?.my_role_pref_unmet && (
          <div
            className="info-block__mini werewolf-role-pref-unmet"
            data-testid="werewolf-role-pref-unmet"
          >
            {t('werewolf.rolePick.unmetInline' as any, {
              role: gameState.my_preferred_role
                ? t(`werewolf.role.${gameState.my_preferred_role}` as any)
                : '?',
            })}
          </div>
        )}
      </div>

      {/* §129 块 2 — 阶段 */}
      <div className="info-block">
        <div className="info-block__label">⏱ 阶段</div>
        <div className="info-block__big" data-testid="werefolf-info-phase">
          {phaseLabel(t, gameState?.phase) ?? '—'}
        </div>
        {Array.isArray(gameState?.sheriff_streams) && (
          <div className="info-block__mini">
            🎖 警徽流 <strong>
              {gameState!.sheriff_streams.filter((s) => s >= 0).length}/2
            </strong>
          </div>
        )}
      </div>

      {/* §129 块 3 — 阵营计数(可点 → §13 阵营侧滑抽屉) */}
      <SideCounts gameState={gameState} onOpenCounts={() => setDrawerOpen(true)} />

      {/* §129 块 4 — 存活进度 */}
      <div className="info-block">
        <div className="info-block__label">📊 状态</div>
        <div className="info-block__row">
          <span className="info-stat">
            <span className="info-stat__num">{aliveCount}/{totalCount}</span>
            <span className="info-stat__sub">存活</span>
          </span>
          <span className="info-stat">
            <span className="info-stat__num">{isOver ? '🏁' : '⚔'}</span>
            <span className="info-stat__sub">{isOver ? '结束' : '进行中'}</span>
          </span>
        </div>
      </div>

      {/* §129 块 5 — 操作:规则 + 退出并排各占 50% */}
      <div className="info-block info-block--actions">
        <RulesButton kind="werewolf" />
        <button
          type="button"
          onClick={() => setConfirmLeave(true)}
          data-testid="werewolf-leave-button"
        >
          🚪 {t('werewolf.leaveRoom' as any)}
        </button>
      </div>

      {/* 2026-07-24 优化:UI 暂停/恢复按钮(房主)。仅真人玩家 1 号位可见。 */}
      {isOwner && gameState && !isOver && (
        <div className="info-block info-block--pause">
          <button
            type="button"
            className={`werewolf-pause-btn ${isPaused ? 'is-paused' : ''}`}
            data-testid="werewolf-pause-button"
            onClick={() => sendPause(!isPaused)}
            title={isPaused ? '点击恢复游戏推进' : '点击暂停 — bot 不再调 LLM,阶段时钟冻结'}
          >
            {isPaused ? '▶ 恢复游戏' : '⏸ 暂停游戏'}
          </button>
          {isPaused && (
            <div className="werewolf-pause-hint">
              已暂停 — 所有 bot 停止调 LLM,等待房主恢复。
              {gameState.paused_reason && <span>({gameState.paused_reason})</span>}
            </div>
          )}
        </div>
      )}

      {confirmLeave && (
        <ConfirmModal
          messageKey={'werewolf.confirmLeave' as any}
          danger
          onConfirm={() => { setConfirmLeave(false); onLeave(); }}
          onCancel={() => setConfirmLeave(false)}
        />
      )}

      {/* §13 阵营侧滑抽屉(gameState 就绪后才挂载,避免空指针) */}
      {gameState && (
        <FactionDrawer
          open={drawerOpen}
          onClose={() => setDrawerOpen(false)}
          gameState={gameState}
          mySeat={mySeat}
          spectator={spectator}
        />
      )}
    </div>
  );
}

/**
 * 屠边计数子组件(13 人局)。
 * §129:窄列布局下直接展示 emoji + 数字,无文字标签(由 info-block__label 提供)。
 */
function SideCounts({ gameState, onOpenCounts }: { gameState: WerewolfGameState | null; onOpenCounts: () => void }) {
  const t = useT();
  let counts: { divine: number; plain: number; wolf: number } | undefined = undefined;
  if (gameState && typeof gameState.wolf_alive === 'number'
      && typeof gameState.divine_alive === 'number'
      && typeof gameState.plain_alive === 'number') {
    counts = {
      divine: gameState.divine_alive,
      plain: gameState.plain_alive,
      wolf: gameState.wolf_alive,
    };
  } else if (gameState?.divine_plain_wolf_alive) {
    counts = gameState.divine_plain_wolf_alive;
  }
  const rows: { key: string; value: number; emoji: string }[] = counts
    ? [
        { key: 'divine', value: counts.divine, emoji: '✨' },
        { key: 'plain',  value: counts.plain,  emoji: '👥' },
        { key: 'wolf',   value: counts.wolf,   emoji: '🐺' },
      ]
    : EstimateFromPlayers(gameState);
  if (rows.length === 0) return null;
  return (
    <button
      type="button"
      className="info-block info-block--counts info-block--clickable"
      title={t('werewolf.drawer.title')}
      onClick={onOpenCounts}
    >
      <div className="info-block__label">⚖ 阵营</div>
      <div className="info-counts-grid">
        {rows.map((r) => (
          <div key={r.key} className={`info-count-cell info-count-cell--${r.key}`}>
            <span className="info-count-cell__emoji">{r.emoji}</span>
            <span className="info-count-cell__num">{r.value}</span>
          </div>
        ))}
      </div>
    </button>
  );
}

const DIVINE_ROLES = new Set([
  'seer', 'witch', 'hunter', 'idiot',
  'guard',
  // ⚠️ 2026-07-29 已退役:无引擎/工具/美术实现,前端隐藏
  // 'knight', 'magician', 'merchant', 'dreamer',
  // 'crow', 'scarecrow', 'prince', 'demon_hunter', 'pure_white',
]);

function EstimateFromPlayers(
  gameState: WerewolfGameState | null,
): { key: string; value: number; emoji: string }[] {
  let divine = 0;
  let plain = 0;
  let wolf = 0;
  if (gameState?.players) {
    for (const p of gameState.players) {
      if (!p.alive || !p.role) continue;
      if (p.role === 'werewolf') wolf += 1;
      else if (p.role === 'villager') plain += 1;
      else if (DIVINE_ROLES.has(p.role)) divine += 1;
    }
  }
  const total = divine + plain + wolf;
  if (total === 0) return [];
  return [
    { key: 'divine', value: divine, emoji: '✨' },
    { key: 'plain',  value: plain,  emoji: '👥' },
    { key: 'wolf',   value: wolf,   emoji: '🐺' },
  ];
}