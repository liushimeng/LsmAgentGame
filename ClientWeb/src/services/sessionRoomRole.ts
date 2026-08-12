/**
 * sessionRoomRole — 纯前端 sessionStorage 工具,记录「我在哪些房间是 player / spectator」
 *
 * 背景 (2026-07-30 §R210-05):
 *   - 用户在 12AI+1 真人房间创建成功后:后端把 userID 写入 t_lsm_game_player 行,
 *     Role = 'player'。Lobby 列表通过 REST 拿到 my_role = 'player'。
 *   - 但当用户**刷新页面**后第一次进入对局路由(`/werewolf/:roomId`),GamePage
 *     在 useEffect 里直接发 `game.join` WS 帧;服务端在房间 Status == 'playing'
 *     时拒绝 (ErrRoomFull = 30001),导致 GamePage 看不到任何 state,UI 卡死。
 *   - 修复路径:把"我已是 player"这个事实缓存到 sessionStorage,GamePage mount
 *     时先查缓存;命中则**跳过 game.join**,仅发 `requestState`,避免被 30001 拒绝。
 *
 * 设计取舍:
 *   - 用 sessionStorage (而不是 localStorage) — 浏览器关闭即清,避免长期脏数据
 *   - storage key: `ww_room_role_${roomId}` → 'player' | 'spectator'
 *   - 调用方: WerewolfLobbyPage (handleCreate/handleJoin/handleSpectate 成功后) +
 *            WerewolfGamePage (useEffect 启动时读)
 *   - 失败容错: SSR/隐私模式/disk full → 全部静默返回 null,不抛错
 */

const KEY_PREFIX = 'ww_room_role_';

export type SessionRoomRole = 'player' | 'spectator' | 'agent';

/**
 * ReadCachedRoomRole — 读取用户在 roomId 的缓存角色。
 * 命中: 'player' | 'spectator' | 'agent'
 * 未命中: null (包括 SSR、storage 不可用、解析失败)。
 */
export function ReadCachedRoomRole(roomId: string): SessionRoomRole | null {
  if (typeof window === 'undefined' || !roomId) return null;
  try {
    const raw = window.sessionStorage.getItem(KEY_PREFIX + roomId);
    if (!raw) return null;
    if (raw === 'player' || raw === 'spectator' || raw === 'agent') return raw;
    return null;
  } catch {
    // 隐私模式 / 配额已满 / security policy → 静默降级
    return null;
  }
}

/**
 * WriteCachedRoomRole — 写入用户在 roomId 的角色。
 * 失败容错: storage 不可用时静默,不抛错。
 */
export function WriteCachedRoomRole(roomId: string, role: SessionRoomRole): void {
  if (typeof window === 'undefined' || !roomId) return;
  try {
    window.sessionStorage.setItem(KEY_PREFIX + roomId, role);
  } catch {
    // 静默
  }
}

/**
 * ClearCachedRoomRole — 离开房间时清掉缓存。
 * 通常在 leaveGame / unspectate 后调用,避免用户退出后再次进入该房间误判。
 */
export function ClearCachedRoomRole(roomId: string): void {
  if (typeof window === 'undefined' || !roomId) return;
  try {
    window.sessionStorage.removeItem(KEY_PREFIX + roomId);
  } catch {
    // 静默
  }
}
