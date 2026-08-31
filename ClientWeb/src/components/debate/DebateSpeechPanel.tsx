/**
 * 辩论发言历史面板 (2026-08-31 §20260831-01 + §20260831-04)
 *
 * 对齐 docs/辩论比赛/04 §3.4 发言历史面板设计:
 *   - 按阶段分组的发言列表(▼ 立论 / 驳论 / 质询 / ...)
 *   - 每条发言显示 立场 + 辩位 + 模型 + 内容
 *   - §20260831-04:每个阶段折叠块,默认展开当前阶段
 *
 * 同时支持点赞按钮(沿用 DebateStage 相同的 wsClient.send('debate.like'))。
 */
import { useMemo, useState } from 'react';
import { wsClient } from '@/services/ws';
import { useDebateStore } from '@/store/debate.store';
import type { DebatePhase, DebateSpeech } from '@/types/debate';

interface PhaseGroup {
  phase: DebatePhase;
  phaseCn: string;
  speeches: DebateSpeech[];
}

// 与后端 PhaseCN 对齐
const PHASE_CN: Record<string, string> = {
  preparation: '赛前准备',
  opening_argument: '① 开篇立论',
  rebuttal: '② 驳论',
  cross_examination: '③ 质询',
  cross_exam_summary: '③-附 质询小结',
  free_debate: '④ 自由辩论',
  closing_argument: '⑤ 总结陈词',
  judging: '⑥ 评审',
  result: '公布结果',
  game_over: '对局结束',
};

export default function DebateSpeechPanel() {
  const speeches = useDebateStore((s) => s.speeches);
  const crossExam = useDebateStore((s) => s.crossExam);
  const phase = useDebateStore((s) => s.phase);
  const currentRoom = useDebateStore((s) => s.currentRoom);
  const likedSpeeches = useDebateStore((s) => s.likedSpeeches);
  const toggleLike = useDebateStore((s) => s.toggleLike);

  const revealThought = currentRoom?.spectator_config?.reveal_agent_thought ?? false;
  const agentThoughts = useDebateStore((s) => s.agentThoughts);

  // 按阶段分组
  const groups = useMemo<PhaseGroup[]>(() => {
    const map = new Map<string, PhaseGroup>();
    for (const sp of speeches) {
      const key = sp.phase;
      let g = map.get(key);
      if (!g) {
        g = {
          phase: key as DebatePhase,
          phaseCn: PHASE_CN[key] ?? key,
          speeches: [],
        };
        map.set(key, g);
      }
      g.speeches.push(sp);
    }
    // 质询阶段额外注入 cross-exam 问答应
    if (crossExam.length > 0) {
      const phase = 'cross_examination' as DebatePhase;
      let g = map.get(phase);
      if (!g) {
        g = {
          phase,
          phaseCn: PHASE_CN[phase] ?? '③ 质询',
          speeches: [],
        };
        map.set(phase, g);
      }
    }
    // 按定义顺序排序
    const order: DebatePhase[] = [
      'preparation',
      'opening_argument',
      'rebuttal',
      'cross_examination',
      'cross_exam_summary',
      'free_debate',
      'closing_argument',
      'judging',
      'result',
      'game_over',
    ];
    return order
      .filter((p) => map.has(p))
      .map((p) => map.get(p)!);
  }, [speeches, crossExam]);

  // 默认展开当前阶段(以及已有内容的阶段)
  const defaultOpen = useMemo(() => {
    const set = new Set<string>();
    set.add(phase);
    groups.forEach((g) => set.add(g.phase));
    return set;
  }, [phase, groups]);

  if (speeches.length === 0 && crossExam.length === 0) {
    return (
      <div className="debate-speech-panel debate-speech-panel--empty">
        <h3>📜 发言历史</h3>
        <p>比赛开始后这里会显示发言记录。</p>
      </div>
    );
  }

  return (
    <div className="debate-speech-panel">
      <h3>📜 发言历史</h3>
      {groups.map((g) => (
        <PhaseGroupView
          key={g.phase}
          group={g}
          defaultOpen={defaultOpen.has(g.phase)}
          revealThought={revealThought}
          agentThoughts={agentThoughts}
          likedSpeeches={likedSpeeches}
          onLike={(sid) => {
            const added = toggleLike(sid);
            if (!added || !currentRoom) return;
            wsClient.send('debate.like', {
              room_id: currentRoom.room_id,
              speech_id: sid,
            });
          }}
        />
      ))}
      {crossExam.length > 0 && (
        <PhaseGroupView
          group={{
            phase: 'cross_examination' as DebatePhase,
            phaseCn: PHASE_CN['cross_examination'],
            speeches: [],
          }}
          defaultOpen={defaultOpen.has('cross_examination')}
          revealThought={revealThought}
          agentThoughts={agentThoughts}
          likedSpeeches={likedSpeeches}
          onLike={(sid) => {
            const added = toggleLike(sid);
            if (!added || !currentRoom) return;
            wsClient.send('debate.like', {
              room_id: currentRoom.room_id,
              speech_id: sid,
            });
          }}
        />
      )}
    </div>
  );
}

