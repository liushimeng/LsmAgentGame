// §20260812-01 U2 — MindMirror:人类直觉 vs Agent 逻辑对比面板(纯前端)。
//
// 仅在**混合房间**且**人类存活**时渲染。**仅展示概率/置信度 + 阵营倾向**,
// 不显示 Agent 真实身份(§135)。人类直觉存 localStorage(隐私保护,§128),
// 不进任何 ws / api / state / Agent prompt。
import { useEffect, useMemo, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  loadMindMirrorGuess,
  saveMindMirrorGuess,
  type MindMirrorGuess,
} from '@/hooks/useMindMirror';

export interface AgentHypothesisEntry {
  seat: number;
  faction: 'wolf' | 'good' | 'unknown';
  confidence: number; // 0~1
  reasoning?: string;
}

export interface MindMirrorPanelProps {
  roomId: string;
  mySeat: number;
  agentHypothesis: AgentHypothesisEntry[];
  isHumanInRoom: boolean;
  isLocalPlayerAlive: boolean;
}

/** 阵营反向判定:wolf ↔ good。 */
function isOpposite(a: 'wolf' | 'good' | 'unknown', b: 'wolf' | 'good' | 'unknown'): boolean {
  if (a === 'unknown' || b === 'unknown') return false;
  return a !== b;
}

function diffTone(scoreSelf: number, scoreAgent: number): 'match' | 'near' | 'opposite' {
  if (scoreSelf === 0 && scoreAgent === 0) return 'match';
  if (isOpposite(scoreSelf > 0.5 ? 'good' : 'wolf', scoreAgent > 0.5 ? 'good' : 'wolf')) {
    return 'opposite';
  }
  const diff = Math.abs(scoreSelf - scoreAgent);
  if (diff >= 0.3) return 'opposite';
  if (diff >= 0.1) return 'near';
  return 'match';
}

export function MindMirrorPanel({
  roomId,
  mySeat,
  agentHypothesis,
  isHumanInRoom,
  isLocalPlayerAlive,
}: MindMirrorPanelProps) {
  const t = useT();
  const [myGuesses, setMyGuesses] = useState<Record<number, MindMirrorGuess>>({});

  useEffect(() => {
    if (!isHumanInRoom || !isLocalPlayerAlive) return;
    const loaded = loadMindMirrorGuess(roomId);
    setMyGuesses(loaded);
  }, [roomId, isHumanInRoom, isLocalPlayerAlive]);

  const rows = useMemo(() => {
    if (!isHumanInRoom || !isLocalPlayerAlive) return [];
    return agentHypothesis
      .filter((h) => h.seat !== mySeat)
      .map((h) => {
        const myGuess = myGuesses[h.seat];
        const selfScore = myGuess?.confidence ?? 0;
        const selfFaction: 'wolf' | 'good' | 'unknown' = myGuess?.faction ?? 'unknown';
        const agentFaction = h.faction;
        const agentScore = h.confidence;
        const tone = diffTone(selfScore, agentScore);
        return {
          seat: h.seat,
          self: { faction: selfFaction, confidence: selfScore },
          agent: { faction: agentFaction, confidence: agentScore },
          tone,
          reasoning: h.reasoning,
        };
      });
  }, [agentHypothesis, myGuesses, mySeat, isHumanInRoom, isLocalPlayerAlive]);

  if (!isHumanInRoom || !isLocalPlayerAlive) {
    return (
      <div className="mindmirror-panel mindmirror-panel--disabled">
        <p>{t('werewolf.mindmirror.empty' as TKey)}</p>
      </div>
    );
  }

  const onGuess = (seat: number, faction: 'wolf' | 'good', confidence: number) => {
    const next = { ...myGuesses, [seat]: { faction, confidence } };
    setMyGuesses(next);
    saveMindMirrorGuess(roomId, seat, { faction, confidence });
  };

  return (
    <div className="mindmirror-panel" data-testid="mindmirror-panel">
      <h4 className="mindmirror-panel__title">🧠 {t('werewolf.mindmirror.title' as TKey)}</h4>
      <table className="mindmirror-panel__table">
        <thead>
          <tr>
            <th>玩家</th>
            <th>{t('werewolf.mindmirror.colYou' as TKey)}</th>
            <th>{t('werewolf.mindmirror.colAgent' as TKey)}</th>
            <th>{t('werewolf.mindmirror.colDiff' as TKey)}</th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 && (
            <tr>
              <td colSpan={4} className="mindmirror-panel__empty">
                {t('werewolf.mindmirror.empty' as TKey)}
              </td>
            </tr>
          )}
          {rows.map((r) => (
            <tr key={r.seat} data-testid={`mindmirror-row-${r.seat}`}>
              <td>#{r.seat + 1}</td>
              <td>
                <select
                  value={r.self.faction}
                  onChange={(e) =>
                    onGuess(r.seat, e.target.value as 'wolf' | 'good', r.self.confidence || 0.5)
                  }
                  aria-label={`my-guess-${r.seat}`}
                >
                  <option value="wolf">🐺 狼</option>
                  <option value="good">👤 好人</option>
                  <option value="unknown">?</option>
                </select>
                <input
                  type="range"
                  min={0}
                  max={1}
                  step={0.05}
                  value={r.self.confidence}
                  onChange={(e) =>
                    onGuess(r.seat, r.self.faction === 'unknown' ? 'good' : r.self.faction, Number(e.target.value))
                  }
                  aria-label={`my-confidence-${r.seat}`}
                />
                <span className="mindmirror-panel__pct">{(r.self.confidence * 100).toFixed(0)}%</span>
              </td>
              <td>
                {r.agent.faction === 'wolf' ? '🐺' : r.agent.faction === 'good' ? '👤' : '?'}
                {' '}
                {(r.agent.confidence * 100).toFixed(0)}%
              </td>
              <td>
                <span className={`mindmirror-panel__tone mindmirror-panel__tone--${r.tone}`}>
                  {r.tone === 'opposite' && t('werewolf.mindmirror.diffOpposite' as TKey)}
                  {r.tone === 'near' && t('werewolf.mindmirror.diffNear' as TKey)}
                  {r.tone === 'match' && t('werewolf.mindmirror.diffMatch' as TKey)}
                </span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
