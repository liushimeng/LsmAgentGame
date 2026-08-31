/**
 * 辩论对局页 — 侧栏折叠状态管理 hook (2026-08-31 §20260831-03)
 *
 * 管理左/右侧栏的折叠状态，支持 localStorage 持久化。
 */
import { useState, useCallback } from 'react';

const LS_KEY_LEFT = 'debate.leftbar.collapsed';
const LS_KEY_RIGHT = 'debate.rightbar.collapsed';

function readStorage(key: string): boolean {
  try {
    if (typeof window === 'undefined') return false;
    return localStorage.getItem(key) === '1';
  } catch {
    return false;
  }
}

function writeStorage(key: string, value: boolean): void {
  try {
    if (typeof window === 'undefined') return;
    localStorage.setItem(key, value ? '1' : '0');
  } catch { /* incognito */ }
}

export function useSidebarCollapse(defaultLeft = false, defaultRight = false) {
  const [leftCollapsed, setLeftCollapsed] = useState(() => readStorage(LS_KEY_LEFT) || defaultLeft);
  const [rightCollapsed, setRightCollapsed] = useState(() => readStorage(LS_KEY_RIGHT) || defaultRight);

  const toggleLeft = useCallback(() => {
    setLeftCollapsed((prev) => {
      const next = !prev;
      writeStorage(LS_KEY_LEFT, next);
      return next;
    });
  }, []);

  const toggleRight = useCallback(() => {
    setRightCollapsed((prev) => {
      const next = !prev;
      writeStorage(LS_KEY_RIGHT, next);
      return next;
    });
  }, []);

  return {
    leftCollapsed,
    rightCollapsed,
    toggleLeft,
    toggleRight,
  };
}
