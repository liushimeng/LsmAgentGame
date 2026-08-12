/**
 * NightActionPanel — 夜晚操作面板
 *
 * 五种形态(§猎魔人 新增第五形态 demon_hunter_hunt):
 *   - wolf_kill         狼人投票选择击杀目标(2026-07-17: 多狼投票)
 *   - guard_protect     守卫「盲守」选择守护目标(2026-07-29 §134 新增)
 *   - seer_check        预言家查验某玩家的阵营
 *   - witch_act         女巫决定用药 / 不操作
 *   - demon_hunter_hunt 猎魔人夜间狩猎(§猎魔人 新增)
 *
 * 守卫形态约束(§2):
 *   - 隐藏自己
 *   - 上晚守护目标(guard_last_protect)渲染为 disabled + 「昨晚已守」
 *   - 「空守」按钮 → target = -1
 *   - 守卫看不到狼刀目标(盲守)
 *
 * 猎魔人形态约束(§猎魔人 §2.2):
 *   - DH1 首夜不可用:DayNumber<2 时面板显示"首夜尚未解锁"
 *   - DH2 只能狩猎存活玩家
 *   - DH3 不能狩猎自己
 *   - DH4 空过合法:提供「放弃狩猎」按钮 → target = -1
 *   - DH7 发动即公开身份(文案提示)
 */

import { useEffect, useState } from 'react';
import { werewolfAssets } from '@/assets/images/werewolf';
import { useT } from '@/hooks/useT';
import { phaseLabel as phaseLabelKey } from '@/components/werewolf/phaseLabel';
import type { WerewolfGameState } from '@/types/werewolf';

interface Props {
  gameState: WerewolfGameState;
  onAction: (action: 'wolf_kill' | 'guard_protect' | 'seer_check' | 'witch_act' | 'demon_hunter_hunt',
             opts: { target?: number; witchAction?: string; witchTarget?: number }) => void;
  busy: boolean;
}

// 投票原因中文标签
function tallyReasonLabel(reason: string): string {
  switch (reason) {
    case 'majority': return '多数决';
    case 'random_tie_break': return '平票随机';
    case 'random_all_abstain': return '全弃权随机';
    default: return reason;
  }
}

