/**
 * 辩论比赛 观众提问面板 (2026-08-31 §20260831-05)
 *
 * 对齐 docs/辩论比赛/04 §6.1 观众交互 + §20260831-05:
 *   - 观众可向裁判 Agent 提问(走 debate.spectator_question WS 帧)
 *   - 问题广播给所有观众(含提问者自己)
 *   - 仅在 SpectatorConfig.AllowSpectatorQuestion=true 时展示
 *
 * §20260831-06 — 观众提问闭环:
 *   - 提问入房间队列,评审阶段注入裁判 prompt
 *   - 裁判 answer_spectator 工具回答 → debate.spectator_answer 帧
 *   - 本面板按 question_id 把回答渲染在对应提问下方
 *
 * §13 SubAgent = frontend-dev:仅修改 ClientWeb/。
 */
import { useEffect, useState } from 'react';
import { wsClient } from '@/services/ws';
import { useDebateStore } from '@/store/debate.store';

interface Props {
  roomId: string;
}

interface QuestionEntry {
  question_id: string;
  user_id: string;
  text: string;
  timestamp: number;
}

export default function DebateSpectatorQuestionPanel({ roomId }: Props) {
  const spectatorConfig = useDebateStore((s) => s.currentRoom?.spectator_config);
  // §20260831-06 — 裁判回答由 useDebate 写入 store(按 question_id 索引)。
  const spectatorAnswers = useDebateStore((s) => s.spectatorAnswers);
  const [questions, setQuestions] = useState<QuestionEntry[]>([]);
  const [inputText, setInputText] = useState('');
  const [sendErr, setSendErr] = useState('');

  const allowQuestion = spectatorConfig?.allow_spectator_question ?? false;

  useEffect(() => {
    const unsub = wsClient.on((env) => {
      if (env.type !== 'debate.spectator_question') return;
      const p = env.payload as Record<string, unknown>;
      if (p.room_id !== roomId) return;
      const entry: QuestionEntry = {
        question_id: (p.question_id ?? '') as string,
        user_id: (p.user_id ?? '') as string,
        text: (p.text ?? '') as string,
        timestamp: (p.timestamp ?? Date.now()) as number,
      };
      setQuestions((prev) => [...prev.slice(-19), entry]);
    });
    return unsub;
  }, [roomId]);

  if (!allowQuestion) {
    return null;
  }

  const handleSend = () => {
    const text = inputText.trim();
    if (!text) return;
    if (text.length > 200) {
      setSendErr('问题长度不能超过 200 字');
      return;
    }
    setSendErr('');
    wsClient.send('debate.spectator_question', {
      room_id: roomId,
      text,
    });
    setInputText('');
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  return (
    <div className="debate-spectator-questions">
      <header className="spectator-questions-header">
        <span className="spectator-questions-icon">❓</span>
        <h3>向裁判提问</h3>
        <span className="spectator-questions-hint">问题将在评审环节被裁判看到</span>
      </header>

      <div className="spectator-questions-list">
        {questions.length === 0 ? (
          <p className="spectator-questions-empty">还没有观众提问,向裁判提问吧...</p>
        ) : (
          <ul>
            {questions.map((q, idx) => {
              const answer = q.question_id ? spectatorAnswers[q.question_id] : undefined;
              return (
                <li key={q.question_id || `${q.timestamp}-${idx}`} className="spectator-question-item">
                  <div className="question-row">
                    <span className="question-user">{maskUserID(q.user_id)}:</span>
                    <span className="question-text">{q.text}</span>
                    <span className="question-time">{formatTime(q.timestamp)}</span>
                  </div>
                  {answer && (
                    <div className="question-answer">
                      <span className="question-answer-badge">⚖️ 裁判{answer.answer_judge_id + 1}</span>
                      <span className="question-answer-text">{answer.answer}</span>
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </div>

      <div className="spectator-questions-input">
        <input
          type="text"
          value={inputText}
          onChange={(e) => setInputText(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="输入问题(≤200 字,Enter 发送)"
          maxLength={200}
          className="spectator-question-input"
        />
        <button
          type="button"
          className="btn-primary btn-sm"
          onClick={handleSend}
          disabled={!inputText.trim()}
        >
          发送
        </button>
      </div>
      {sendErr && <div className="spectator-questions-error">{sendErr}</div>}
    </div>
  );
}

function maskUserID(userID: string): string {
  if (!userID) return '匿名';
  if (userID.length <= 4) return '***';
  return userID.slice(0, 2) + '***' + userID.slice(-2);
}

function formatTime(ts: number): string {
  const d = new Date(ts);
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  return `${hh}:${mm}`;
}
