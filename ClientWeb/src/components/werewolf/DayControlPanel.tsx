/**
 * DayControlPanel — 白天发言/投票/警长相关 UI。
 *
 * §51 BUG-WEREWOLF-DAWN-NO-UI:
 *   dawn 阶段需要展示「昨夜死亡公告」+ 「进入白天」按钮,否则玩家永远卡死。
 *   兜底:8s 自动 setTimeout 触发 start_day,避免玩家挂机阻塞全场。
 */

import { useEffect, useState } from 'react';
import { useT } from '@/hooks/useT';
import { phaseLabel as phaseLabelKey } from '@/components/werewolf/phaseLabel';
import { ConfirmModal } from '@/components/ui/ConfirmModal';
import type { WerewolfGameState } from '@/types/werewolf';

interface Props {
  gameState: WerewolfGameState;
  mySeat: number;
  onVote: (target: number) => void;
  onSheriff: (action: 'candidate' | 'vote' | 'elect', target?: number) => void;
  onFinish: (action: 'speak' | 'vote' | 'start_day' | 'idiot_reveal', tiedRound?: number) => void;
  // 2026-07-10 12 人局: 打开警徽流声明面板(SheriffStreamPanel)。
  onOpenSheriffStream?: () => void;
  // 2026-07-11: 预言家发起投票
  onProposeVote?: () => void;
  // §198 骑士决斗:白天发言阶段由 onDuel(target) 触发, target=-1 = 放弃本轮。
  // 服务端 action='knight_duel',target 字段与 guard_protect/witch_act 一致。
  onDuel?: (target: number) => void;
  busy: boolean;
}

const DAWN_AUTO_START_MS = 8000;

