// useSpectatorMode — true when the current route is a `/<game>/spectate/:roomId`
// sibling. The five game pages consult this to switch between player join vs.
// spectator subscribe, and to hide move / resign controls.

import { useLocation } from 'react-router-dom';

export function useSpectatorMode(): boolean {
  const { pathname } = useLocation();
  return /^\/[a-z]+\/spectate\//.test(pathname);
}
