/**
 * 辩论舞台主组件 (2026-08-31 §20260831-01)
 *
 * 对齐 docs/辩论比赛/04 §3.1 主舞台区域设计。
 */
import { useDebateStore } from '@/store/debate.store';

export default function DebateStage() {
  const { currentRoom, currentSpeech, phase, currentSpeaker } = useDebateStore();

  if (!currentRoom) {
    return (
      <div className="debate-stage debate-stage--loading">
        <div className="spinner" />
        <p>加载中...</p>
      </div>
    );
  }

  const phaseCn = currentRoom.phase_cn;

  return (
    <div className="debate-stage">
      {/* 辩题展示 */}
      <div className="stage-header">
        <h2 className="topic">「{currentRoom.topic.text}」</h2>
        <div className="phase-info">
          <span className="phase-tag">{phaseCn}</span>
          {currentRoom.time_remaining_sec > 0 && (
            <span className="timer">⏱ {formatTime(currentRoom.time_remaining_sec)}</span>
          )}
        </div>
      </div>

      {/* 当前发言者 */}
      {currentSpeech && (
        <div className="current-speaker">
          <div className="speaker-tag">🎤 {currentSpeech.speaker_name}</div>
          <div className="speech-content">
            <p>{currentSpeech.content}</p>
          </div>
        </div>
      )}

      {/* 当前阶段空闲 */}
      {!currentSpeech && phase === 'preparation' && (
        <div className="preparation">
          <p>双方正在审题构思...</p>
        </div>
      )}
      {!currentSpeech && phase === 'free_debate' && (
        <div className="free-debate-idle">
          <p>等待下一位辩手发言...</p>
          {currentSpeaker && <small>当前发言:{currentSpeaker}</small>}
        </div>
      )}
      {!currentSpeech && phase === 'result' && (
        <div className="result-pending">
          <p>评审中...</p>
        </div>
      )}
    </div>
  );
}

function formatTime(sec: number): string {
  const m = Math.floor(sec / 60);
  const s = sec % 60;
  return `${m}:${String(s).padStart(2, '0')}`;
}