export function DayControlPanel({ gameState, mySeat, onVote, onSheriff, onFinish, onOpenSheriffStream, onProposeVote, onDuel, busy }: Props) {
  const t = useT();
  const phase = gameState.phase;
  const [target, setTarget] = useState<number | null>(null);
  // §20260811-06 U1 — Knight 决斗二次确认 state。null = modal 关闭;非 null = 待确认目标座。
  const [confirmDuelTarget, setConfirmDuelTarget] = useState<number | null>(null);
  // 2026-08-09 §20260808-03 — 死亡守卫统一标志。sheriff 分支已有死态分支处理,
  // 这里只覆盖 vote / speak / idiot_reveal(dawn / hunter_shoot 不需,见下)。
  const mySeatSafe = mySeat >= 0 ? mySeat : -1;
  const iAmDead = mySeatSafe >= 0 && gameState.players?.[mySeatSafe]?.alive === false;

  // dawn 阶段:8s 兜底自动进入白天,避免玩家离线导致全场卡死
  const [autoCountdown, setAutoCountdown] = useState<number>(DAWN_AUTO_START_MS / 1000);

  // §20260811-01 U3 — 投票半公开计票悬念状态。
  // 当 vote_suspense=true 且投票阶段结束时，延迟显示完整票型。
  const [voteSuspenseActive, setVoteSuspenseActive] = useState(false);
  const [voteSuspenseTimer, setVoteSuspenseTimer] = useState<ReturnType<typeof setTimeout> | null>(null);

  // 检测投票结束 → 触发悬念
  useEffect(() => {
    if (!gameState.vote_suspense || phase !== 'vote' || !gameState.votes || Object.keys(gameState.votes).length === 0) {
      return;
    }
    // 投票已结束的信号：my_voted=true（本人已投）或 phase 即将切换
    // 简单方案：当票型首次出现且本人已投时触发悬念
    if (gameState.my_voted && !voteSuspenseActive && !voteSuspenseTimer) {
      setVoteSuspenseActive(true);
      const delay = gameState.vote_suspense_delay_ms || 3000;
      const timer = setTimeout(() => {
        setVoteSuspenseActive(false);
        setVoteSuspenseTimer(null);
      }, delay);
      setVoteSuspenseTimer(timer);
    }
    return () => {
      if (voteSuspenseTimer) {
        clearTimeout(voteSuspenseTimer);
      }
    };
  }, [gameState.vote_suspense, gameState.votes, gameState.my_voted, phase, voteSuspenseActive, voteSuspenseTimer]);

  // phase 切换时清理悬念状态
  useEffect(() => {
    if (phase !== 'vote') {
      setVoteSuspenseActive(false);
      if (voteSuspenseTimer) {
        clearTimeout(voteSuspenseTimer);
        setVoteSuspenseTimer(null);
      }
    }
  }, [phase]);
  useEffect(() => {
    if (phase !== 'dawn') {
      setAutoCountdown(DAWN_AUTO_START_MS / 1000);
      return;
    }
    setAutoCountdown(DAWN_AUTO_START_MS / 1000);
    const tick = setInterval(() => {
      setAutoCountdown((prev) => (prev > 0 ? prev - 1 : 0));
    }, 1000);
    const auto = setTimeout(() => {
      onFinish('start_day');
    }, DAWN_AUTO_START_MS);
    return () => {
      clearInterval(tick);
      clearTimeout(auto);
    };
    // onFinish 是稳定引用(包在 useCallback);phase 是核心触发条件
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [phase]);
  const aliveSeatCandidates: number[] = [];
  for (let i = 0; i < gameState.max_seat; i++) {
    const p = gameState.players[i];
    if (p && p.alive && i !== mySeat) aliveSeatCandidates.push(i);
  }

  if (phase === 'dawn') {
    const deaths = Array.isArray(gameState.last_night_deaths) ? gameState.last_night_deaths : [];
    const deathNames = deaths.map((s) => `#${s + 1}`);
    return (
      <div className="werewolf-action-panel" data-testid="werewolf-dawn-panel">
        <h4>🌅 黎明 · 昨夜死亡公告</h4>
        {deathNames.length === 0 ? (
          <p className="dawn-announcement">☀️ 昨夜是平安夜,无玩家死亡</p>
        ) : (
          <p className="dawn-announcement">
            ☠ 昨夜死亡:{deathNames.join('、')}(请等待其遗言后进入白天)
          </p>
        )}
        <div className="action-row">
          <button
            className="btn btn-primary"
            onClick={() => onFinish('start_day')}
            disabled={busy}
            data-testid="werewolf-start-day-button"
          >
            ☀️ 进入白天({autoCountdown}s)
          </button>
        </div>
      </div>
    );
  }

  if (phase === 'sheriff') {
    // R196 报告 P1: 死亡玩家不应看到完整竞选 UI(后端会用 ErrDeadPlayerAction 40112 兜底拒收,
    // 但 UI 误导玩家可参与,体验更糟)。死玩家仅展示观战提示,避免点错按钮。
    const isAlive = gameState.players?.[mySeat]?.alive ?? false;
    // §报告-20260804-03 BUG-03: 警长竞选**没有**轮流发言顺序 —— 所有存活玩家
    // 同时举手参选 + 同时投票(服务端 StartDay 显式把 SpeakTurnSeat 置为 NoSeat)。
    // 此前这里渲染 `当前发言:#{speak_turn_seat + 1}`,-1+1=0 画出不存在的 #0。
    const candidates = gameState.sheriff_candidates ?? [];
    if (!isAlive) {
      return (
        <div className="werewolf-action-panel" data-testid="werewolf-sheriff-dead-panel">
          <h4>⚖ 警长竞选</h4>
          <p className="dead-observer-hint">☠ 你已阵亡,无法参选或投票警长。请静观存活玩家竞选。</p>
          <p>
            已参选:
            {candidates.length === 0 ? '暂无人参选' : candidates.map((s) => `#${s + 1}`).join('、')}
          </p>
        </div>
      );
    }

    // §报告-20260804-03 BUG-04/05: 参选与投票状态此前完全不下发,玩家点完按钮
    // UI 零变化,与「按钮坏了」无法区分。现在由服务端 sheriff_candidates /
    // my_voted / my_vote_target 三个字段驱动可见反馈。
    const iAmCandidate = candidates.includes(mySeat);
    const iVoted = gameState.my_voted === true;
    const myVoteTarget = gameState.my_vote_target ?? -1;
    // 只能投给已参选者,且不能投自己。无人参选时不渲染 chip(给出明确提示)。
    const votableSeats = candidates.filter((s) => s !== mySeat);

    return (
      <div className="werewolf-action-panel" data-testid="werewolf-sheriff-panel">
        <h4>⚖ 警长竞选</h4>
        <p className="sheriff-hint">
          所有存活玩家可同时参选与投票(本阶段无发言顺序)。得票最多者当选警长。
        </p>
        <div className="action-row">
          <button
            className="btn btn-primary"
            onClick={() => onSheriff('candidate')}
            disabled={busy || iAmCandidate}
            data-testid="werewolf-sheriff-candidate"
          >
            {iAmCandidate ? '✅ 已参选' : '🙋 参选警长'}
          </button>
        </div>

        <div className="sheriff-candidates">
          <h5>候选人({candidates.length})</h5>
          {candidates.length === 0 ? (
            <p className="sheriff-empty-hint">暂无人参选。无人参选时结算 → 本局无警长。</p>
          ) : (
            <p>{candidates.map((s) => `#${s + 1}${s === mySeat ? '(你)' : ''}`).join('、')}</p>
          )}
        </div>

        {votableSeats.length > 0 && (
          <div className="seat-grid">
            {votableSeats.map((s) => (
              <button
                key={s}
                type="button"
                className={`seat-chip ${target === s ? 'is-selected' : ''} ${myVoteTarget === s ? 'is-voted' : ''}`}
                onClick={() => setTarget(s)}
                disabled={busy || iVoted}
                data-testid={`werewolf-sheriff-vote-target-${s}`}
              >
                #{s + 1}
              </button>
            ))}
            {/* BUG-HUNTER2-P1-01 (2026-08-07): 警长竞选 chips 必须与候选
                文本保持一致 —— 人类自身若参选,候选文本会显示 "#12(你)"
                但下方投票 chip 被滤掉,造成「数据 vs 渲染」不一致。
                规则:不可投自己(后端 DayVote target==actor 拒绝),故
                self chip 显示为禁用态并附「你已参选」说明,既保持 UI
                对称,又明示「不可投自己」的原因。 */}
            {iAmCandidate && (
              <button
                type="button"
                className="seat-chip is-self is-disabled"
                disabled
                aria-disabled="true"
                title="你已参选,不可投自己"
                data-testid="werewolf-sheriff-vote-target-self"
              >
                #{mySeat + 1}(你)
              </button>
            )}
          </div>
        )}

        <div className="action-row">
          <button
            className="btn btn-secondary"
            onClick={() => target !== null && onSheriff('vote', target)}
            disabled={busy || iVoted || target === null}
            data-testid="werewolf-sheriff-vote"
          >
            {iVoted
              ? `✅ 已投 #${myVoteTarget >= 0 ? myVoteTarget + 1 : '弃票'}`
              : `投票警长${target === null ? '' : ` → #${target + 1}`}`}
          </button>
          {/* §报告-20260804-03 BUG-01: 此前这里发 onFinish('start_day'),而
              StartDay 首行就要求 Phase==PhaseDawn,在竞选阶段 100% 返回
              「not at dawn」—— 人类玩家没有任何合法出口,只能等 240s watchdog。
              真正的出口是 sheriff_elect(结算票数并选出警长)。 */}
          <button
            className="btn btn-warning"
            onClick={() => onSheriff('elect')}
            disabled={busy}
            data-testid="werewolf-sheriff-elect"
          >
            ⚖ 结束竞选 → 宣布警长
          </button>
          {/* 2026-07-10 12 人局: 预言家警长在警长竞选阶段即可声明警徽流。 */}
          {gameState.sheriff_seat === mySeat && gameState.my_role === 'seer' && onOpenSheriffStream && (
            <button
              className="btn btn-primary"
              onClick={() => onOpenSheriffStream()}
              disabled={busy}
              data-testid="werewolf-open-sheriff-stream"
            >
              🎖 警徽流声明
            </button>
          )}
        </div>

        {/* §报告-20260804-03 BUG-05: 与 vote 阶段(见下方)同构的实时票数块。
            此前 sheriff 分支完全不渲染 votes,投票成功也看不到任何变化。 */}
        {gameState.votes && Object.keys(gameState.votes).length > 0 && (
          <div className="tally">
            <h5>实时票数</h5>
            <ul>
              {Object.entries(gameState.votes).map(([k, v]) => (
                <li key={k}>#{parseInt(k, 10) + 1} → {v} 票</li>
              ))}
            </ul>
          </div>
        )}
      </div>
    );
  }

  if (phase === 'speak') {
    // 2026-08-09 §20260808-03 — 死态守卫(缺陷 D):死者不该看到「我说完了」
    // /「发起投票」/骑士决斗按钮,改为等待当前发言者 + 观察者提示。
    if (iAmDead) {
      const currentSpeakSeat = (gameState.speak_turn_seat ?? -1) + 1;
      const phaseText = phaseLabelKey(t, phase) ?? phase;
      return (
        <div className="werewolf-action-panel werewolf-action-panel--dead" data-testid="werewolf-dead-speak-panel">
          <h4>☠ {t('werewolf.dead.title', { phase: phaseText })}</h4>
          <p>{currentSpeakSeat > 0 ? t('werewolf.dead.speakHint', { seat: currentSpeakSeat }) : '—'}</p>
          <p className="dead-observer-hint">{t('werewolf.dead.actions.title', { phase: phaseText })}</p>
          <ul className="dead-observer-actions">
            <li>{t('werewolf.dead.actions.watch')}</li>
            <li>{t('werewolf.dead.actions.lastwords')}</li>
          </ul>
        </div>
      );
    }
    // §198 骑士决斗 UI 触发条件:
    //   my_role === 'knight' && 存活 && 当前是发言回合(为我自己) &&
    //   未用过(KnightDuelUsed 本地 state — 前端不暴露该字段,所以用 !role_used 标志 —
    //   **不**, 这里我们用更可靠的 proxy: 服务端 `RolePubliclyRevealed(my_seat)` 时为 true
    //   即已用。所以 !role_revealed 推断"未用过"。
    //
    // 注:这种本地判断只能粗略防止再点击;最终服务端 KnightDuelUsed 字段是权威 ——
    // 已经用过的骑士即使点了也会收到 ErrValidationFailed,前端 formError 会展示。
    const knightCanDuel =
      gameState.my_role === 'knight' &&
      (gameState.players?.[mySeat]?.alive ?? false) &&
      gameState.speak_turn_seat === mySeat &&
      gameState.players?.[mySeat]?.role_revealed !== true &&
      onDuel !== undefined;
    return (
      <div className="werewolf-action-panel">
        <h4>🗣 白天发言</h4>
        <p>当前发言:#{(gameState.speak_turn_seat ?? -1) + 1}</p>
        {gameState.speak_turn_seat === mySeat && (
          <button
            className="btn btn-primary"
            onClick={() => onFinish('speak')}
            disabled={busy}
          >
            ✅ 我说完了
          </button>
        )}
        {/* 2026-07-10 12 人局: 预言家警长在发言阶段也可声明警徽流(覆盖既有声明)。 */}
        {gameState.sheriff_seat === mySeat && gameState.my_role === 'seer' && onOpenSheriffStream && (
          <button
            className="btn btn-secondary"
            onClick={() => onOpenSheriffStream()}
            disabled={busy}
            data-testid="werewolf-open-sheriff-stream-speak"
          >
            🎖 警徽流声明
          </button>
        )}
        {/* 2026-07-11: 预言家在发言阶段可发起投票 */}
        {/* R176 报告 P1: 增加 alive 检查,死亡玩家不应看到按钮(后端用 ErrDeadPlayerAction 40112 兜底) */}
        {gameState.my_role === 'seer' && !gameState.vote_proposed && (gameState.players?.[mySeat]?.alive ?? false) && onProposeVote && (
          <button
            className="btn btn-warning"
            onClick={() => onProposeVote()}
            disabled={busy}
            data-testid="werewolf-propose-vote"
          >
            📢 发起投票
          </button>
        )}
        {gameState.vote_proposed && (
          <div className="werewolf-vote-proposed-banner">
            ⚡ 预言家已发起投票,即将进入投票阶段
          </div>
        )}
        {/* §20260811-06 U1 — Knight 决斗二次确认(替换原 window.confirm 简化版)。
            主按钮不再直接 onDuel,而是先 setState({pending:true, target}) 弹出
            ConfirmModal;用户在 modal 中点「确认」才真正发起 WS knight_duel 帧。
            不可逆操作(命中/自决后立即翻牌公开身份)必须经 §27 ConfirmModal 兜底。 */}
        {knightCanDuel && (
          <details className="knight-duel-panel" data-testid="werewolf-knight-duel-panel">
            <summary className="btn btn-warning">⚔️ 骑士决斗(每局限一次)</summary>
            <div className="action-row knight-duel-hint">
              <p>⚠️ 翻牌对外公开身份;命中狼 → 对方出局;否则 → 你自己出局。</p>
            </div>
            <div className="seat-grid">
              {aliveSeatCandidates.map((s) => (
                <button
                  key={s}
                  type="button"
                  className={`seat-chip ${target === s ? 'is-selected' : ''}`}
                  onClick={() => setTarget(s)}
                  disabled={busy}
                  data-testid={`werewolf-knight-target-${s}`}
                >
                  #{s + 1}
                </button>
              ))}
            </div>
            <div className="action-row">
              <button
                className="btn btn-primary"
                onClick={() => target !== null && setConfirmDuelTarget(target)}
                disabled={busy || target === null}
                data-testid="werewolf-knight-duel-confirm"
              >
                ⚔️ 对 #{target === null ? '?' : target + 1} 发动决斗
              </button>
              <button
                className="btn btn-secondary"
                onClick={() => onDuel(-1)}
                disabled={busy}
                data-testid="werewolf-knight-duel-skip"
              >
                放弃本轮(技能保留)
              </button>
            </div>
          </details>
        )}
        {/* §20260811-06 U1 — Knight 决斗二次确认弹层。danger=true 让确认按钮红色,
            突出「不可逆」风险。messageKey 走 i18n 三语(zh-CN/en/ja 同款文案)。 */}
        {knightCanDuel && confirmDuelTarget !== null && (
          <ConfirmModal
            messageKey="werewolf.knight.confirmDialog"
            message={`⚔️ 对 ${confirmDuelTarget + 1} 号 发动决斗?\n\n命中狼 → 对方出局\n否则 → 你自己出局\n\n⚠️ 此操作不可撤销,身份将公开。`}
            confirmKey="werewolf.knight.confirmCta"
            confirmLabel="⚔️ 确认决斗"
            cancelKey="common.cancel"
            danger
            onConfirm={() => {
              const t = confirmDuelTarget;
              setConfirmDuelTarget(null);
              if (onDuel && t !== null) onDuel(t);
            }}
            onCancel={() => setConfirmDuelTarget(null)}
          />
        )}
      </div>
    );
  }

  if (phase === 'vote') {
    // 2026-08-09 §20260808-03 — 死态守卫:显示实时票数(只读) + 提示等待。
    if (iAmDead) {
      const tally = gameState.votes && Object.keys(gameState.votes).length > 0
        ? Object.entries(gameState.votes).map(([k, v]) => `#${Number(k) + 1}:${v}`).join(' / ')
        : '—';
      const phaseText = phaseLabelKey(t, phase) ?? phase;
      return (
        <div className="werewolf-action-panel werewolf-action-panel--dead" data-testid="werewolf-dead-vote-panel">
          <h4>☠ {t('werewolf.dead.title', { phase: phaseText })}</h4>
          <p>{t('werewolf.dead.voteHint', { tally })}</p>
          <p className="dead-observer-hint">{t('werewolf.dead.actions.title', { phase: phaseText })}</p>
          <ul className="dead-observer-actions">
            <li>{t('werewolf.dead.actions.watch')}</li>
            <li>{t('werewolf.dead.actions.lastwords')}</li>
          </ul>
        </div>
      );
    }
    return (
      <div className="werewolf-action-panel">
        <h4>🗳 白天投票放逐</h4>
        <div className="seat-grid">
          {aliveSeatCandidates.map((s) => (
            <button
              key={s}
              type="button"
              className={`seat-chip ${target === s ? 'is-selected' : ''}`}
              onClick={() => setTarget(s)}
              disabled={busy}
              data-testid={`werewolf-vote-target-${s}`}
            >
              #{s + 1}
            </button>
          ))}
        </div>
        <div className="action-row">
          <button
            className="btn btn-primary"
            onClick={() => target !== null && onVote(target)}
            disabled={busy || target === null}
          >
            {target === null ? '请选择投票目标' : `投 #${target + 1}`}
          </button>
          <button
            className="btn btn-secondary"
            onClick={() => onFinish('vote', 1)}
            disabled={busy}
          >
            ⏭ 投票结束(无平票则直接出结果)
          </button>
        </div>
        {gameState.votes && Object.keys(gameState.votes).length > 0 && (
          <div className="tally">
            <h5>{voteSuspenseActive ? '🗳 投票已结束…' : '实时票数'}</h5>
            <ul className={voteSuspenseActive ? 'vote-suspense' : ''}>
              {Object.entries(gameState.votes).map(([k, v]) => (
                <li key={k}>
                  #{parseInt(k, 10) + 1} → {voteSuspenseActive ? '?' : `${v} 票`}
                </li>
              ))}
            </ul>
            {voteSuspenseActive && (
              <p className="vote-suspense-hint">⏳ 完整票型即将揭晓…</p>
            )}
          </div>
        )}
      </div>
    );
  }

  // 2026-07-10 12 人局: 白痴翻牌阶段 —— 白痴本人通过 IdiotRevealPanel 决策,本处
  // 仅提供进度提示;预言家警长同白天可继续声明警徽流(翻牌后仍跳 night_wolves)。
  if (phase === 'idiot_reveal') {
    // 2026-08-09 §20260808-03 — 死态守卫:白痴翻牌阶段,除白痴本人外其他死亡玩家
    // 不应被误显示「等待 #N 决定」白板;统一为观察者提示。
    if (iAmDead) {
      const elimSeat = (gameState.day_eliminated ?? -1) + 1;
      const phaseText = phaseLabelKey(t, phase) ?? phase;
      return (
        <div className="werewolf-action-panel werewolf-action-panel--dead" data-testid="werewolf-dead-idiot-panel">
          <h4>☠ {t('werewolf.dead.title', { phase: phaseText })}</h4>
          <p>{elimSeat > 0 ? t('werewolf.dead.idiotHint', { seat: elimSeat }) : '—'}</p>
          <p className="dead-observer-hint">{t('werewolf.dead.actions.title', { phase: phaseText })}</p>
          <ul className="dead-observer-actions">
            <li>{t('werewolf.dead.actions.watch')}</li>
            <li>{t('werewolf.dead.actions.lastwords')}</li>
          </ul>
        </div>
      );
    }
    const isMyIdiotTurn = gameState.my_role === 'idiot' && gameState.day_eliminated === mySeat;
    return (
      <div className="werewolf-action-panel" data-testid="werewolf-idiot-reveal-panel">
        <h4>🃏 白痴翻牌中…</h4>
        <p>
          {isMyIdiotTurn
            ? '轮到你决策:请在全屏面板选择「翻牌 / 放弃」。'
            : `等待 ${gameState.day_eliminated >= 0 ? `#${gameState.day_eliminated + 1}` : '白痴'} 决定是否翻牌。`}
        </p>
        {gameState.sheriff_seat === mySeat && gameState.my_role === 'seer' && onOpenSheriffStream && (
          <button
            className="btn btn-primary"
            onClick={() => onOpenSheriffStream()}
            disabled={busy}
          >
            🎖 警徽流声明
          </button>
        )}
      </div>
    );
  }

  if (phase === 'hunter_shoot' && gameState.my_role === 'hunter') {
    // 2026-08-09 §20260808-03 — 缺陷 F:hunter_shoot 必然是死亡触发的合法操作,
    // 但需要在顶部加死态 hint,明示「死前的最后一击」。
    return (
      <div className="werewolf-action-panel" data-testid="werewolf-hunter-shoot-panel">
        <h4>🎯 猎人请决定开枪</h4>
        {iAmDead && (
          <p className="hunter-dead-hint" data-testid="werewolf-hunter-dead-hint">
            {t('werewolf.dead.hunterDeadHint')}
          </p>
        )}
        <div className="action-row">
          <button
            className="btn btn-secondary"
            onClick={() => onFinish('vote')} // 不开枪等同放弃
            disabled={busy}
          >
            不开枪
          </button>
          <div className="seat-grid">
            {aliveSeatCandidates.map((s) => (
              <button
                key={s}
                type="button"
                className={`seat-chip ${target === s ? 'is-selected' : ''}`}
                onClick={() => setTarget(s)}
                disabled={busy}
              >
                #{s + 1}
              </button>
            ))}
          </div>
        </div>
      </div>
    );
  }

  return null;
}
