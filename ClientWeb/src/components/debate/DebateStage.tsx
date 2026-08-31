/**
 * 辩论舞台主组件 (2026-08-31 §20260831-01 + §20260831-04)
 *
 * 对齐 docs/辩论比赛/04 §3.1 主舞台区域设计。
 *
 * §20260831-04 增补:
 *   - 当前发言旁加 👍 点赞按钮(走 debate.like WS 帧)
 *   - 当前发言下方折叠显示 internal_thought(§04 §3.1「Agent 思考」面板)
 *     仅在 SpectatorConfig.RevealAgentThought=true 时展示
 *   - 准备阶段 / 自由辩论空挡 / 评审阶段空挡继续保留
 *
 * §20260831-11 — R8 P1-A 修复:
 *   - 阶段标签 / 计时器改为优先读顶层 phase / timeRemaining(WS 帧实时更新),
 *     currentRoom.phase_cn 仅作 fallback,新增 PHASE_CN 本地映射兜底
 */
import { useMemo } from 'react';
import { wsClient } from '@/services/ws';
import { useDebateStore } from '@/store/debate.store';

/**
 * §20260831-11(P1-A 修复)— phase → 中文兜底映射。
 * 与后端 ServerGo/game/debate/types.go 的 PhaseCN() 逐项一致;
 * 优先用「顶层 phase 本地映射」,store 同步写入值 / currentRoom.phase_cn 作 fallback,
 * 保证 WS debate.phase 帧到达后标签立即推进(不再依赖 HTTP detail 初值)。
 */
const PHASE_CN: Record<string, string> = {
  filling: '等待开始',
  preparation: '赛前准备',
  opening_argument: '开篇立论',
  rebuttal: '驳论',
  cross_examination: '质询',
  cross_exam_summary: '质询小结',
  free_debate: '自由辩论',
  closing_argument: '总结陈词',
  judging: '裁判评审',
  result: '公布结果',
  game_over: '对局结束',
};

export default function DebateStage() {
  const {
    currentRoom,
    currentSpeech,
    phase,
    timeRemaining,
    currentSpeaker,
    agentThoughts,
    likedSpeeches,
    toggleLike,
  } = useDebateStore();

  const revealThought = currentRoom?.spectator_config?.reveal_agent_thought ?? false;

  const thoughtText = useMemo(() => {
    if (!currentSpeech) return '';
    const key = `${currentSpeech.team_id}:${currentSpeech.seat}`;
    // 优先取 store 中"实时"推送的 thought,fallback 到当前 Speech 自带的 internal_thought
    return agentThoughts[key] || currentSpeech.internal_thought || '';
  }, [agentThoughts, currentSpeech]);

  if (!currentRoom) {
    return (
      <div className="debate-stage debate-stage--loading">
        <div className="spinner" />
        <p>加载中...</p>
      </div>
    );
  }

  // §20260831-11(P1-A 修复):优先取顶层 phase 的本地映射(store 由 WS 帧实时更新),
  // currentRoom.phase_cn 仅作 fallback(未知 phase 时沿用服务端文案)。
  const phaseCn = PHASE_CN[phase] ?? currentRoom.phase_cn ?? phase;
  // 计时器同理:优先用顶层 timeRemaining(与 phase 同一帧更新),
  // 顶层为 0 时回退 currentRoom.time_remaining_sec。
  const timeRemainingSec = timeRemaining > 0 ? timeRemaining : currentRoom.time_remaining_sec;
  const isSpeechPhases =
    phase === 'opening_argument' ||
    phase === 'rebuttal' ||
    phase === 'cross_exam_summary' ||
    phase === 'closing_argument' ||
    phase === 'free_debate';

  const handleLike = () => {
    if (!currentSpeech) return;
    const added = toggleLike(currentSpeech.id);
    if (!added) return; // 取消点赞不发帧
    wsClient.send('debate.like', {
      room_id: currentRoom.room_id,
      speech_id: currentSpeech.id,
    });
  };

  const isLiked = currentSpeech ? (likedSpeeches[currentSpeech.id] ?? 0) > 0 : false;

  return (
    <div className="debate-stage">
      {/* 辩题展示 */}
      <div className="stage-header">
        <h2 className="topic">「{currentRoom.topic.text}」</h2>
        <div className="phase-info">
          <span className="phase-tag">{phaseCn}</span>
          {timeRemainingSec > 0 && (
            <span className="timer">⏱ {formatTime(timeRemainingSec)}</span>
          )}
        </div>
      </div>

      {/* 当前发言者 */}
      {currentSpeech && (
        <div className="current-speaker">
          <div className="speaker-tag">
            🎤 {currentSpeech.speaker_name}
            <span className={`stance-tag stance-tag--${currentSpeech.stance}`}>
              {stanceLabel(currentSpeech.stance)}
            </span>
          </div>
          <div className="speech-content">
            <p>{currentSpeech.content}</p>
          </div>
          <div className="speech-actions">
            <button
              type="button"
              className={`like-btn${isLiked ? ' like-btn--active' : ''}`}
              onClick={handleLike}
              aria-pressed={isLiked}
              title={isLiked ? '已点赞' : '点赞该发言'}
            >
              👍 <span className="like-count">
                {currentSpeech.id in likedSpeeches ? likedSpeeches[currentSpeech.id] : 0}
              </span>
            </button>
          </div>

          {/* Agent 内部思考(可折叠) */}
          {revealThought && thoughtText && (
            <details className="agent-thought">
              <summary>💭 Agent 思考</summary>
              <p>{thoughtText}</p>
            </details>
          )}
        </div>
      )}

      {/* 当前阶段空闲 */}
      {!currentSpeech && isSpeechPhases && phase === 'free_debate' && (
        <div className="free-debate-idle">
          <p>等待下一位辩手发言...</p>
          {currentSpeaker && <small>当前发言:{currentSpeaker}</small>}
        </div>
      )}
      {!currentSpeech && phase === 'preparation' && (
        <div className="preparation">
          <p>双方正在审题构思...</p>
        </div>
      )}
      {!currentSpeech && phase === 'result' && (
        <div className="result-pending">
          <p>评审中...</p>
        </div>
      )}
      {!currentSpeech && phase === 'judging' && (
        <div className="judging-pending">
          <p>3 位裁判正在评审...</p>
        </div>
      )}
      {!currentSpeech && phase === 'filling' && (
        <div className="filling-pending">
          <p>⏳ 等待房主点击「开始比赛」</p>
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

function stanceLabel(s: string): string {
  switch (s) {
    case 'pro': return '正方';
    case 'con': return '反方';
    case 'neutral': return '中立';
    case 'gov_upper': return '政府上院';
    case 'gov_lower': return '政府下院';
    case 'opp_upper': return '反对上院';
    case 'opp_lower': return '反对下院';
    case 'angle_1': return '角度一';
    case 'angle_2': return '角度二';
    case 'angle_3': return '角度三';
    case 'angle_4': return '角度四';
    case 'angle_5': return '角度五';
    default: return s;
  }
}
