# WebSocket 自动重连与状态恢复

> 本文档供 Agent 自动加载。涉及前端 WS 连接、Loading 遮罩、断线重连、刷新恢复、
> 以及用户列表 `user.*` 帧时，先读本文件。

## 1. 总览

登录成功后，所有界面交互通过 WebSocket 的 JSON 信封（Envelope）实现，沿用现有
`chat.* / room.* / game.*` 风格。新增 `user.*` 帧承载「用户列表」读取与删除。
（`proto/*.proto` 为契约文档，当前 on-wire 为 JSON，未引入 protoc 二进制 framing。）

WS 端点：`wss://HOST:39001/ws?token=<jwt>`（同时挂在 39002 向后兼容）。JWT 经
query string 传递，断线重连时自动带上（5 分钟窗口后尝试 `authService.refresh()` 换取新 token）。

## 2. 连接生命周期（唯一持有者：AppLayout）

- **`AppLayout`**（`components/layout/AppLayout.tsx`）是全站唯一的 WS 生命周期持有者：
  登录后 `wsClient.connect()`，登出后 `wsClient.close()`。
- 页面/路由切换**不再**各自 connect/close，避免误关唯一连接。
- `useWebSocket`（`hooks/useWebSocket.ts`）只观察最新帧，**不**connect/close。
- 各 GamePage 仍调用 `wsClient.connect()`（幂等，已连接时无副作用），可保留。

## 3. 自动重连（`services/ws.ts`）

`WsClient` 内置指数退避重连：

| 机制 | 说明 |
|------|------|
| 退避 | 起始 500ms，每次 ×2，上限 8s |
| 重连窗口 | `RECONNECT_WINDOW_MS = 5min`；窗口内持续重连 |
| token 刷新 | 窗口耗尽后调 `authService.refresh()`，成功则用新 token 重建 URL 并重置窗口；失败则停止（status=`closed`） |
| 状态广播 | `onStatus(fn)` 订阅 `idle/connecting/open/reconnecting/closed` |
| 重订阅 | `onOpen(fn)` 在每次（重）连成功时触发，用于重订阅/状态恢复 |

## 4. Loading 遮罩

- **`connection.store.ts`**（zustand）：模块首次 import 时通过 `wsClient.onStatus`
  把状态同步进 store；`overlayVisible = status ∈ {connecting, reconnecting}`。
  （`ReconnectingOverlay` 自己再叠加一条「曾经 open 过」的门槛。）
- **`ReconnectingOverlay`**（`components/common/ReconnectingOverlay.tsx`）：订阅 store，
  只在「之前已经 open 过、现在掉线进入 `reconnecting`」时显示全屏半透明 Loading
  (“正在重新连接服务器…”)。首次握手的 `connecting` 不显示遮罩 —— F5 / 登录
  跳转不该闪一下 spinner。`open` 后自动隐藏。由 `AppLayout` 在登录态下挂载。

## 5. 状态恢复（`hooks/useSessionRestore.ts`）

在 `AppLayout` 顶层挂载一次。每当 WS（重新）连接成功（`onOpen`），按**当前路由**重建状态：

1. **会话**：token 由 zustand `persist` 恢复并自动带在 WS query 上 —— 无需额外动作。
2. **房间/对局**：若停留在对局页（`/xiangqi/:roomId`、`/chess/:roomId`、`/junqi/:roomId`、`/texasholdem/:roomId`、Doudizhu 同上），
   依次发送 `room.join` → `game.join` → `game.state`：
   - `room.join`：把该连接重新加入房间订阅（幂等）。
   - `game.join`：重新进入对局（各游戏 join 额外参数见下）。
   - `game.state`：服务端回放完整棋盘/对局视图（含 hidden 模式按视角裁剪）。
   - join 额外参数：象棋 `{}`；国际象棋 `{ game_kind:'chess' }`；军棋 `{ game_kind:'junqi', mode:'hidden' }`；德州扑克 `{ game_kind:'texasholdem' }`。
