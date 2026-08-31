/**
 * 辩论比赛 — 复盘详情弹窗 (2026-08-31 §20260831-08)
 *
 * 对齐后端 GET /api/games/debate/history/:id 契约(data: { room, speeches, scores }):
 *   - 队伍与辩位:team_config[] 每队一张卡(立场 + 辩位 + 模型;后端 []TeamConfig 原样透传,辩位 seat_id、优先 role_name)
 *   - 发言记录:按阶段分组(阶段中文标签复用 DebateSpeechPanel.PHASE_CN)
 *   - 裁判评分:每队 5 维均分雷达(复用 DebateRadarChart)+ 分数条(复用 .score-bar),
 *     裁判点评折叠区(样式对齐 DebateScorePanel.judge-comments)
 *
 * 复用优先(CLAUDE.md 任务约束「不要重复造轮子」):
 *   DebateRadarChart / PHASE_CN / stanceLabel / DIM_LABELS / STANCE_ICONS /
 *   .modal-* / .team-card* / .agent-row / .team-score* / .winner-banner /
 *   .best-debater / .judge-comments / .speech-item;新增类在 styles/debate-history.css。
 *
 * §13 SubAgent = frontend-dev:仅修改 ClientWeb/。
 */
import { useEffect, useMemo, useState } from 'react';
import { debateService } from '@/api/debate';
import { isSessionExpiredError } from '@/services/http';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type {
  DebateHistoryDetail,
  DebateHistoryRoom,
  DebateHistoryScore,
  DebateHistorySpeech,
} from '@/types/debate';
import DebateRadarChart, { TEAM_RADAR_COLORS } from './DebateRadarChart';
import { PHASE_CN, stanceLabel } from './DebateSpeechPanel';
import { DIM_LABELS } from './DebateScorePanel';
import { STANCE_ICONS } from './DebateTeamPanel';
import {
  HISTORY_DIM_KEYS,
  HISTORY_PHASE_ORDER,
  bestDebaterName,
  modeLabel,
  roleLabel,
  shortModelKey,
  teamLabel,
} from './historyUtils';

interface Props {
  roomId: string;
  onClose: () => void;
}

export default function DebateReplayModal({ roomId, onClose }: Props) {
  const t = useT();
  const [detail, setDetail] = useState<DebateHistoryDetail | null>(null);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setErr('');
    setDetail(null);
    debateService
      .historyDetail(roomId)
      .then((d) => {
        if (!cancelled) setDetail(d);
      })
      .catch((e: Error) => {
        if (cancelled || isSessionExpiredError(e)) return;
        setErr(e.message);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [roomId]);

  const room: DebateHistoryRoom | null = detail?.room ?? null;
  const speeches = detail?.speeches ?? [];
  const scores = detail?.scores ?? [];

  const bestName = useMemo(
    () => (room ? bestDebaterName(room, speeches, t) : ''),
    // t 随语言变化需重算
    [room, speeches, t],
  );

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-card debate-replay-modal" onClick={(e) => e.stopPropagation()}>
        <header className="modal-header">
          <h2>📜 {t('debate.replay.title' as TKey)}</h2>
          <button className="modal-close" onClick={onClose}>×</button>
        </header>

        <div className="modal-body debate-replay-body">
          {loading && (
            <p className="model-stats-empty">{t('debate.replay.loading' as TKey)}</p>
          )}

          {!loading && err && (
            <div className="form-error" role="alert">
              {t('debate.replay.error' as TKey, { msg: err })}
            </div>
          )}

          {room && (
            <>
              {/* 辩题 + 模式 + 异常标记 */}
              <div className="replay-topic-line">
                <span className="replay-topic-text">{room.topic_text}</span>
                <span className="mode-tag">{modeLabel(room.mode, t)}</span>
                {room.is_abnormal && (
                  <span className="history-abnormal-badge">
                    {t('debate.history.abnormal' as TKey)}
                  </span>
                )}
              </div>

              {/* 胜方 + 最佳辩手横幅(复用 ScorePanel 同款样式) */}
              <div className="winner-banner">
                {room.winner_team_id
                  ? t('debate.replay.winnerIs' as TKey, { team: teamLabel(room, room.winner_team_id) })
                  : t('debate.replay.noWinner' as TKey)}
              </div>
              <div className="best-debater">
                {t('debate.replay.bestIs' as TKey, {
                  name: bestName,
                  team: teamLabel(room, room.best_debater_team_id) || '—',
                  seat: room.best_debater_seat,
                })}
              </div>

              <ReplayTeamsSection room={room} />
              <ReplayScoresSection room={room} scores={scores} />
              <ReplaySpeechesSection speeches={speeches} />
            </>
          )}
        </div>

        <footer className="modal-footer">
          <button className="btn-secondary" onClick={onClose}>
            {t('debate.replay.close' as TKey)}
          </button>
        </footer>
      </div>
    </div>
  );
}

