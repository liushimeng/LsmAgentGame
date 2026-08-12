/**
 * IdiotRevealPanel — 2026-07-10 12 人局新增。
 *
 * 当 PhaseVote 最高票为白痴时进入 PhaseIdiotReveal,白痴玩家可在本面板选择:
 *   - 「翻牌」(reveal):翻开身份牌免予出局,丧失投票权与被投票权,继续发言;跳回 night_wolves
 *   - 「放弃」(skip):正常放逐,保留遗言资格
 *
 * UI:
 *   - 全屏遮罩(居中卡片),30s 倒计时(服务端 deadline 兜底,本地 watchdog)
 *   - 仅白痴本人(my_seat 为最高票白痴座位)可见「翻牌 / 放弃」;其他玩家看到等待提示
 *   - 结果动效:服务端广播 game.idiot_revealed 后显示翻牌/放弃横幅(对齐 §14)
 *   - ESC 关闭(仅结果态可关);i18n 中/英/日
 *
 * 发送帧: game.werewolf_idiot_reveal { choice:"reveal"|"skip" } —— 由
 * WerewolfGamePage 经 useWerewolf().idiotReveal 派发。
 */

import { useEffect, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { WerewolfGameState } from '@/types/werewolf';

interface Props {
  gameState: WerewolfGameState;
  mySeat: number;
  /** 白痴选择 reveal / skip。 */
  onChoose: (choice: 'reveal' | 'skip') => void;
}

const DEFAULT_DEADLINE_SEC = 30;

export const IdiotRevealPanel: React.FC<Props> = ({ gameState, mySeat, onChoose }) => {
  const t = useT();
  const idleRevealedSeats = gameState.idiot_revealed_seats ?? [];

  // 结果态: 任意白痴座位已翻牌/放弃 → 显示结果横幅。本面板仅 PhaseIdiotReveal
  // 全程可见,服务端广播 game.idiot_revealed 后由 gameState 带动。
  const [outcome, setOutcome] = useState<{ seat: number; revealed: boolean } | null>(null);
  useEffect(() => {
    // 取最近一次等同于 my_seat 的结果;若无,按已翻牌集合推断。
    if (idleRevealedSeats.includes(mySeat)) {
      setOutcome({ seat: mySeat, revealed: true });
    }
  }, [idleRevealedSeats, mySeat]);

  // 30s 本地倒计时(兜底,服务端 deadline 推进为准)。
  const [remaining, setRemaining] = useState<number>(DEFAULT_DEADLINE_SEC);
  useEffect(() => {
    setRemaining(DEFAULT_DEADLINE_SEC);
    const id = setInterval(() => {
      setRemaining((prev) => (prev > 0 ? prev - 1 : 0));
    }, 1000);
    return () => clearInterval(id);
  }, [gameState.phase_extra?.phase_deadline_at]);

  // ESC 关闭:仅在结果态生效(避免玩家误触跳过决策)。
  useEffect(() => {
    if (!outcome) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        // 触发父级关闭: 将 outcome 提交为「已观看」 — 简单起见自动关闭整局遮罩,
        // 这里通过 window 自定义事件通知 GamePage。
        window.dispatchEvent(new CustomEvent('idiot-reveal-closed'));
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [outcome]);

  const isSpectator = mySeat < 0;
  const isMyIdiotTurn =
    !isSpectator &&
    gameState.my_role === 'idiot' &&
    (gameState.tied_players ?? []).length === 0 &&
    // 最高票判定由服务端权威,前端仅靠「自己为白痴 + 阶段进入 idiot_reveal」推断;
    // 多个白痴不会出现(牌组唯一)。
    gameState.phase === 'idiot_reveal' &&
    gameState.day_eliminated === mySeat;

  // 是否可操作: 我的白痴回合且尚无结果。
  const canAct = isMyIdiotTurn && !outcome;

  return (
    <div className="idiot-reveal" data-testid="idiot-reveal-panel">
      <div className="idiot-reveal__card">
        <header className="idiot-reveal__header">
          <h2>🃏 {t('werewolf.idiotReveal.title')}</h2>
          <span className="idiot-reveal__countdown">
            ⏱ {t('werewolf.idiotReveal.deadlineHint', { sec: remaining })}
          </span>
        </header>

        {outcome ? (
          <div className={`idiot-reveal__outcome ${outcome.revealed ? 'is-reveal' : 'is-skip'}`}>
            {outcome.revealed
              ? t('werewolf.idiotReveal.done.reveal', { seat: outcome.seat + 1 })
              : t('werewolf.idiotReveal.done.skip', { seat: outcome.seat + 1 })}
          </div>
        ) : (
          <>
            <p className="idiot-reveal__prompt">{t('werewolf.idiotReveal.prompt')}</p>
            {canAct ? (
              <div className="idiot-reveal__actions">
                <button
                  type="button"
                  className="btn btn-primary"
                  onClick={() => onChoose('reveal')}
                  data-testid="idiot-reveal-reveal-btn"
                >
                  {t('werewolf.idiotReveal.revealBtn')}
                </button>
                <button
                  type="button"
                  className="btn btn-secondary"
                  onClick={() => onChoose('skip')}
                  data-testid="idiot-reveal-skip-btn"
                >
                  {t('werewolf.idiotReveal.skipBtn')}
                </button>
              </div>
            ) : (
              <p className="idiot-reveal__spectator">
                {isSpectator
                  ? t('werewolf.idiotReveal.spectatorHint')
                  : t('werewolf.idiotReveal.prompt')}
              </p>
            )}
          </>
        )}
      </div>
    </div>
  );
};

export default IdiotRevealPanel;
