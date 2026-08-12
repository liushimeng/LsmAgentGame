/**
 * GuessAccuracyCard — 狼人杀结算页「我的推理准确率」展示卡
 * (§20260809-02 U3 人类身份猜测准确率)
 *
 * 设计动机:
 *   useIdentityGuess 让玩家在活着时给每个座位打"我猜测身份"标签(纯 localStorage,
 *   不上行),终局后一直没有反馈——猜对了不知道,猜错了也不知道。本组件:
 *     1. 终局时从 localStorage 读玩家本局所有猜测
 *     2. 与 gameState.players[].role 真实身份比对(§135 终局已合法公开)
 *     3. 渲染准确率 + 逐座位对错列表
 *
 * 角色归一化(避免同阵营差异):
 *   - villager / idiot / guard / knight / demon_hunter → "good" (好人阵营)
 *   - werewolf → "wolf" (狼人阵营)
 *   - seer / witch / hunter → "good" (神职 = 好人阵营)
 *   - unknown / null → 跳过(不算入分母)
 *
 * 隐私:
 *   - 纯本地计算,不上行;与 useIdentityGuess.ts 同模式。
 *   - localStorage 缺失时显示「本局未做推理标注」(不是 bug)。
 *
 * §119/§135 合规:
 *   - 不引入新的下发通道,只读 gameState.players[].role(终局合法公开)。
 *   - 不显示其他玩家的猜测,仅显示当前用户的。
 */

import { useMemo } from 'react';
import { useT } from '@/hooks/useT';
import { useIdentityGuess } from '@/hooks/useIdentityGuess';
import type { WerewolfRole } from '@/types/werewolf';

interface GuessAccuracyCardProps {
  roomId: string;
  /** 当前用户的 account(与 useIdentityGuess 同 key 拼装规则一致) */
  account: string;
  /** 当前局所有座位的真实角色;key = seat idx (0-indexed) */
  actualRoles: Record<number, string>;
}

const STORAGE_PREFIX = 'werewolf.guess.v1:';
const STORAGE_KEY_OF = (roomId: string, account: string) =>
  `${STORAGE_PREFIX}${roomId}:${account}`;

/** 把角色归一化到三大类(好人/狼人/未知),用于猜测 vs 真实对比。 */
function normalizeRole(role: string | null | undefined): 'good' | 'wolf' | 'unknown' {
  if (!role) return 'unknown';
  const r = role.toLowerCase();
  // 狼人阵营
  if (r === 'werewolf' || r === 'wolf') return 'wolf';
  // 好人阵营(包括所有神职 + 平民 + 翻牌白痴)
  if (
    r === 'villager' ||
    r === 'seer' ||
    r === 'witch' ||
    r === 'hunter' ||
    r === 'idiot' ||
    r === 'guard' ||
    r === 'knight' ||
    r === 'demon_hunter'
  ) {
    return 'good';
  }
  return 'unknown';
}

interface ComparisonRow {
  seat: number;
  /** 玩家猜测(可能 null = 未猜) */
  guessedRole: WerewolfRole | null;
  /** 真实角色(原始字符串,可能 unknown) */
  actualRole: string;
  /** 归一化后是否一致 */
  match: boolean;
  /** 是否跳过(null 猜测 / unknown 真实) */
  skipped: boolean;
}

function readGuessesFromStorage(
  roomId: string,
  account: string,
): Record<number, WerewolfRole | null> {
  if (typeof window === 'undefined' || !window.localStorage) return {};
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY_OF(roomId, account));
    if (!raw) return {};
    const parsed = JSON.parse(raw);
    if (
      !parsed ||
      typeof parsed !== 'object' ||
      typeof parsed.guesses !== 'object' ||
      parsed.guesses === null
    ) {
      return {};
    }
    const out: Record<number, WerewolfRole | null> = {};
    for (const [k, v] of Object.entries(parsed.guesses as Record<string, unknown>)) {
      const seat = Number(k);
      if (!Number.isInteger(seat) || seat < 0 || seat > 12) continue;
      if (v === null) {
        out[seat] = null;
      } else if (typeof v === 'string') {
        out[seat] = v as WerewolfRole;
      }
    }
    return out;
  } catch {
    return {};
  }
}