3. **observer 路由**（`/xiangqi/spectate/:roomId` 等 5 个兄弟路由）：发 `room.join` + `game.spectate` + `game.state`，**不发** `game.join`（用户已通过 `POST /api/rooms/:id/spectate` 在 DB 注册了 `role='spectator'` 行，再发 `game.join` 会触发 `ErrAlreadyInOtherRole 30012`）。
4. **聊天**：`useChat` 自行用 `onOpen` 重订阅（lobby/room），本 hook 不处理。

> 刷新页面（F5）会重新建立 WS，因此**同一恢复路径**对刷新与中途断线都生效。
> observer 路由的恢复路径见 `CLAUDE.md §22`。

服务端用 `t_lsm_game_player (room_id, user_id)` 持久化房间归属；对局 in-progress 状态由
`ws/game_service.go:handleGetState`（`game.state` 帧）返回，五种游戏均已支持（象棋/国际象棋/军棋/斗地主/德州扑克）。

## 6. 用户列表 `user.*` 帧

后端 `ServerGo/ws/user_service.go`（`UserWsService`），按调用者 `user_type` 裁剪字段。

```
client → server
  user.list    {}            —— 请求用户列表（按权限裁剪）
  user.delete  { id }        —— 删除用户（仅超管，级联删除关联数据）

server → client
  user.list_resp   { users:[UserItem], my_user_type }
  user.delete_resp { id }    —— 删除发起者回执（带 seq）
  user.deleted     { id }    —— 广播给所有在线连接，实时刷新列表
  user.error       { code, message }
```

`UserItem` 字段可见性：

| 调用者 | 可见字段 |
|--------|----------|
| 普通(1) | `id, nickname, online` |
| 管理员(2) | 上述 + `account/phone/email/user_type/my_invite_code/referral_count/referrer_user_id/created_at/last_login_at`（**无密码**，`PasswordHash` 标了 `json:"-"`） |
| 超管(3) | 管理员字段 + `can_delete`（仅对「非超管且非自己」的行为 true） |

- 在线判定：`hub.ConnectedUserIDs()` → `map[userID]bool` 注入每行 `online`。
- 删除复用 `service.UserService.DeleteUserWithRelatedData`（事务级联：聊天消息 →
  房间席位 → 会话 → 把下级 `referrer_user_id` 置空 → 删用户本身）。
- 删除守卫：仅超管、禁止删自己、禁止删其它超管。
- 前端 `pages/AdminUsersPage.tsx` 全程走 `wsClient`，`onOpen` 重连后自动重发 `user.list`。
- 侧边栏 `AppSidebar.tsx` 中「用户列表」`minUserType:1`，对所有登录用户可见。

REST `/api/admin/users`（`api/admin_api.go`）保留向后兼容，前端已不再使用。

## 7. 关键文件

后端：`ws/user_service.go`、`ws/hub.go`(`BroadcastAll`/`ConnectedUserIDs`)、
`ws/client.go`/`ws/handler.go`/`main.go`(接线)、`proto/user.proto`、
`router/router.go`(静态资源缓存策略)。

前端：`services/ws.ts`(`onStatus`)、`store/connection.store.ts`、
`components/common/ReconnectingOverlay.tsx`、`hooks/useSessionRestore.ts`、
`components/layout/AppLayout.tsx`、`hooks/useWebSocket.ts`、
`components/layout/AppSidebar.tsx`、`pages/AdminUsersPage.tsx`。

## 8. 静态资源缓存策略（`router/router.go`）

为保证 bug fix 部署后用户能立即拿到新 bundle（而不是继续用浏览器缓存
的旧 JS 卡在「正在重新连接…」遮罩），静态资源按是否带 hash 区分对待：

| 路径 | 策略 | 原因 |
|------|------|------|
| `/assets/*` | `Cache-Control: public, max-age=31536000, immutable` | Vite 给 JS/CSS/图片加了 content hash，新部署的 URL 一定不同，老缓存自然作废 |
| `/`、`/index.html`、`/favicon.svg`、SPA fallback（任意非 `/api/*` 路径） | `Cache-Control: no-cache, no-store, must-revalidate` + `Pragma: no-cache` + `Expires: 0` | 不带 hash，必须每次回源；否则用户会一直拿到引用旧 asset 名的 `index.html` |
| `/api/*`、`/ws` | 不动 | API 与 WebSocket 自己有正确的语义 |