export function NightActionPanel({ gameState, onAction, busy }: Props) {
  const t = useT();
  const phase = gameState.phase;
  const [target, setTarget] = useState<number | null>(null);
  const [poisonTarget, setPoisonTarget] = useState<number | null>(null);

  // 跨阶段/跨玩家死亡重置本地选中态,避免上轮已死座位的"投票给 #N"按钮残留(自动化测试报告 T2)。
  // 例: D1 夜狼人选中 #8 → 进入 seer/witch 阶段(组件仍挂载,仅渲染"闭眼") → D2 夜 phase 回到 night_wolves,
  // 此时 #8 已死,座位按钮被隐藏(p.alive=false),但 action-row 仍显示"投票给 #8"。
  const aliveSnap = gameState.players.map(p => p.alive).join(',');
  useEffect(() => {
    setTarget(null);
    setPoisonTarget(null);
  }, [phase, aliveSnap]);

  // 若当前选中目标已死亡(由其它路径导致),同步清除,避免"投票给 #N"指向不可选座位。
  if (target !== null && !gameState.players[target]?.alive) {
    setTarget(null);
  }
  if (poisonTarget !== null && !gameState.players[poisonTarget]?.alive) {
    setPoisonTarget(null);
  }

  const mySeat = gameState.my_seat;
  const myRole = gameState.my_role;
  const voteView = gameState.wolf_vote_view;

  // 2026-08-09 §20260808-03 — 人类玩家死亡守卫(缺陷 E 修复)。
  // 死态时所有 5 种夜间形态均不渲染动作面板,仅显示观察者提示。
  // 例外:death_lyric 阶段由 LastWordsPanel 接管,不在此守卫范围内。
  // 自检而非外部 prop 传入 — 更解耦,任何路径调到此组件都生效。
  const mySeatSafe = mySeat >= 0 ? mySeat : -1;
  const iAmDeadAtNight = phase !== 'death_lyric' &&
    mySeatSafe >= 0 &&
    gameState.players[mySeatSafe]?.alive === false;
  if (iAmDeadAtNight) {
    const phaseText = phaseLabelKey(t, phase) ?? phase;
    return (
      <div className="werewolf-action-panel werewolf-action-panel--dead">
        <h4>☠ {t('werewolf.dead.title', { phase: phaseText })}</h4>
        <p>{t('werewolf.dead.nightHint')}</p>
        <p className="dead-observer-hint">{t('werewolf.dead.actions.title', { phase: phaseText })}</p>
        <ul className="dead-observer-actions">
          <li>{t('werewolf.dead.actions.watch')}</li>
          <li>{t('werewolf.dead.actions.lastwords')}</li>
        </ul>
      </div>
    );
  }

  if (phase === 'night_guard' && myRole === 'guard') {
    // 2026-07-29 §134 守卫形态:
    //   - G1 不可连守:上晚已守的人今晚不能再守(渲染为 disabled)
    //   - G2 不可守自己:座位网格隐藏自己
    //   - G3 只能守存活玩家
    //   - G4 空守合法:提供「空守」按钮 → target = -1
    //   - 盲守:守卫看不到狼刀目标(服务端不下发,前端不展示)
    const lastProtect = gameState.guard_last_protect ?? -1;
    return (
      <div className="werewolf-action-panel">
        <h4>🛡️ {t('werewolf.guard.title')}</h4>

        <div className="guard-hints">
          <p className="sub guard-blind-hint">
            ℹ️ {t('werewolf.guard.blindHint')}
          </p>
          <p className="sub guard-consecutive-hint">
            ⚠️ {t('werewolf.guard.noConsecutiveHint')}
          </p>
        </div>

        <div className="seat-grid">
          {Array.from({ length: gameState.max_seat }).map((_, seat) => {
            const p = gameState.players[seat];
            if (!p || !p.alive) return null;
            // G2: 不可守自己 → 直接隐藏(不渲染 disabled 按钮,避免视觉污染)
            if (seat === mySeat) return null;
            const isLast = seat === lastProtect;
            const selected = target === seat;
            return (
              <button
                key={seat}
                type="button"
                className={`seat-chip ${selected ? 'is-selected' : ''} ${isLast ? 'is-last-guarded' : ''}`}
                onClick={() => setTarget(seat)}
                // G1 连守 + G3 死亡:两种 disabled 状态合并
                disabled={busy || isLast}
                title={isLast ? t('werewolf.guard.lastNight') : undefined}
                data-testid={`werewolf-guard-target-${seat}`}
              >
                #{seat + 1}
                {isLast && <span className="last-guard-mark" aria-hidden="true">⏳</span>}
              </button>
            );
          })}
        </div>
        <div className="action-row">
          <button
            className="btn btn-primary"
            onClick={() => target !== null && onAction('guard_protect', { target })}
            disabled={busy || target === null}
          >
            {t('werewolf.guard.protect')}
            {target !== null && ` #${target + 1}`}
          </button>
          <button
            className="btn btn-secondary"
            onClick={() => onAction('guard_protect', { target: -1 })}
            disabled={busy}
          >
            {t('werewolf.guard.skip')}
          </button>
        </div>
      </div>
    );
  }

  if (phase === 'night_wolves' && myRole === 'werewolf') {
    // 2026-07-17: 投票已结算 → 显示结果
    if (voteView && !voteView.voting && voteView.tally) {
      return (
        <div className="werewolf-action-panel">
          <h4>🐺 狼人投票结果</h4>
          <p className="sub">
            最终击杀: <b>#{voteView.tally.final + 1}</b>
            <span className="vote-reason">({tallyReasonLabel(voteView.tally.reason)})</span>
          </p>
          {voteView.tally.counts && Object.keys(voteView.tally.counts).length > 0 && (
            <div className="vote-result">
              {Object.entries(voteView.tally.counts).map(([seat, cnt]) => (
                <span key={seat} className="vote-count">
                  #{Number(seat) + 1}: {cnt}票
                </span>
              ))}
            </div>
          )}
        </div>
      );
    }

    // 投票中: 判断本狼是否已投票
    // 2026-07-18 R148 修复: 服务端 nil slice 序列化可能为 null;前端必须双保险(§44 教训)。
    const myVoted = voteView ? voteView.votes[mySeat] !== undefined || (voteView.abstain ?? []).includes(mySeat) : false;
    const myVoteTarget = voteView ? voteView.votes[mySeat] : undefined;
    // R168 P3-4: 全部狼人已投票(含弃权)但计票结果未下发 → 显示"计票中"占位,避免 UI 空白期。
    const allVoted = voteView
      ? (voteView.votes_cast + (voteView.abstain ?? []).length) >= voteView.total_wolves && voteView.total_wolves > 0
      : false;

    return (
      <div className="werewolf-action-panel">
        <h4>🐺 狼人夜间投票</h4>
        <p className="sub">
          已投票 {voteView ? voteView.votes_cast : 0}/{voteView ? voteView.total_wolves : '?'}
          {/* BUG-R195-P0 (2026-07-23): voteView.abstain 服务端可能为 null/未下发,
              必须用 ?? [] 兜底,否则读取 .length 抛 TypeError 触发 ErrorBoundary(§44 教训)。 */}
          {voteView && (voteView.abstain?.length ?? 0) > 0 && (
            <span className="abstain-hint"> · {(voteView.abstain ?? []).length}人弃权</span>
          )}
        </p>

        {/* R168 P3-4: 计票中 loading 占位(全员已投、结果未出) */}
        {allVoted && (
          <p className="sub tally-pending is-pulsing" role="status">
            ⏳ 全员已投票,计票中…
          </p>
        )}

        {/* 队友投票状态 */}
        {voteView && voteView.wolf_seats.length > 0 && (
          <div className="wolf-vote-status">
            {voteView.wolf_seats.map((ws) => {
              const isSelf = ws === mySeat;
              const votedTarget = voteView.votes[ws];
              const isAbstain = (voteView.abstain ?? []).includes(ws);
              let status: string;
              if (votedTarget !== undefined) status = `→ #${votedTarget + 1}`;
              else if (isAbstain) status = '弃权';
              else status = '等待';
              return (
                <span key={ws} className={`wolf-vote-chip${isSelf ? ' is-self' : ''}`}>
                  {isSelf ? '我' : `#${ws + 1}`} {status}
                </span>
              );
            })}
          </div>
        )}

        {/* 平票提示 — tally.tied 同样可能为 null/未下发(§44 教训)。
            ?? [] 兜底 + .length 守卫,避免 undefined.length 崩溃触发 ErrorBoundary。 */}
        {voteView && voteView.tally && (voteView.tally.tied?.length ?? 0) > 1 && (
          <p className="sub tie-hint">
            ⚠ 平票: {(voteView.tally.tied ?? []).map(s => `#${s + 1}`).join(' / ')} — 将随机选择
          </p>
        )}

        <div className="seat-grid">
          {Array.from({ length: gameState.max_seat }).map((_, seat) => {
            const p = gameState.players[seat];
            if (!p || !p.alive) return null;
            const isSelf = seat === mySeat;
            const selected = target === seat;
            return (
              <button
                key={seat}
                type="button"
                className={`seat-chip ${selected ? 'is-selected' : ''}`}
                onClick={() => setTarget(seat)}
                disabled={isSelf || busy || myVoted}
                data-testid={`werewolf-target-${seat}`}
              >
                <img src={werewolfAssets.knife} alt="knife" className="knife-icon" />
                #{seat + 1}
              </button>
            );
          })}
        </div>
        <div className="action-row">
          {myVoted ? (
            <span className="vote-confirmed">
              ✓ 已投票{myVoteTarget !== undefined ? ` → #${myVoteTarget + 1}` : ' (弃权)'},等待其他狼人...
            </span>
          ) : (
            <>
              <button
                className="btn btn-primary"
                onClick={() => onAction('wolf_kill', { target: target ?? -1 })}
                disabled={busy || target === null || !gameState.players[target]?.alive}
              >
                投票给 #{target !== null ? target + 1 : '?'}
              </button>
              <button
                className="btn btn-secondary"
                onClick={() => onAction('wolf_kill', { target: -1 })}
                disabled={busy}
              >
                弃权
              </button>
            </>
          )}
        </div>
      </div>
    );
  }

  if (phase === 'night_seer' && myRole === 'seer') {
    return (
      <div className="werewolf-action-panel">
        <h4>🔮 预言家请查验一名玩家</h4>
        <div className="seat-grid">
          {Array.from({ length: gameState.max_seat }).map((_, seat) => {
            const p = gameState.players[seat];
            if (!p || !p.alive || seat === mySeat) return null;
            return (
              <button
                key={seat}
                type="button"
                className={`seat-chip ${target === seat ? 'is-selected' : ''}`}
                onClick={() => setTarget(seat)}
                disabled={busy}
              >
                #{seat + 1}
              </button>
            );
          })}
        </div>
        <div className="action-row">
          <button
            className="btn btn-primary"
            onClick={() => target !== null && onAction('seer_check', { target })}
            disabled={busy || target === null}
          >
            查验
          </button>
        </div>
      </div>
    );
  }

  if (phase === 'night_witch' && myRole === 'witch') {
    const wolfTarget = gameState.witch_wolf_target ?? -1;
    const antidoteAvail = !gameState.witch_antidote_used;
    const poisonAvail = !gameState.witch_poison_used;
    return (
      <div className="werewolf-action-panel">
        <h4>🧪 女巫的决定</h4>
        <div className="witch-info">
          {wolfTarget >= 0 && antidoteAvail && (
            <p>今晚被狼杀的人: <b>#{wolfTarget + 1}</b> — 可用解药救人</p>
          )}
          {wolfTarget < 0 && <p>今晚平安夜,狼人空刀。</p>}
        </div>
        <div className="seat-grid">
          <p className="sub">如使用毒药,请选择目标:</p>
          {Array.from({ length: gameState.max_seat }).map((_, seat) => {
            const p = gameState.players[seat];
            if (!p || !p.alive || seat === mySeat) return null;
            return (
              <button
                key={seat}
                type="button"
                className={`seat-chip ${poisonTarget === seat ? 'is-selected' : ''}`}
                onClick={() => setPoisonTarget(seat)}
                disabled={busy || !poisonAvail}
              >
                #{seat + 1}
              </button>
            );
          })}
        </div>
        <div className="action-row">
          <button
            className="btn btn-secondary"
            onClick={() => onAction('witch_act', { witchAction: 'none' })}
            disabled={busy}
          >
            不用药
          </button>
          {wolfTarget >= 0 && antidoteAvail && (
            <button
              className="btn btn-primary"
              onClick={() => onAction('witch_act', { witchAction: 'antidote' })}
              disabled={busy}
            >
              💊 解药救人
            </button>
          )}
          {poisonAvail && poisonTarget !== null && (
            <button
              className="btn btn-danger"
              onClick={() => onAction('witch_act', { witchAction: 'poison', witchTarget: poisonTarget })}
              disabled={busy}
            >
              ☠ 毒 #{poisonTarget + 1}
            </button>
          )}
        </div>
      </div>
    );
  }

  if (phase === 'night_demon_hunter' && myRole === 'demon_hunter') {
    // §猎魔人 猎魔人夜间狩猎形态:
    //   - DH1 首夜不可用:DayNumber<2 时面板显示"首夜尚未解锁"
    //   - DH2 只能狩猎存活玩家
    //   - DH3 不能狩猎自己
    //   - DH4 空过合法:提供「放弃狩猎」按钮 → target = -1
    //   - DH7 发动即公开身份(文案提示)
    const dayNumber = gameState.day ?? 1;
    const firstNightDisabled = dayNumber < 2;
    return (
      <div className="werewolf-action-panel">
        <h4>🎯 {t('werewolf.demon_hunter.title')}</h4>

        <div className="demon-hunter-hints">
          <p className="sub demon-hunter-risk-hint">
            ⚠️ {t('werewolf.demon_hunter.risk')}
          </p>
          {firstNightDisabled && (
            <p className="sub demon-hunter-first-night-hint">
              🌙 {t('werewolf.demon_hunter.firstNightDisabled')}
            </p>
          )}
        </div>

        {!firstNightDisabled && (
          <>
            <div className="seat-grid">
              {Array.from({ length: gameState.max_seat }).map((_, seat) => {
                const p = gameState.players[seat];
                if (!p || !p.alive) return null;
                // DH3: 不能狩猎自己 → 直接隐藏
                if (seat === mySeat) return null;
                const selected = target === seat;
                return (
                  <button
                    key={seat}
                    type="button"
                    className={`seat-chip ${selected ? 'is-selected' : ''}`}
                    onClick={() => setTarget(seat)}
                    disabled={busy}
                    data-testid={`werewolf-demon-hunter-target-${seat}`}
                  >
                    #{seat + 1}
                  </button>
                );
              })}
            </div>
            <div className="action-row">
              <button
                className="btn btn-primary"
                onClick={() => target !== null && onAction('demon_hunter_hunt', { target })}
                disabled={busy || target === null}
              >
                {t('werewolf.demon_hunter.confirm')}
                {target !== null && ` #${target + 1}`}
              </button>
              <button
                className="btn btn-secondary"
                onClick={() => onAction('demon_hunter_hunt', { target: -1 })}
                disabled={busy}
              >
                {t('werewolf.demon_hunter.skip')}
              </button>
            </div>
          </>
        )}
      </div>
    );
  }

  // 夜晚其它阶段(非自己回合)
  if (phase === 'night_guard' || phase === 'night_wolves' || phase === 'night_seer' || phase === 'night_witch' || phase === 'night_demon_hunter') {
    return (
      <div className="werewolf-action-panel muted">
        <h4>🌙 天黑了</h4>
        <p>{(t('werewolf.eyesClosed' as any)) ?? '请闭眼,等待白天来临'}</p>
      </div>
    );
  }

  return null;
}
