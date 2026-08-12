/**
 * useIdentityGuess — 狼人杀玩家身份猜测的本地持久化存储。
 *
 * 2026-07-22 §任务2 — 未知身份卡用户标注:
 *   - 玩家在活着时可以给任何座位打"我猜测他是 X 角色"的标签,
 *     死亡/复盘阶段拿来与"服务端揭示的真实身份"对比。
 *   - 持久化到 localStorage(以 roomId + 当前 account 为 scope),
 *     刷新页面 / 重连 WS 后仍然保留。
 *   - **不入服务端状态**——纯本地辅助工具,不会广播给其他玩家,
 *     也不会影响游戏逻辑。
 *
 * 教学:
 *   - localStorage 必须 try/catch 兜底(隐私模式 / quota 超限)。
 *   - 跨 tab 用 storage 事件同步(用户开两个窗口同房间时同步猜测)。
 *   - guess 集合需限制上限(13 座位够用,但仍 clamp 防止脏数据)。
 */

import { useCallback, useEffect, useState } from 'react';
import type { WerewolfRole } from '@/types/werewolf';

/** 单一猜测:{ guessedBy, guesses: { seat -> role|null } }。
 *  guesses 的 value 为 null 表示"取消猜测"(显式删除,不是缺省)。
 *  缺省 key 视为未猜测。 */
export interface IdentityGuessMap {
  guessedBy: string;
  updatedAt: number;
  guesses: Record<number, WerewolfRole | null>;
}

const STORAGE_PREFIX = 'werewolf.guess.v1:';
/** 角色猜测枚举(留出 'unknown' 占位,表示"我猜不出"——与 role enum 对齐)。 */
const GUESSABLE_ROLES: WerewolfRole[] = [
  'werewolf',
  'seer',
  'witch',
  'hunter',
  'idiot',
  'villager',
  'guard',
  // §198 骑士重新实现 — 加回猜测池。
  'knight',
  // §猎魔人 猎魔人重新实现 — 加回猜测池。
  'demon_hunter',
  // ⚠️ 2026-07-29 已退役:无引擎/工具/美术实现,前端隐藏。
  // 'magician', 'merchant', 'dreamer', 'crow', 'pure_white',
  'unknown',
];

function storageKey(roomId: string, account: string): string {
  return `${STORAGE_PREFIX}${roomId}:${account}`;
}

function readGuess(roomId: string, account: string): IdentityGuessMap {
  const empty: IdentityGuessMap = { guessedBy: account, updatedAt: 0, guesses: {} };
  if (typeof window === 'undefined' || !window.localStorage) return empty;
  try {
    const raw = window.localStorage.getItem(storageKey(roomId, account));
    if (!raw) return empty;
    const parsed = JSON.parse(raw);
    if (
      !parsed ||
      typeof parsed !== 'object' ||
      parsed.guessedBy !== account ||
      typeof parsed.guesses !== 'object' ||
      parsed.guesses === null
    ) {
      return empty;
    }
    // 防御:guesses key 必须是数字,value 必须是 enum 内或 null
    const sanitized: Record<number, WerewolfRole | null> = {};
    for (const [k, v] of Object.entries(parsed.guesses as Record<string, unknown>)) {
      const seat = Number(k);
      if (!Number.isInteger(seat) || seat < 0 || seat > 12) continue;
      if (v === null) {
        sanitized[seat] = null;
      } else if (typeof v === 'string' && (GUESSABLE_ROLES as string[]).includes(v)) {
        sanitized[seat] = v as WerewolfRole;
      }
    }
    return {
      guessedBy: account,
      updatedAt: Number(parsed.updatedAt) || 0,
      guesses: sanitized,
    };
  } catch {
    return empty;
  }
}

function writeGuess(roomId: string, map: IdentityGuessMap): void {
  if (typeof window === 'undefined' || !window.localStorage) return;
  try {
    window.localStorage.setItem(storageKey(roomId, map.guessedBy), JSON.stringify(map));
  } catch {
    /* quota exceeded / private mode — silently degrade, guess 仍在本会话可用 */
  }
}

export interface UseIdentityGuess {
  guesses: Record<number, WerewolfRole | null>;
  updatedAt: number;
  setGuess: (seat: number, role: WerewolfRole | null) => void;
  clearGuess: (seat: number) => void;
  clearAll: () => void;
  guessableRoles: WerewolfRole[];
}

/**
 * 当前用户在指定房间的身份猜测 store。
 * - 多个组件共享同一 roomId+account 时,会通过 storage 事件同步。
 * - 切换 account / roomId 时自动重新加载。
 */
export function useIdentityGuess(roomId: string, account: string): UseIdentityGuess {
  const [map, setMap] = useState<IdentityGuessMap>(() => readGuess(roomId, account));

  // 切换 roomId/account 时重新加载
  useEffect(() => {
    setMap(readGuess(roomId, account));
  }, [roomId, account]);

  // 跨 tab 同步
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const onStorage = (e: StorageEvent) => {
      if (e.key !== storageKey(roomId, account)) return;
      setMap(readGuess(roomId, account));
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, [roomId, account]);

  const setGuess = useCallback(
    (seat: number, role: WerewolfRole | null) => {
      if (!Number.isInteger(seat) || seat < 0) return;
      setMap((prev) => {
        const next: IdentityGuessMap = {
          guessedBy: account,
          updatedAt: Date.now(),
          guesses: { ...prev.guesses },
        };
        if (role === null) {
          delete next.guesses[seat];
        } else {
          next.guesses[seat] = role;
        }
        writeGuess(roomId, next);
        return next;
      });
    },
    [account, roomId],
  );

  const clearGuess = useCallback(
    (seat: number) => {
      setGuess(seat, null);
    },
    [setGuess],
  );

  const clearAll = useCallback(() => {
    setMap(() => {
      const next: IdentityGuessMap = {
        guessedBy: account,
        updatedAt: Date.now(),
        guesses: {},
      };
      writeGuess(roomId, next);
      return next;
    });
  }, [account, roomId]);

  return {
    guesses: map.guesses,
    updatedAt: map.updatedAt,
    setGuess,
    clearGuess,
    clearAll,
    guessableRoles: GUESSABLE_ROLES,
  };
}
