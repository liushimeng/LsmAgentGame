/**
 * SheriffStreamPanel — 2026-07-10 13 人标准竞技局(默认)。
 *
 * 预言家警长在白天阶段(发言 / 警长竞选 / 黎明)声明「警徽流」—— 验人顺序。
 * 夜间死亡后好人据此在下一天黎明自动移交警徽(详见 docs/狼人杀13人标准局规则.md §7)。
 *
 * 暴露条件: 仅 seat === sheriff_seat 且 role === 'seer' 时启用「声明 / 撤回」按钮;
 * 其余玩家只能看到「当前警长已声明 N 段(空槽不公布目标座号)」摘要,不泄露验人对象。
 *
 * UI:
 *   - 展示当前警长 + 第一/第二警徽流槽位(已声明显示「X号」,空槽「未声明」)
 *   - 每个槽位: 存活玩家选择器 + 「声明」按钮;已声明时显示「撤回」(-1)
 *   - ESC 关闭(props.onClose)、倒计时(复用 PhaseClock 风格,来自 phase_extra)
 *   - i18n: 中/英/日
 *
 * 发送帧: game.werewolf_sheriff_stream { slot:1|2, target:-1|0..11 } —— 由
 * parent (DayControlPanel) 经 useWerewolf().sheriffStream 派发。
 */

import { useEffect, useMemo, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { WerewolfGameState } from '@/types/werewolf';

interface Props {
  gameState: WerewolfGameState;
  mySeat: number;
  /** 派发声明/撤回: slot=1 第一 / slot=2 第二;target=-1 撤回。 */
  onDeclare: (slot: 1 | 2, target: number) => void;
  onClose: () => void;
}

const fmtSeat = (seat: number): string => (seat < 0 ? '—' : `${seat + 1}号`);

export const SheriffStreamPanel: React.FC<Props> = ({ gameState, mySeat, onDeclare, onClose }) => {
  const t = useT();
  const sheriffSeat = gameState.sheriff_seat ?? -1;
  const isSeerSheriff = mySeat === sheriffSeat && gameState.my_role === 'seer';
  const streams = gameState.sheriff_streams ?? [];

  // 两槽位本地选中值(未声明用 -1)。
  const [slot1, setSlot1] = useState<number>(-1);
  const [slot2, setSlot2] = useState<number>(-1);

  // 同步服务端已声明状态为本地默认(仅在未操作时)。
  useEffect(() => {
    if (streams[0] !== undefined && streams[0] >= 0) setSlot1(streams[0]);
  }, [streams[0]]);
  useEffect(() => {
    if (streams[1] !== undefined && streams[1] >= 0) setSlot2(streams[1]);
  }, [streams[1]]);

  // ESC 关闭。
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  // 候选目标:当前存活的「非自己」玩家(警徽流不能验自己)。
  const candidates = useMemo(() => {
    const out: number[] = [];
    for (let i = 0; i < gameState.max_seat; i++) {
      if (i === sheriffSeat) continue;
      const p = gameState.players[i];
      if (p && p.alive) out.push(i);
    }
    return out;
  }, [gameState.players, gameState.max_seat, sheriffSeat]);

  const declared1 = streams[0] !== undefined && streams[0] >= 0 ? streams[0] : -1;
  const declared2 = streams[1] !== undefined && streams[1] >= 0 ? streams[1] : -1;

  return (
    <div className="sheriff-stream" data-testid="sheriff-stream-panel" role="dialog" aria-modal="true">
      <div className="sheriff-stream__card">
        <header className="sheriff-stream__header">
          <h3>🎖 {t('werewolf.sheriffStream.title')}</h3>
          <button className="sheriff-stream__close" onClick={onClose} aria-label={t('werewolf.sheriffStream.close')}>
            ×
          </button>
        </header>

        <p className="sheriff-stream__status">
          🎖 警长:<strong>#{sheriffSeat + 1}</strong>
          {' · '}
          {isSeerSheriff
            ? t('werewolf.sheriffStream.hint')
            : `已声明 ${(declared1 >= 0 ? 1 : 0) + (declared2 >= 0 ? 1 : 0)}/2 段`}
        </p>

        {isSeerSheriff && (
          <div className="sheriff-stream__slots">
            <SlotRow
              label={t('werewolf.sheriffStream.slot1')}
              candidates={candidates}
              selected={slot1}
              declared={declared1}
              onSelect={setSlot1}
              onDeclare={(target) => onDeclare(1, target)}
            />
            <SlotRow
              label={t('werewolf.sheriffStream.slot2')}
              candidates={candidates}
              selected={slot2}
              declared={declared2}
              onSelect={setSlot2}
              onDeclare={(target) => onDeclare(2, target)}
            />
          </div>
        )}

        {!isSeerSheriff && (
          <p className="sheriff-stream__readonly">
            <span>1️⃣ {t('werewolf.sheriffStream.slot1')}: {fmtSeat(declared1)}</span>
            <span>2️⃣ {t('werewolf.sheriffStream.slot2')}: {fmtSeat(declared2)}</span>
          </p>
        )}
      </div>
    </div>
  );
};

interface SlotRowProps {
  label: string;
  candidates: number[];
  selected: number;
  declared: number;
  onSelect: (seat: number) => void;
  onDeclare: (target: number) => void;
}

const SlotRow: React.FC<SlotRowProps> = ({ label, candidates, selected, declared, onSelect, onDeclare }) => {
  const t = useT();
  const isDeclared = declared >= 0;
  return (
    <div className="sheriff-stream__slot">
      <div className="sheriff-stream__slot-head">
        <span className="sheriff-stream__slot-label">{label}</span>
        <span className="sheriff-stream__slot-current">
          {isDeclared ? `✓ ${fmtSeat(declared)}` : <em>未声明</em>}
        </span>
      </div>
      <div className="seat-grid sheriff-stream__grid">
        {candidates.map((s) => (
          <button
            key={s}
            type="button"
            className={`seat-chip ${selected === s ? 'is-selected' : ''}`}
            onClick={() => onSelect(s)}
          >
            #{s + 1}
          </button>
        ))}
      </div>
      <div className="action-row">
        <button
          className="btn btn-primary btn-sm"
          onClick={() => selected >= 0 && onDeclare(selected)}
          disabled={selected < 0}
        >
          {t('werewolf.sheriffStream.declare')}
        </button>
        {isDeclared && (
          <button
            className="btn btn-secondary btn-sm"
            onClick={() => onDeclare(-1)}
          >
            {t('werewolf.sheriffStream.revoke')}
          </button>
        )}
      </div>
    </div>
  );
};

export default SheriffStreamPanel;