interface PhaseGroupViewProps {
  group: PhaseGroup;
  defaultOpen: boolean;
  revealThought: boolean;
  agentThoughts: Record<string, string>;
  likedSpeeches: Record<string, number>;
  onLike: (speechId: string) => void;
}

function PhaseGroupView({
  group,
  defaultOpen,
  revealThought,
  agentThoughts,
  likedSpeeches,
  onLike,
}: PhaseGroupViewProps) {
  const [open, setOpen] = useState(defaultOpen);
  const crossExam = useDebateStore((s) => s.crossExam);

  // 质询阶段注入问答应
  const isCrossExam = group.phase === 'cross_examination';
  const crossEntries = isCrossExam ? crossExam : [];

  return (
    <details
      className="phase-group"
      open={open}
      onToggle={(e) => setOpen((e.target as HTMLDetailsElement).open)}
    >
      <summary>
        <span className="phase-group-icon">{open ? '▼' : '▶'}</span>{' '}
        {group.phaseCn}
        <span className="phase-group-count">
          ({group.speeches.length + crossEntries.length})
        </span>
      </summary>
      <ul className="phase-group-list">
        {group.speeches.map((sp) => {
          const isLiked = (likedSpeeches[sp.id] ?? 0) > 0;
          const key = `${sp.team_id}:${sp.seat}`;
          const thought = agentThoughts[key] || sp.internal_thought || '';
          return (
            <li key={sp.id} className={`speech-item speech-item--${sp.stance}`}>
              <div className="speech-item-header">
                <span className="speaker">{sp.speaker_name}</span>
                <span className={`stance-tag stance-tag--${sp.stance}`}>
                  {stanceLabel(sp.stance)}
                </span>
              </div>
              <p className="speech-item-content">{sp.content}</p>
              {revealThought && thought && (
                <details className="speech-item-thought">
                  <summary>💭 Agent 思考</summary>
                  <p>{thought}</p>
                </details>
              )}
              <div className="speech-item-actions">
                <button
                  type="button"
                  className={`like-btn like-btn--sm${isLiked ? ' like-btn--active' : ''}`}
                  onClick={() => onLike(sp.id)}
                  aria-pressed={isLiked}
                >
                  👍 {likedSpeeches[sp.id] ?? 0}
                </button>
              </div>
            </li>
          );
        })}
        {crossEntries.map((ce) => (
          <li key={ce.id} className={`speech-item ${ce.is_answer ? 'speech-item--answer' : 'speech-item--question'}`}>
            <div className="speech-item-header">
              <span className="speaker">{ce.questioner} → {ce.answerer}</span>
            </div>
            {ce.is_answer ? (
              <p className="speech-item-content">↪ 答:{ce.answer}</p>
            ) : (
              <p className="speech-item-content">❓ 问:{ce.question}</p>
            )}
          </li>
        ))}
      </ul>
    </details>
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
    case 'angle_1': return '角度一';
    case 'angle_2': return '角度二';
    case 'angle_3': return '角度三';
    case 'angle_4': return '角度四';
    case 'angle_5': return '角度五';
    default: return s;
  }
}
