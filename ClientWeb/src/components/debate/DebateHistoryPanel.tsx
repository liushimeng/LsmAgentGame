/**
 * 辩论比赛 发言历史面板 (2026-08-31 §20260831-05)
 *
 * 对齐 docs/辩论比赛/04 §3.4 SpeechPanel 设计 + §20260831-05:
 *   - 从 /api/games/debate/rooms/:id/history 拉取完整发言历史
 *   - 按阶段分组展示(立论/驳论/质询/自由辩/总结)
 *   - 可折叠/展开各阶段
 *   - 显示质询记录
 *
 * §13 SubAgent = frontend-dev:仅修改 ClientWeb/。
 */
import { useEffect, useState } from 'react';
import { debateService } from '@/api/debate';
import { reportGlobalError } from '@/services/globalError';
import type { DebateCrossExamEntry, DebateSpeech } from '@/types/debate';

interface Props {
  roomId: string;
}

const PHASE_ORDER = [
  'opening_argument',
  'rebuttal',
  'cross_examination',
  'cross_exam_summary',
  'free_debate',
  'closing_argument',
];

const PHASE_LABELS: Record<string, string> = {
  opening_argument: '开篇立论',
  rebuttal: '驳论',
  cross_examination: '质询',
  cross_exam_summary: '质询小结',
  free_debate: '自由辩论',
  closing_argument: '总结陈词',
};

export default function DebateHistoryPanel({ roomId }: Props) {
  const [speeches, setSpeeches] = useState<DebateSpeech[]>([]);
  const [crossExam, setCrossExam] = useState<DebateCrossExamEntry[]>([]);
  
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');
  const [expandedPhases, setExpandedPhases] = useState<Record<string, boolean>>({});

  useEffect(() => {
    setLoading(true);
    debateService
      .history(roomId)
      .then((data) => {
        setSpeeches(data.speeches ?? []);
        setCrossExam(data.cross_exam ?? []);
        
        // 默认展开所有阶段
        const init: Record<string, boolean> = {};
        for (const phase of PHASE_ORDER) {
          init[phase] = true;
        }
        setExpandedPhases(init);
      })
      .catch((e: Error) => {
        setErr(e.message);
        reportGlobalError({ message: e.message, severity: 'error' });
      })
      .finally(() => setLoading(false));
  }, [roomId]);

  const togglePhase = (phase: string) => {
    setExpandedPhases((prev) => ({ ...prev, [phase]: !prev[phase] }));
  };

  const speechesByPhase: Record<string, DebateSpeech[]> = {};
  for (const sp of speeches) {
    if (!speechesByPhase[sp.phase]) {
      speechesByPhase[sp.phase] = [];
    }
    speechesByPhase[sp.phase].push(sp);
  }

  if (loading) {
    return (
      <div className="debate-history-panel">
        <h3>📜 发言历史</h3>
        <p>加载中...</p>
      </div>
    );
  }

  return (
    <div className="debate-history-panel">
      <h3>📜 发言历史</h3>

      {err && <div className="history-error">{err}</div>}

      {speeches.length === 0 && !err && (
        <p className="history-empty">暂无发言记录</p>
      )}

      {PHASE_ORDER.map((phase) => {
        const phaseSpeeches = speechesByPhase[phase];
        if (!phaseSpeeches || phaseSpeeches.length === 0) return null;
        const expanded = expandedPhases[phase] ?? true;
        return (
          <div key={phase} className="history-phase">
            <button
              type="button"
              className="history-phase-header"
              onClick={() => togglePhase(phase)}
            >
              <span className="history-phase-toggle">{expanded ? '▼' : '▶'}</span>
              <span className="history-phase-label">{PHASE_LABELS[phase] ?? phase}</span>
              <span className="history-phase-count">{phaseSpeeches.length} 条</span>
            </button>
            {expanded && (
              <div className="history-phase-content">
                {phase === 'cross_examination'
                  ? renderCrossExam(crossExam)
                  : phaseSpeeches.map((sp) => (
                      <div key={sp.id} className="history-speech">
                        <div className="history-speech-header">
                          <span className={`stance-tag stance-tag--${sp.stance}`}>
                            {stanceLabel(sp.stance)}
                          </span>
                          <span className="history-speech-speaker">{sp.speaker_name}</span>
                        </div>
                        <p className="history-speech-content">{sp.content}</p>
                      </div>
                    ))}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

function renderCrossExam(entries: DebateCrossExamEntry[]) {
  if (entries.length === 0) {
    return <p className="history-empty">暂无质询记录</p>;
  }
  return (
    <div className="history-cross-exam">
      {entries.map((e) => (
        <div key={e.id} className={`history-cross-item${e.is_answer ? ' history-cross-item--answer' : ''}`}>
          <span className="history-cross-role">{e.is_answer ? '答' : '问'}:</span>
          <span className="history-cross-text">{e.is_answer ? e.answer : e.question}</span>
        </div>
      ))}
    </div>
  );
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
    default: return s;
  }
}
