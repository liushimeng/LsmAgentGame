/**
 * LastWordsStage — 2026-08-08 §20260808-02 遗言阶段全员可见进度条
 *
 * 修复缺陷 A/B(方案 §1.2):phase === 'death_lyric' 时,除当前遗言者本人
 * (看到 LastWordsPanel)外,其余座位玩家与观战者在主界面没有任何阶段专属 UI;
 * phase_extra.death_lyric{current_seat,total,done} 与 dead_list[].last_words_status
 * 自 2026-07-09 起就由服务端下发但零组件消费。
 *
 * 渲染条件:仅 phase === 'death_lyric'(其它阶段返回 null)。
 * 观战者同样渲染 —— 全部字段均为公开信息,无 spectator 分支。
 *
 * 数据来源(全部既有字段,后端零改动):
 *   - 当前座位 / 总数 / 已完成:phase_extra.death_lyric{current_seat,total,done}
 *   - 逐座位状态:phase_extra.dead_list[].last_words_status(spoken/skipped/pending/ineligible)
 *   - 倒计时:phase_extra.remaining_sec(1s 本地 setInterval tick,与 LastWordsPanel 同机制)
 *
 * 不做的事(方案 §3.1 / §4.8):
 *   - 不显示死者角色(dead_list[].role 由服务端按 §135 RolePubliclyRevealed 白名单
 *     脱敏裁决,前端不透传角色到该组件)。
 *   - 无本地持久 state,阶段结束组件立即卸载,倒计时 tick 随卸载清理。
 */

import { useEffect, useState } from 'react';
import type { WerewolfGameState } from '@/types/werewolf';
import { useT } from '@/hooks/useT';

interface Props {
  gameState: WerewolfGameState;
}

type ChipStatus = 'spoken' | 'speaking' | 'skipped' | 'pending';

/** 逐座位状态 chip 的修饰类 + emoji(状态色相沿用 §26.4 既有库:紫/灰)。 */
function chipMeta(status: ChipStatus): { cls: string; icon: string } {
  switch (status) {
    case 'spoken':
      return { cls: 'last-words-stage__chip--spoken', icon: '💀' };
    case 'speaking':
      return { cls: 'last-words-stage__chip--speaking', icon: '🕯' };
    case 'skipped':
      return { cls: 'last-words-stage__chip--skipped', icon: '⏭' };
    default:
      return { cls: 'last-words-stage__chip--pending', icon: '…' };
  }
}

/** 状态 → i18n 键。 */
function chipStatusKey(status: ChipStatus) {
  switch (status) {
    case 'spoken':
      return 'werewolf.lastWords.statusSpoken';
    case 'speaking':
      return 'werewolf.lastWords.nowSpeaking';
    case 'skipped':
      return 'werewolf.lastWords.statusSkipped';
    default:
      return 'werewolf.lastWords.statusPending';
  }
}

export function LastWordsStage({ gameState }: Props) {
  const t = useT();
  const phase = gameState.phase;
  // 1s tick 刷新倒计时(服务端给的是 1 帧快照;与 LastWordsPanel 同一机制)。
  const [, setTick] = useState(0);
  useEffect(() => {
    if (phase !== 'death_lyric') return;
    const id = setInterval(() => setTick((n) => n + 1), 1000);
    return () => clearInterval(id);
  }, [phase]);

  if (phase !== 'death_lyric') return null;

  const extra = gameState.phase_extra;
  const lyric = extra?.death_lyric;
  const currentSeat = lyric?.current_seat ?? -1;
  const total = Math.max(0, lyric?.total ?? 0);
  const done = Math.max(0, lyric?.done ?? 0);
  const remaining = Math.max(0, extra?.remaining_sec ?? 30);
  const deadList = (extra?.dead_list ?? []).filter(
    (d) => d && d.last_words_status !== 'ineligible',
  );
  const players = gameState.players ?? [];
  /** 座位昵称:bot 显示 agent_name,否则用 dead_list 下发的 account;空座位回落座位号。 */
  const seatName = (seat: number, account?: string) => {
    const p = players[seat];
    return p?.agent_name || account || `#${seat + 1}`;
  };

  return (
    <div className="last-words-stage" data-testid="last-words-stage">
      <header className="last-words-stage__header">
        <h4 className="last-words-stage__title">
          💀 {t('werewolf.lastWords.title')} · {t('werewolf.day' as any)} {gameState.day ?? 1}
        </h4>
        <span
          className={`last-words-stage__countdown${remaining <= 5 ? ' is-critical' : ''}`}
        >
          ⏱ {remaining}s
        </span>
      </header>

      <div className="last-words-stage__progress">
        <div className="last-words-stage__dots" aria-hidden="true">
          {Array.from({ length: total }, (_, i) => {
            // done 实心、当前半填、其余空心。
            const cls =
              i < done ? 'is-done' : i === done ? 'is-current' : 'is-pending';
            return <span key={i} className={`last-words-stage__dot ${cls}`} />;
          })}
        </div>
        <span className="last-words-stage__progress-text">
          {t('werewolf.lastWords.progress', { done, total })}
        </span>
        {currentSeat >= 0 && (
          <span className="last-words-stage__now-speaking">
            🕯 {t('werewolf.lastWords.nowSpeaking')}: #{currentSeat + 1}{' '}
            {seatName(currentSeat)}
          </span>
        )}
      </div>

      {deadList.length > 0 && (
        <ul className="last-words-stage__chips">
          {deadList.map((d) => {
            const status: ChipStatus =
              d.seat === currentSeat
                ? 'speaking'
                : d.last_words_status === 'spoken'
                  ? 'spoken'
                  : d.last_words_status === 'skipped'
                    ? 'skipped'
                    : 'pending';
            const meta = chipMeta(status);
            return (
              <li
                key={d.seat}
                className={`last-words-stage__chip ${meta.cls}`}
                data-testid={`last-words-chip-${d.seat}`}
              >
                <span className="last-words-stage__chip-icon" aria-hidden="true">
                  {meta.icon}
                </span>
                <span className="last-words-stage__chip-name">
                  #{d.seat + 1} {seatName(d.seat, d.account)}
                </span>
                <span className="last-words-stage__chip-status">
                  {t(chipStatusKey(status) as any)}
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

export default LastWordsStage;
