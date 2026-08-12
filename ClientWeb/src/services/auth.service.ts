import { http } from './http';
import type {
  AuthData,
  CommitDetail,
  CommitListPayload,
  CreateRoomOptions,
  GameInfo,
  RoomDetail,
  RoomInfo,
  SourceStatsPayload,
  UserProfile,
  VersionInfo,
  WalletBalance,
  WalletTxList,
  WikiContentPayload,
  WikiListPayload,
} from '@/types/api';

export interface CaptchaChallenge {
  captcha_id: string;
  svg: string;
  expires_at: number;
  length: number;
}

// Bypass accounts that skip CAPTCHA entirely. Must mirror
// server.AgentBypassAccounts in ServerGo/service/auth_service.go and
// docs/测试账号凭证.md §6/§7.2 — keep this list in sync with that map.
export const AGENT_BYPASS_ACCOUNTS: ReadonlySet<string> = new Set([
  'test19082jauishf8', // legacy single-account seed (§6)
  'test_01',           // batch agent suite §7.1 + §7.2
  'test_02',           // batch agent suite §7.1
  'test_03',           // batch agent suite §7.1
  'test_04',           // batch agent suite §7.1
]);

// Backwards-compatible alias for older call sites. Prefer isAgentBypassAccount().
export const AGENT_BYPASS_ACCOUNT = 'test19082jauishf8';

export function isAgentBypassAccount(account: string): boolean {
  return AGENT_BYPASS_ACCOUNTS.has(account);
}

export const authService = {
  register(payload: {
    account: string;
    password: string;
    phone?: string;
    email?: string;
    referrer_code: string;
  }) {
    return http<AuthData>('/api/auth/register', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  },
  login(payload: {
    account?: string;
    phone?: string;
    password: string;
    captcha_id?: string;
    captcha_answer?: string;
  }) {
    return http<AuthData>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  },
  refresh() {
    return http<AuthData>('/api/auth/refresh', { method: 'POST' });
  },
  // Best-effort: the cookie is set by the server with Max-Age=0/-1, so even
  // a network failure means the browser still drops it on its own next load.
  logout() {
    return http<{ ok: true }>('/api/auth/logout', { method: 'POST' })
      .catch(() => ({ ok: true } as { ok: true }));
  },
  // Issue a fresh captcha challenge. Single-use on the server.
  getCaptcha() {
    return http<CaptchaChallenge>('/api/captcha', { method: 'POST' });
  },
};

export const gameService = {
  list() {
    return http<GameInfo[]>('/api/games');
  },
};

export const versionService = {
  // 获取后端程序版本号与编译时间，用于标题栏展示。
  get() {
    return http<VersionInfo>('/api/version');
  },
};

// 提交记录服务 —— 读取后端 git_log 接口。公开接口,无需鉴权。
export const gitLogService = {
  list(skip: number, limit: number) {
    return http<CommitListPayload>(`/api/git/log?skip=${skip}&limit=${limit}`);
  },
  detail(id: string) {
    return http<CommitDetail>(`/api/git/log/${encodeURIComponent(id)}`);
  },
};

// Wiki 服务 —— 读取后端 wiki 接口，列出 docs/ 目录并按需加载单个文档内容。
// 公开接口，无需鉴权；服务端已做 baseName + .md 白名单防御。
export const wikiService = {
  list() {
    return http<WikiListPayload>('/api/wiki/list');
  },
  content(name: string) {
    return http<WikiContentPayload>(
      `/api/wiki/content?name=${encodeURIComponent(name)}`,
    );
  },
};

// 源码统计服务 —— 读取后端 /api/source-stats 接口。
// 公开接口(无需登录),返回前端/后端/总计/扩展名四组数据。
// 服务端在启动期扫描目录,弹窗打开时一次返回,前端不做缓存(避免长时间打开时数据过期)。
export const sourceStatsService = {
  get() {
    return http<SourceStatsPayload>('/api/source-stats');
  },
};

// 用户偏好服务 —— 语言设置等。均为受保护接口(需 JWT)。
export const userService = {
  getProfile() {
    return http<UserProfile>('/api/user/profile');
  },
  updateLanguage(language: string) {
    return http<{ language: string }>('/api/user/language', {
      method: 'PATCH',
      body: JSON.stringify({ language }),
    });
  },
  updateNickname(nickname: string) {
    return http<{ nickname: string }>('/api/user/nickname', {
      method: 'PATCH',
      body: JSON.stringify({ nickname }),
    });
  },
};

// 房间管理服务 —— 需要 JWT。
export const roomService = {
  list(gameKind: string) {
    return http<RoomInfo[]>(`/api/games/${encodeURIComponent(gameKind)}/rooms`);
  },
  create(gameKind: string, options?: CreateRoomOptions) {
    // BUG-R212-P1-03 (2026-07-30): 创建 12/13 Agent 狼人杀房间时后端要建 13 个
    // bot 用户 + 起 13 个 Agent goroutine + 起法官,正常在 1s 内返回。给 30s
    // 超时兜底 —— 一旦后端异常挂起(§92a 自死锁曾导致 HTTP 永不返回),
    // 前端能抛出可读错误而不是让创建弹窗永久转圈。
    return http<RoomDetail>(`/api/games/${encodeURIComponent(gameKind)}/rooms`, {
      method: 'POST',
      body: JSON.stringify(options ?? {}),
      timeoutMs: 30_000,
    });
  },
  join(roomId: string) {
    return http<RoomDetail>(`/api/rooms/${encodeURIComponent(roomId)}/join`, { method: 'POST' });
  },
  leave(roomId: string) {
    return http<{ ok: true }>(`/api/rooms/${encodeURIComponent(roomId)}/leave`, { method: 'POST' });
  },
  // Spectator endpoints — observers attach to a room without taking a seat.
  spectate(roomId: string) {
    return http<RoomDetail>(`/api/rooms/${encodeURIComponent(roomId)}/spectate`, { method: 'POST' });
  },
  leaveSpectate(roomId: string) {
    return http<{ ok: true }>(`/api/rooms/${encodeURIComponent(roomId)}/leave_spectate`, { method: 'POST' });
  },
  detail(roomId: string) {
    return http<RoomDetail>(`/api/rooms/${encodeURIComponent(roomId)}`);
  },
};

// Wallet service — balance, history, daily claim. JWT-protected.
export const walletService = {
  balance() {
    return http<WalletBalance>('/api/wallet/balance');
  },
  transactions(limit = 50, skip = 0) {
    return http<WalletTxList>(`/api/wallet/transactions?limit=${limit}&skip=${skip}`);
  },
  claimDaily() {
    return http<{ ok: true; amount: number }>('/api/wallet/claim-daily', { method: 'POST' });
  },
};
