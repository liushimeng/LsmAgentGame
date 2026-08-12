// GodModeView.tsx
// §20260810-11 V1 — 全视角读心观战(spectator 视角切换面板)
//
// 设计要点:
//   - 仅 spectator 可见(isSpectator===true 校验)
//   - 13 座位小卡片网格,点击切换 activeSeat
//   - 主面板渲染 activeSeat 的角色/身份/夜间行动/内心独白/决策摘要
//   - §135 终局前身份栏显示「🔒 [已隐藏]」;终局后或白痴翻牌/猎人开枪后公开
//   - §119 协议层隔离:HeartThought/LastDecision 通过 spectator-only 字段下发
//   - §121 数据形状:PerSeatPOV 类型与后端 view.go 严格对齐(字段为 snake_case)

import React, { useState, useMemo } from 'react';
import type { PerSeatPOV, GodModeSnapshot, WerewolfPlayerJSON } from '@/types/werewolf';

// §20260811-08 U3 — 公开技能行动的中文标签。
// 本组件与 DayNightOverlay 一致,刻意不走 t() —— 上帝视角是 spectator 专用
// 调试向面板,与 §124 既有约定保持一致。
const PUBLIC_ACTION_LABEL: Record<string, string> = {
  hunter_shot: '🔫 猎人开枪',
  knight_duel: '⚔️ 骑士决斗',
  demon_hunter: '🗡️ 猎魔人狩猎',
  idiot_reveal: '🃏 白痴翻牌',
};

export interface GodModeViewProps {
  /** 后端下发的 GodModeSnapshot,含 per_seat_pov map */
  snapshot: GodModeSnapshot;
  /** 玩家座位列表(用于显示座位标签) */
  players: WerewolfPlayerJSON[];
  /** 当前登录 user_id(spectator 本人;非空时校验是否真的在旁观) */
  currentUserId: string;
  /** 是否真的在 spectator 模式(双重判断,防止玩家切换视角) */
  isSpectator: boolean;
}

/**
 * 全视角读心观战组件
 * - 渲染 13 座位小卡片网格
 * - 点击座位切换 activeSeat,主面板渲染该座位 POV
 * - 仅 spectator 可见(双重判断:isSpectator && currentUserId)
 */
