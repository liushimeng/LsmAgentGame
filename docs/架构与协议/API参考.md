# API 参考

所有响应统一使用以下信封结构：

```json
{ "code": 0, "message": "ok", "data": { ... } }
```

`code: 0` 表示成功，其他均为错误；错误码契约以 `ServerGo/errcode/errcode.go` 中的表为准。

## HTTP

### `GET /api/health`
- **鉴权**：公开
- **200 响应**：`{ code:0, data:{ status:"ok", time:"2025-…Z" } }`

### `GET /api/games`
- **鉴权**：公开
- **200 响应**：`{ code:0, data:[ {id, name, kind, online}, … ] }`
- 返回大厅游戏目录（真实游戏记录入库前的占位列表）。

### `POST /api/auth/register`
- **鉴权**：公开
- **请求体**：`{ "account": "alice", "password": "P@ssw0rd!", "phone": "…", "email": "…", "invite_code": "…" }`
- **校验**：账号 3-32 字符、密码 6-64 字符、若提供 email 则需符合 RFC 校验
- **副作用**：成功后下发 `Set-Cookie: lsm_auth=…; HttpOnly; Secure; SameSite=Strict; Max-Age=172800`
- **200 响应**：`{ code:0, data:{ user_id, token, expires_at } }`
- **错误**：10103（账号已存在）、10104（邮箱已注册）、10105（手机号已注册）、10201-10204（邀请码问题）、20001（参数校验失败）

### `POST /api/captcha`
- **鉴权**：公开
- **请求体**：`{}`
- **200 响应**：`{ code:0, data:{ captcha_id, svg, expires_at, length } }`
- 一次性验证码（默认 5 位字符、180 秒有效），单独保存在服务端内存，`/api/auth/login` 时提交。
- **错误**：40001（生成失败，例如随机源异常）

### `POST /api/auth/login`
- **鉴权**：公开
- **请求体**（账号模式）：`{ "account": "alice", "password": "P@ssw0rd!", "captcha_id": "…", "captcha_answer": "…" }`
- **请求体**（手机号模式）：`{ "phone": "+86138…", "password": "P@ssw0rd!", "captcha_id": "…", "captcha_answer": "…" }`
- **Agent 旁路**：当 `account === "test19082jauishf8"` 时可省略 captcha 字段（大小写敏感）。
- **副作用**：成功后下发 `Set-Cookie: lsm_auth=…; HttpOnly; Secure; SameSite=Strict; Max-Age=172800`
- **200 响应**：`{ code:0, data:{ user_id, token, expires_at } }`
- **错误**：10101（账号不存在）、10102（密码错误）、10301（缺验证码）、10302（验证码错误）、10303（验证码已过期）

### `POST /api/auth/logout`
- **鉴权**：公开
- **副作用**：`Set-Cookie: lsm_auth=; Max-Age=0`，立即清除登录 cookie。
- **200 响应**：`{ code:0, data:{} }`

### `POST /api/auth/refresh`
- **鉴权**：Bearer
- **副作用**：滚动 cookie（重新下发 48h 的 `lsm_auth`）。
- **200 响应**：`{ code:0, data:{ user_id, token, expires_at } }`
- **错误**：10001/10002/10003（token 相关错误）、10101（账号不存在）

## WebSocket

### `GET /ws?token=<jwt>`  （Upgrade: websocket）
- **鉴权**：JWT 通过 query 字符串传递
- **端点**：同时监听 HTTPS 39001 和 WSS 39002 端口
- **主要端口**：39001（前端通过 `location.host` 自动连接）
- **线缆格式**：JSON 信封（见下文）。阶段三将切换为 protobuf。
- **服务端推送**：每 15 秒一次 `heartbeat`。
- **客户端发送**：任意信封——当前服务端会原样回 `ack`。
```json
{ "type": "heartbeat", "seq": 0, "payload": { "server_ts": 1700000000000 } }
{ "type": "ack",       "seq": 7, "payload": { ...回显内容... } }
{ "type": "error",     "payload": { "code": 20001, "message": "bad envelope" } }
```

## 错误码（节选）

| 错误码 | 含义                        |
|--------|-----------------------------|
| 0      | 成功                        |
| 10001  | 缺少鉴权 token              |
| 10002  | 鉴权 token 无效             |
| 10003  | 鉴权 token 已过期           |
| 10101  | 账号不存在                  |
| 10102  | 密码不匹配                  |
| 10103  | 账号已存在                  |
| 10104  | 邮箱已注册                  |
| 10105  | 手机号已注册                |
| 10201-10204 | 邀请码问题             |
| 10301  | 缺少验证码                  |
| 10302  | 验证码错误                  |
| 10303  | 验证码已过期                |
| 20001  | 参数校验失败                |
| 40001  | 服务器内部错误              |
| 40002  | 数据库错误                  |

## 鉴权 Cookie

登录 / 注册 / refresh 成功后，响应头会包含：

```
Set-Cookie: lsm_auth=<base64url(nonce‖AES-256-GCM(plaintext))>; Path=/; Max-Age=172800; HttpOnly; Secure; SameSite=Strict
```

负载格式：`v1|<user_id>|<issued_at>|<expires_at>`，使用 `LsmWebGame.conf → cookie.secret`（缺省回退到 `jwt.secret`）进行 AES-256-GCM 加密。默认 48 小时有效。`/api/auth/logout` 通过 `Max-Age=0` 清除。

## 钱包 API（游戏金币系统）

> 参见 CLAUDE.md §19。所有端点需要 JWT（`Authorization: Bearer <token>`）。

### GET `/api/wallet/balance`

返回当前用户余额。

```json
{ "code": 0, "message": "ok", "data": { "user_id": "...", "balance": 1000 } }
```

### GET `/api/wallet/transactions?limit=20&offset=0`

分页查询钱包流水（最大 200 / 次）。

```json
{
  "code": 0, "message": "ok",
  "data": {
    "total": 42, "limit": 20, "offset": 0,
    "transactions": [
      {
        "id": "...", "user_id": "...",
        "tx_type": "daily_login",
        "amount": 2000,
        "balance_after": 3000,
        "ref_type": "", "ref_id": "",
        "game_kind": "",
        "remark": "每日登录奖励",
        "created_at": "2026-07-02T10:00:00Z"
      }
    ]
  }
}
```

### POST `/api/wallet/claim-daily`

手动领每日奖励（**幂等**）。每次 UTC+8 自然日仅生效一次。

**成功（首次领取）**：
```json
{ "code": 0, "message": "ok", "data": { "claimed": true, "amount": 2000, "balance_after": 3000 } }
```

**已领取（同一天重入）**：
```json
{ "code": 30014, "message": "daily reward already claimed today" }
```

### 错误码

| HTTP | Code | 说明 |
|---|---|---|
| 200 | 30014 | 今日已领 |
| 400 | 30013 | 余额不足 |
| 400 | 30015 | 流水写入失败 |
| 401 | 10001/10003 | 登录鉴权失败（与现有规则一致）|

### WebSocket `wallet.balance` 推送

服务端在余额变化时主动推送：

```
wss://127.0.0.1:39002/ws?token=<jwt>
```

帧体：
```json
{ "type": "wallet.balance", "balance": 12345, "delta": 2000, "reason": "daily_login" }
```

前端禁止本地修改余额，须以服务端推送为准。
