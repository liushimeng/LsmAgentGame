import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import { wsClient } from '@/services/ws';
import { useUiStore } from '@/store/ui.store';

// 从对局路由解析出 roomId + gameKind + 额外 join 参数 + 是否 spectator 模式。
// 支持：/xiangqi/:roomId、/chess/:roomId、/junqi/:roomId、/doudizhu/:roomId、/texasholdem/:roomId
// 以及 spectator 兄弟路由：/<game>/spectate/:roomId。
function parseGameRoute(pathname: string):
  | { gameKind: 'xiangqi' | 'chess' | 'junqi' | 'doudizhu' | 'texasholdem' | 'werewolf'; roomId: string; joinExtra: Record<string, unknown>; spectator: boolean }
  | null {
  const m = pathname.match(/^\/(xiangqi|chess|junqi|doudizhu|texasholdem|werewolf)(?:\/spectate)?\/([^/]+)$/);
  if (!m) return null;
  const gameKind = m[1] as 'xiangqi' | 'chess' | 'junqi' | 'doudizhu' | 'texasholdem' | 'werewolf';
  const roomId = decodeURIComponent(m[2]);
  const spectator = pathname.includes('/spectate/');
  // 各游戏 join 帧所需的额外字段，与各 GamePage 首次 join 保持一致。
  const joinExtra: Record<string, unknown> =
    gameKind === 'chess'
      ? { game_kind: 'chess' }
      : gameKind === 'junqi'
        ? { game_kind: 'junqi', mode: 'hidden' }
        : gameKind === 'doudizhu'
          ? { game_kind: 'doudizhu' }
          : gameKind === 'texasholdem'
            ? { game_kind: 'texasholdem' }
            : gameKind === 'werewolf'
              ? { game_kind: 'werewolf' }
              : {}; // 象棋首次 join 仅需 room_id
  return { gameKind, roomId, joinExtra, spectator };
}

/**
 * useSessionRestore —— 断线重连/刷新后的状态恢复编排。
 *
 * 在 AppLayout 顶层挂载一次。每当 WS（重新）连接成功（wsClient.onOpen），按当前
 * 路由自动重建用户所在房间与对局状态：
 *   1. 会话：token 已由 zustand persist 恢复并自动带在 WS query 上，无需额外动作。
 *   2. 房间/对局：若当前停留在某对局页，则依次发送
 *        room.join → game.join → game.state
 *      让服务端把该连接重新加入房间订阅，并回放完整棋盘/对局视图。
 *   3. 聊天：由 useChat 自行用 onOpen 重订阅，这里不处理。
 *
 * 由于刷新页面会重新建立 WS，同一恢复路径对 F5 刷新与中途断线都生效。
 */
export function useSessionRestore() {
  const location = useLocation();

  useEffect(() => {
    // 进入对局页时自动折叠 sidebar + chat，给棋盘 / 牌桌留出最大空间；
    // 离开对局时**不**主动恢复展开状态 —— 用户在大厅折叠的偏好必须保留,
    // 否则每次从对局返回大厅,localStorage 里记的折叠状态都会被本 hook 覆盖,
    // 出现"刷新页面后折叠状态没记住"的 bug。
    // 移动端默认就已经折叠，不需要再 set。
    const parsedRoute = parseGameRoute(location.pathname);
    const uiState = useUiStore.getState();
    if (uiState.breakpoint !== 'mobile' && parsedRoute) {
      // 仅在进入对局页时强制折叠一次,确保棋盘有最大可视空间。
      if (!uiState.sidebarCollapsed) uiState.setSidebarCollapsed(true);
      if (!uiState.chatCollapsed) uiState.setChatCollapsed(true);
    }
    // 非对局路由:不主动 set 任何折叠状态,保留用户在 localStorage 的偏好。

    const restore = () => {
      const parsed = parseGameRoute(location.pathname);
      if (!parsed) return;
      const { roomId, joinExtra, spectator } = parsed;
      // 重新加入房间订阅（幂等：已在房间则服务端返回详情）。
      wsClient.send('room.join', { room_id: roomId });
      // spectator 路由：服务端已有 role='spectator' 行（HTTP POST /api/rooms/:id/spectate
      // 已写），不能再发 game.join，否则会触发 ErrAlreadyInOtherRole（30012）让
      // game.spectate 也走不通。这里直接走 game.spectate 让服务端把本连接加入
      // hub.spectators[roomID] 并立即推一份脱敏 game.state。
      if (spectator) {
        wsClient.send('game.spectate', { room_id: roomId, ...joinExtra });
        wsClient.send('game.state', { room_id: roomId, ...joinExtra });
        return;
      }
      // 玩家模式：重新加入对局并拉取完整状态。
      wsClient.send('game.join', { room_id: roomId, ...joinExtra });
      wsClient.send('game.state', { room_id: roomId, ...joinExtra });
    };

    // 若当前已是连接状态，立即恢复一次（覆盖「先连上、后进入对局页」的场景）。
    if (wsClient.connected) restore();
    // 之后每次（重）连接成功都恢复。
    const off = wsClient.onOpen(restore);
    return off;
  }, [location.pathname]);
}