export function GuessAccuracyCard({
  roomId,
  account,
  actualRoles,
}: GuessAccuracyCardProps) {
  const t = useT();
  // 双保险:useIdentityGuess 已读 localStorage,但这里再次直接读避免 hook
  // 在结算页可能没挂载的场景下拿到空数据。
  const guesses = useIdentityGuess(roomId, account).guesses;
  const storageGuesses = useMemo(
    () => readGuessesFromStorage(roomId, account),
    [roomId, account],
  );
  // 优先用 hook 数据(响应式),缺失时回退到 storage 直读。
  const finalGuesses: Record<number, WerewolfRole | null> = useMemo(() => {
    const merged: Record<number, WerewolfRole | null> = { ...storageGuesses };
    for (const [k, v] of Object.entries(guesses)) {
      merged[Number(k)] = v;
    }
    return merged;
  }, [guesses, storageGuesses]);

  const rows = useMemo<ComparisonRow[]>(() => {
    const seats = Array.from(
      new Set([
        ...Object.keys(finalGuesses).map((k) => Number(k)),
        ...Object.keys(actualRoles).map((k) => Number(k)),
      ]),
    ).sort((a, b) => a - b);
    return seats.map((seat) => {
      const guessed = finalGuesses[seat] ?? null;
      const actual = actualRoles[seat];
      const guessedNorm = normalizeRole(guessed);
      const actualNorm = normalizeRole(actual);
      const skipped = !guessed || actualNorm === 'unknown';
      const match = !skipped && guessedNorm === actualNorm;
      return {
        seat,
        guessedRole: guessed,
        actualRole: actual || '?',
        match,
        skipped,
      };
    });
  }, [finalGuesses, actualRoles]);

  const total = rows.filter((r) => !r.skipped).length;
  const correct = rows.filter((r) => r.match).length;
  const accuracy = total > 0 ? Math.round((correct / total) * 100) : 0;

  // localStorage 完全无数据 → 显示「本局未做推理标注」
  const hasAnyGuess = Object.values(finalGuesses).some((v) => v != null);
  if (!hasAnyGuess) {
    return (
      <div
        className="werewolf-guess-accuracy"
        data-testid="ww-guess-accuracy-empty"
      >
        <p className="werewolf-guess-accuracy__empty">
          {t('werewolf.settlement.guessAccuracy.empty' as any)}
        </p>
      </div>
    );
  }

  return (
    <div className="werewolf-guess-accuracy" data-testid="ww-guess-accuracy">
      <h4 className="werewolf-guess-accuracy__title">
        {t('werewolf.settlement.guessAccuracy.title' as any)}
      </h4>
      <div className="werewolf-guess-accuracy__summary">
        <span className="werewolf-guess-accuracy__big">
          {correct}/{total}
        </span>
        <span className="werewolf-guess-accuracy__percent">{accuracy}%</span>
      </div>
      <div className="werewolf-guess-accuracy__bar" aria-hidden>
        <div
          className="werewolf-guess-accuracy__bar-fill"
          style={{ width: `${accuracy}%` }}
        />
      </div>
      <details className="werewolf-guess-accuracy__details">
        <summary>{t('werewolf.settlement.guessAccuracy.detail' as any)}</summary>
        <ul className="werewolf-guess-accuracy__list">
          {rows.map((r) => (
            <li
              key={r.seat}
              className={`werewolf-guess-accuracy__row ${r.match ? 'is-match' : r.skipped ? 'is-skipped' : 'is-mismatch'}`}
              data-testid={`ww-guess-row-${r.seat}`}
            >
              <span className="werewolf-guess-accuracy__seat">
                {r.seat + 1}号
              </span>
              <span className="werewolf-guess-accuracy__guess">
                {r.guessedRole
                  ? t(`werewolf.role.${r.guessedRole}` as any)
                  : '—'}
              </span>
              <span className="werewolf-guess-accuracy__arrow">
                {r.match ? '✓' : r.skipped ? '·' : '✗'}
              </span>
              <span className="werewolf-guess-accuracy__actual">
                {t(`werewolf.role.${r.actualRole}` as any)}
              </span>
            </li>
          ))}
        </ul>
      </details>
    </div>
  );
}
