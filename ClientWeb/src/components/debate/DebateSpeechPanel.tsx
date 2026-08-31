/**
 * 辩论发言历史面板 (2026-08-31 §20260831-01)
 *
 * 对齐 docs/辩论比赛/04 §3.4 发言历史面板设计。
 */
import { useState } from 'react';
import { useDebateStore } from '@/store/debate.store';
import type { DebateSpeech } from '@/types/debate';

const PHASE_GROUPS: { phase: string; cn: string }[] = [
  { phase: 'preparation', cn: '赛前准备' },
  { phase: 'opening_argument', cn: '① 开篇立论' },
  { phase: 'rebuttal', cn: '② 驳论' },
  { phase: 'cross_examination', cn: '③ 质询' },
  { phase: 'cross_exam_summary', cn: '③ 质询小结' },
  { phase: 'free_debate', cn: '④ 自由辩论' },
  { phase: 'closing_argument', cn: '⑤ 总结陈词' },
];

export default function DebateSpeechPanel() {
  const { speeches, currentRoom } = useDebateStore();
  const [expanded, setExpanded] = useState(true);

  if (!currentRoom) return null;

  return (
    <div className={`debate-speech-panel${expanded ? '' : ' collapsed'}`}>
      <header
        className="speech-panel__header"
        onClick={() => setExpanded(!expanded)}
      >
        📜 发言历史
        <span className="toggle">{expanded ? '▼' : '▶'}</span>
      </header>
      {expanded && (
        <div className="speech-panel__body">
          {PHASE_GROUPS.map((g) => {
            const phaseSpeeches = speeches.filter((s) => s.phase === g.phase);
            const [open, setOpen] = useState(true);
            return (
              <section key={g.phase} className="phase-section">
                <header
                  className="phase-section__header"
                  onClick={() => setOpen(!open)}
                >
                  {open ? '▼' : '▶'} {g.cn} ({phaseSpeeches.length})
                </header>
                {open && phaseSpeeches.length > 0 && (
                  <ul className="speech-list">
                    {phaseSpeeches.map((s) => (
                      <SpeechItem key={s.id} speech={s} />
                    ))}
                  </ul>
                )}
                {open && phaseSpeeches.length === 0 && (
                  <div className="empty">暂无发言</div>
                )}
              </section>
            );
          })}
        </div>
      )}
    </div>
  );
}

function SpeechItem({ speech }: { speech: DebateSpeech }) {
  const isPro = speech.stance === 'pro' || speech.stance === 'gov_upper' || speech.stance === 'gov_lower';
  return (
    <li className={`speech-item${speech.id ? '' : ''} ${isPro ? 'pro' : speech.stance === 'con' || speech.stance === 'opp_upper' || speech.stance === 'opp_lower' ? 'con' : 'neutral'}`}>
      <div className="speech-item__speaker">[{speech.speaker_name}]</div>
      <div className="speech-item__content">{speech.content}</div>
      {speech.internal_thought && (
        <details className="speech-item__thought">
          <summary>💭 思考过程</summary>
          <div>{speech.internal_thought}</div>
        </details>
      )}
    </li>
  );
}