// §20260812-01 U2 — MindMirror localStorage hook(纯前端,§128 隐私保护)。
//
// 人类直觉存 localStorage,**不**进任何 ws / api / state / Agent prompt。

export interface MindMirrorGuess {
  faction: 'wolf' | 'good' | 'unknown';
  confidence: number; // 0~1
}

const STORAGE_PREFIX = 'werewolf-mindmirror:';

function storageKey(roomId: string): string {
  return `${STORAGE_PREFIX}${roomId}`;
}

export function loadMindMirrorGuess(roomId: string): Record<number, MindMirrorGuess> {
  if (typeof window === 'undefined') return {};
  try {
    const raw = window.localStorage.getItem(storageKey(roomId));
    if (!raw) return {};
    const parsed = JSON.parse(raw);
    if (typeof parsed !== 'object' || parsed === null) return {};
    return parsed as Record<number, MindMirrorGuess>;
  } catch {
    return {};
  }
}

export function saveMindMirrorGuess(roomId: string, seat: number, guess: MindMirrorGuess): void {
  if (typeof window === 'undefined') return;
  try {
    const cur = loadMindMirrorGuess(roomId);
    cur[seat] = guess;
    window.localStorage.setItem(storageKey(roomId), JSON.stringify(cur));
  } catch {
    // best-effort:localStorage 容量满 / 隐私模式关闭时不报错
  }
}

export function clearMindMirrorGuess(roomId: string): void {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.removeItem(storageKey(roomId));
  } catch {
    // best-effort
  }
}
