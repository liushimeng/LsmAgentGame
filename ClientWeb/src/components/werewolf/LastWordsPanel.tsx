/**
 * LastWordsPanel — 2026-07-21 §人类玩家操作重构
 *
 * 人类遗言面板。仅在 phase === 'death_lyric' 且 gameState.my_seat === DeathLyricCurrent
 * 时渲染(由 WerewolfGamePage 控制)。其它阶段/非自己回合时组件不渲染。
 *
 * 行为:
 *   - 显示「💀 你的遗言」标题 + 30s 倒计时
 *   - textarea 最长 200 字(对应服务端 Action_LastWords 校验)
 *   - 「📤 发送遗言」调 onSpeak(text) → ws frame game.werewolf_last_words
 *   - 「⏭ 放弃遗言」调 onSkip() → ws frame game.werewolf_last_words {choice:"skip"}
 *   - 提交后 textarea 清空 + busy=true 锁住按钮 500ms(对齐其它 Action_* 节流)
 */

import { useEffect, useRef, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { WerewolfGameState } from '@/types/werewolf';

interface Props {
  gameState: WerewolfGameState;
  onSpeak: (text: string) => void;
  onSkip: () => void;
  busy: boolean;
}

const MAX_TEXT = 200;

export function LastWordsPanel({ gameState, onSpeak, onSkip, busy }: Props) {
  const t = useT();
  const phase = gameState.phase;
  const mySeat = gameState.my_seat;
  const [text, setText] = useState('');
  // §20260823-02 P9 — 提交成功后(busy 等待阶段推进期间)渲染紧凑「已提交」态,
  // 避免全表单定格;若 busy 回落而阶段未推进(被拒/超时)则恢复表单可重试。
  const [submitted, setSubmitted] = useState(false);
  const busyRef = useRef(false);
  useEffect(() => {
    if (busy) {
      busyRef.current = true;
    } else if (busyRef.current) {
      busyRef.current = false;
      setSubmitted(false);
    }
  }, [busy]);
  // 1s tick for countdown (服务端给的是 1 帧快照)
  const [_, setTick] = useState(0);
  useEffect(() => {
    if (phase !== 'death_lyric') return;
    const t = setInterval(() => setTick((n) => n + 1), 1000);
    return () => clearInterval(t);
  }, [phase]);

  // 仅 death_lyric + 自己座位 = 当前遗言座位时渲染
  if (phase !== 'death_lyric') return null;
  const currentSeat = gameState.phase_extra?.death_lyric?.current_seat ?? -1;
  if (mySeat !== currentSeat) return null;

  // §20260823-02 P9 — 紧凑已提交态(点击可恢复表单重试,兜底服务端拒收场景)。
  if (submitted) {
    return (
      <div
        className="last-words-panel ww-cap-submitted"
        role="button"
        tabIndex={0}
        onClick={() => setSubmitted(false)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            setSubmitted(false);
          }
        }}
        data-testid="last-words-submitted"
      >
        {t('werewolf.panel.submitted')}
      </div>
    );
  }

  // 倒计时:用 phase_extra.remaining_sec(快照)+ 自 tick 同步刷新
  const remaining = Math.max(0, gameState.phase_extra?.remaining_sec ?? 30);
  const trimmed = text.trim();
  const tooLong = trimmed.length > MAX_TEXT;
  const tooShort = trimmed.length === 0;

  const submit = () => {
    if (tooLong || tooShort || busy) return;
    onSpeak(trimmed);
    setText('');
    setSubmitted(true);
  };

  const skip = () => {
    if (busy) return;
    onSkip();
    setSubmitted(true);
  };

  return (
    <div className="last-words-panel" role="dialog" aria-label="遗言面板" data-testid="last-words-panel">
      <header className="last-words-panel__header">
        <h4>💀 你的遗言</h4>
        <span className={`last-words-panel__countdown${remaining <= 5 ? ' is-critical' : ''}`}>
          ⏱ {remaining}s
        </span>
      </header>
      <textarea
        className="last-words-panel__textarea"
        value={text}
        onChange={(e) => setText(e.target.value.slice(0, MAX_TEXT + 20))}
        placeholder="留下你的最后一句话..."
        maxLength={MAX_TEXT + 20}
        rows={4}
        disabled={busy}
        data-testid="last-words-textarea"
      />
      <footer className="last-words-panel__footer">
        <span className="last-words-panel__hint">
          {trimmed.length}/{MAX_TEXT} 字
        </span>
        <div className="last-words-panel__actions">
          <button
            type="button"
            className="btn btn-secondary"
            onClick={skip}
            disabled={busy}
            data-testid="last-words-skip"
          >
            ⏭ 放弃遗言
          </button>
          <button
            type="button"
            className="btn btn-primary"
            onClick={submit}
            disabled={busy || tooLong || tooShort}
            data-testid="last-words-send"
          >
            📤 发送遗言
          </button>
        </div>
      </footer>
    </div>
  );
}
