// useSpectatorMode — true when the current route is a `/<game>/spectate/:roomId`
// sibling. The five game pages consult this to switch between player join vs.
// spectator subscribe, and to hide move / resign controls.

import { useLocation } from 'react-router-dom';

export function useSpectatorMode(): boolean {
  const { pathname } = useLocation();
  // 2026-09-25 §观战35036修复 — 路由段允许小写字母 / 数字 / 连字符
  // (/virtual-city/spectate/:id)。旧正则 [a-z]+ 不匹配含 '-' 的 virtual-city,
  // 导致观战页误发 game.join → 35036。
  return /^\/[a-z][a-z0-9-]*\/spectate\//.test(pathname);
}