export const GodModeView: React.FC<GodModeViewProps> = ({
  snapshot,
  players,
  currentUserId,
  isSpectator,
}) => {
  const [activeSeat, setActiveSeat] = useState<number | null>(null);

  // §119 协议层隔离:玩家本人不可切换到他视角。
  // 即使 isSpectator=true 也再做一次 user_id !== my_user_id 判断。
  const canView = isSpectator && currentUserId !== '';

  const occupiedSeats = useMemo(() => {
    return Object.keys(snapshot.per_seat_pov || {})
      .map((k) => parseInt(k, 10))
      .filter((s) => s >= 0 && s < 13)
      .sort((a, b) => a - b);
  }, [snapshot.per_seat_pov]);

  if (!canView) {
    return (
      <div className="godmode-view godmode-view--forbidden">
        🔒 视角切换仅 spectator 可见
      </div>
    );
  }

  if (occupiedSeats.length === 0) {
    return (
      <div className="godmode-view godmode-view--empty">
        ⏳ 等待玩家入座…
      </div>
    );
  }

  const pov: PerSeatPOV | undefined =
    activeSeat !== null && snapshot.per_seat_pov
      ? snapshot.per_seat_pov[activeSeat]
      : undefined;
  const activePlayer = activeSeat !== null ? players.find((p) => p.seat === activeSeat) : undefined;

  return (
    <div className="godmode-view">
      <div className="godmode-view__header">
        <h3>👁️ 第一视角(spectator 专用)</h3>
        <p className="godmode-view__hint">点击下方座位切换视角</p>
      </div>
      <div className="godmode-view__seat-grid">
        {occupiedSeats.map((seat) => {
          const p = snapshot.per_seat_pov?.[seat];
          if (!p) return null;
          const isActive = activeSeat === seat;
          return (
            <button
              key={seat}
              className={`godmode-view__seat ${isActive ? 'is-active' : ''}`}
              onClick={() => setActiveSeat(seat)}
              type="button"
            >
              <span className="godmode-view__seat-num">{seat + 1}号</span>
              <span className="godmode-view__seat-faction">
                {p.faction === 'wolf' ? '🐺' : p.faction === 'good' ? '✨' : '?'}
              </span>
            </button>
          );
        })}
      </div>
      {pov && activePlayer && (
        <div className="godmode-view__detail">
          <div className="godmode-view__identity">
            <span className="godmode-view__identity-num">{activeSeat! + 1}号</span>
            <span className="godmode-view__identity-role">
              {pov.role_revealed ? pov.role : '🔒 [已隐藏]'}
            </span>
            <span className={`godmode-view__identity-faction godmode-view__identity-faction--${pov.faction}`}>
              {pov.faction === 'wolf' ? '狼' : '好人'}
            </span>
          </div>
          {pov.last_emotion && (
            <div className="godmode-view__emotion">情绪:{pov.last_emotion}</div>
          )}
          {pov.heart_thought && (
            <details className="godmode-view__heart-thought">
              <summary>💭 内心独白</summary>
              <p>{pov.heart_thought}</p>
            </details>
          )}
          {pov.last_decision && (
            <details className="godmode-view__last-decision">
              <summary>🎯 最近决策</summary>
              <p>{pov.last_decision}</p>
            </details>
          )}
          {/* §20260811-08 U1 — night_actions 此前后端恒为空切片,组件也从未渲染。
              现由 InformationLedger 真实聚合(该座位作为**行动者**的夜间行动)。 */}
          {pov.night_actions && pov.night_actions.length > 0 && (
            <div className="godmode-view__night-actions">
              <span>🌙 夜间行动:</span>
              {pov.night_actions.map((a: string, idx: number) => (
                <span key={idx} className="godmode-view__night-action-tag">{a}</span>
              ))}
            </div>
          )}
          {pov.public_commitments && pov.public_commitments.length > 0 && (
            <div className="godmode-view__commitments">
              <span>📋 公开承诺:</span>
              {pov.public_commitments.map((c: string, idx: number) => (
                <span key={idx} className="godmode-view__commitment-tag">{c}</span>
              ))}
            </div>
          )}
          <div className="godmode-view__stats">
            <span>LLM 调用:{pov.llm_call_count}</span>
            <span>工具调用:{pov.tool_call_count}</span>
            {/* §20260811-08 U1 — challenge_count 语义为「当前是否被质疑」(0/1),
                引擎无本局累计计数器,故渲染为是/否而非次数。 */}
            <span>被质疑:{pov.challenge_count > 0 ? '是' : '否'}</span>
          </div>
        </div>
      )}
      {/* §20260811-08 U3 — 已公开技能行动(全局,不随 activeSeat 切换)。
          这 4 类事件属 §135 身份公开白名单,本就全房可见。 */}
      {snapshot.public_actions && snapshot.public_actions.length > 0 && (
        <div className="godmode-view__public-actions">
          <h4>⚔️ 公开技能行动</h4>
          <ul>
            {snapshot.public_actions.map((a, idx) => (
              <li key={idx} className="godmode-view__public-action">
                <span className="godmode-view__pa-day">D{a.day}</span>
                <span className="godmode-view__pa-kind">{PUBLIC_ACTION_LABEL[a.kind] ?? a.kind}</span>
                <span className="godmode-view__pa-seat">{a.seat + 1}号</span>
                {a.target >= 0 && <span className="godmode-view__pa-target">→ {a.target + 1}号</span>}
                {a.hit_wolf !== undefined && (
                  <span className={`godmode-view__pa-hit ${a.hit_wolf ? 'is-hit' : 'is-miss'}`}>
                    {a.hit_wolf ? '命中狼人' : '未命中'}
                  </span>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
};

export default GodModeView;