/* ---------------- 队伍与辩位 ---------------- */

function ReplayTeamsSection({ room }: { room: DebateHistoryRoom }) {
  const t = useT();
  const teams = room.team_config ?? [];
  if (teams.length === 0) return null;
  return (
    <section>
      <h4 className="replay-section-title">👥 {t('debate.replay.teams' as TKey)}</h4>
      <div className="replay-teams-grid">
        {teams.map((team) => (
          <div key={team.team_id} className={`team-card team-card--${team.stance}`}>
            <header className="team-card__header">
              {STANCE_ICONS[team.stance] ?? '⚪'} {team.stance_label || stanceLabel(team.stance)}
            </header>
            <ul className="team-card__agents">
              {team.agents.map((ag) => (
                <li key={ag.seat_id} className="agent-row">
                  <span className="agent-name">
                    {ag.role_name || roleLabel(ag.role, t)} · {t('debate.history.seat' as TKey, { n: ag.seat_id })}
                  </span>
                  {ag.model_key && (
                    <span className="replay-agent-model">{shortModelKey(ag.model_key)}</span>
                  )}
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </section>
  );
}

/* ---------------- 裁判评分 ---------------- */

/** 按维度求均分(total 为各行 total_score 均值)。 */
function avgScores(rows: DebateHistoryScore[]): { dims: Record<string, number>; total: number } {
  const dims: Record<string, number> = {};
  for (const key of HISTORY_DIM_KEYS) {
    const sum = rows.reduce((acc, r) => acc + (Number(r[key]) || 0), 0);
    dims[key] = rows.length > 0 ? sum / rows.length : 0;
  }
  const totalSum = rows.reduce((acc, r) => acc + (Number(r.total_score) || 0), 0);
  return { dims, total: rows.length > 0 ? totalSum / rows.length : 0 };
}

function ReplayScoresSection({ room, scores }: { room: DebateHistoryRoom; scores: DebateHistoryScore[] }) {
  const t = useT();

  // 出场顺序以 team_config 为准;缺 team_config 时回退评分行中的 team_id 去重。
  const teamIds: number[] = room.team_config?.map((x) => x.team_id)
    ?? Array.from(new Set(scores.map((s) => s.team_id)));

  // 裁判分组(同一裁判对每队各一行;overall_comment 取首行)
  const judgeIds = Array.from(new Set(scores.map((s) => s.judge_id))).sort((a, b) => a - b);

  if (scores.length === 0) {
    return (
      <section>
        <h4 className="replay-section-title">🏆 {t('debate.replay.scores' as TKey)}</h4>
        <p className="model-stats-empty">{t('debate.replay.noScores' as TKey)}</p>
      </section>
    );
  }

  return (
    <section>
      <h4 className="replay-section-title">🏆 {t('debate.replay.scores' as TKey)}</h4>

      {teamIds.map((teamId, idx) => {
        const rows = scores.filter((s) => s.team_id === teamId);
        if (rows.length === 0) return null;
        const { dims, total } = avgScores(rows);
        const isWinner = room.winner_team_id === teamId;
        const radarColor = TEAM_RADAR_COLORS[idx % TEAM_RADAR_COLORS.length];
        return (
          <div key={teamId} className={`team-score${isWinner ? ' team-score--winner' : ''}`}>
            <header className="team-score__header">
              <span>
                {isWinner ? '🥇' : '🥈'} {teamLabel(room, teamId)}
                <span className="replay-avg-hint">
                  {t('debate.replay.avgOf' as TKey, { n: rows.length })}
                </span>
              </span>
              <span className="total">{total.toFixed(1)}</span>
            </header>
            <div className="team-score__body">
              <DebateRadarChart dimensionScores={dims} color={radarColor} size={128} />
              <div className="team-score__bars">
                {HISTORY_DIM_KEYS.map((key) => (
                  <div key={key} className="score-bar">
                    <span className="score-bar__label">{DIM_LABELS[key] ?? key}</span>
                    <div className="score-bar__track">
                      <div
                        className="score-bar__fill"
                        style={{ width: `${(dims[key] / 10) * 100}%` }}
                      />
                    </div>
                    <span className="score-bar__value">{dims[key].toFixed(1)}</span>
                  </div>
                ))}
              </div>
            </div>
          </div>
        );
      })}

      {/* 裁判点评(样式对齐 DebateScorePanel) */}
      {judgeIds.length > 0 && (
        <details className="judge-comments">
          <summary>📝 {t('debate.replay.judgeComments' as TKey)}</summary>
          <ul>
            {judgeIds.map((judgeId) => {
              const rows = scores.filter((s) => s.judge_id === judgeId);
              const head = rows[0];
              return (
                <li key={judgeId}>
                  <strong>
                    {t('debate.replay.judge' as TKey, { id: judgeId + 1 })}
                    {head?.judge_model_key ? `(${shortModelKey(head.judge_model_key)})` : ''}
                    {rows.some((r) => r.is_fallback) && (
                      <span className="replay-fallback-badge">
                        {t('debate.replay.fallback' as TKey)}
                      </span>
                    )}
                  </strong>
                  {head?.overall_comment && <p>{head.overall_comment}</p>}
                  {rows.map((r) => (
                    <div key={r.id} className="replay-judge-team">
                      <span className="replay-judge-score">
                        {teamLabel(room, r.team_id)} · {Number(r.total_score ?? 0).toFixed(1)}
                      </span>
                      {r.comment && (
                        <span className="replay-judge-comment">{r.comment}</span>
                      )}
                    </div>
                  ))}
                </li>
              );
            })}
          </ul>
        </details>
      )}
    </section>
  );
}

/* ---------------- 发言记录(按阶段分组) ---------------- */

interface SpeechGroup {
  phase: string;
  speeches: DebateHistorySpeech[];
}

function groupSpeeches(speeches: DebateHistorySpeech[]): SpeechGroup[] {
  const map = new Map<string, DebateHistorySpeech[]>();
  for (const sp of speeches) {
    const list = map.get(sp.phase);
    if (list) list.push(sp);
    else map.set(sp.phase, [sp]);
  }
  const known = HISTORY_PHASE_ORDER.filter((p) => map.has(p)) as string[];
  const unknown = Array.from(map.keys()).filter((p) => !known.includes(p));
  return [...known, ...unknown].map((phase) => ({ phase, speeches: map.get(phase)! }));
}

function ReplaySpeechesSection({ speeches }: { speeches: DebateHistorySpeech[] }) {
  const t = useT();
  const groups = useMemo(() => groupSpeeches(speeches), [speeches]);

  return (
    <section>
      <h4 className="replay-section-title">📜 {t('debate.replay.speeches' as TKey)}</h4>
      {groups.length === 0 ? (
        <p className="model-stats-empty">{t('debate.replay.noSpeeches' as TKey)}</p>
      ) : (
        groups.map((g) => <ReplayPhaseGroup key={g.phase} group={g} />)
      )}
    </section>
  );
}

function ReplayPhaseGroup({ group }: { group: SpeechGroup }) {
  const t = useT();
  const [open, setOpen] = useState(true);

  return (
    <details
      className="replay-phase-group"
      open={open}
      onToggle={(e) => setOpen((e.target as HTMLDetailsElement).open)}
    >
      <summary>
        <span className="phase-group-icon">{open ? '▼' : '▶'}</span>
        <span className="replay-phase-label">{PHASE_CN[group.phase] ?? group.phase}</span>
        <span className="phase-group-count">({group.speeches.length})</span>
      </summary>
      <ul className="replay-speech-list">
        {group.speeches.map((sp) => (
          <li key={sp.id} className={`speech-item ${sp.stance}`}>
            <div className="replay-speech-meta">
              <span className="speaker">{sp.speaker_name}</span>
              <span className={`stance-tag stance-tag--${sp.stance}`}>{stanceLabel(sp.stance)}</span>
              <span>{roleLabel(sp.role, t)}</span>
              {sp.model_key && <span className="replay-speech-model">{shortModelKey(sp.model_key)}</span>}
              <span>{t('debate.replay.words' as TKey, { n: sp.word_count })}</span>
            </div>
            <p className="speech-item__content">{sp.content}</p>
          </li>
        ))}
      </ul>
    </details>
  );
}
