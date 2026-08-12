# Agent 钱包与每日 Grant 设计文档

> 2026-07-14 §135 — LLM Bot 钱包体验与人类玩家对齐：默认金币 5000 +
> 超级管理员每日 grant 批量发放。本文档配套 CLAUDE.md §118 / §121 / §135
> 等既有 lessons，**不**修改三个规则文件。

## 1. 设计目标

1. **公平性** — Bot 玩家的初始金币从 1000 提升到 5000，与预期的人类用户
   注册奖励量级对齐（人类 `register_bonus` 钱包流水入口相同）。
2. **日常运营可恢复** — Bot 参与狼人杀等会消耗 / 产出金币（±100/局），长
   期运营可能因连续输局陷入负资产。超级管理员可「每日一次」批量给所有
   enabled Provider 的 bot 钱包发金币，**防止运营被迫手工逐 bot 单条
   调整**。
3. **可审计** — 每次 grant 写入 `t_lsm_game_admin_grant` 去重表 +
   `t_lsm_game_wallet_tx` 双簿记流水，与现有 wallet 双源真理一致。

## 2. 现状（2026-07-14 之前）

| 组件 | 状态 | 说明 |
|------|------|------|
| `t_lsm_game_wallet` | ✅ | 已存在，唯一键 `user_id` |
| `t_lsm_game_wallet_tx` | ✅ | 已存在，复合索引 `(user_id, created_at)` |
| `BotUserService.EnsureBotUserForProvider` | ✅ | 启动时建 bot user + 钱包 |
| `WalletService.Credit / Debit` | ✅ | 原子加 / 减 + 写流水 |
| `ModelWalletAPI.AdjustBotWallet` | ✅ | 超管单 bot 调整（super only） |
| `ModelDetailPage` 钱包卡片 | ✅ | 显示余额 + 最近 20 条流水 |
| `ModelDetailPage` 对局历史 | ✅ | 显示最近 20 局（`listProviderGames`） |
| 5000 默认金币 | ❌ | `DefaultInitialBalance=1000` |
| 每日 grant 端点 | ❌ | **新功能** |
| grant 去重表 | ❌ | **新表** |

> 关于「钱包暂无流水」「暂无对局」：当一个 Provider 的 bot 还没参与过任何
> 完整对局时，UI 自然显示为空。这不是 bug，是预期行为。

## 3. 数据库变更

### 3.1 新表 — `t_lsm_game_admin_grant`

```sql
CREATE TABLE t_lsm_game_admin_grant (
  id              CHAR(36)    NOT NULL,
  provider_id     CHAR(36)    NOT NULL,
  grant_date      DATE        NOT NULL,
  granted_by_uid  CHAR(36)    NOT NULL DEFAULT '',
  amount          BIGINT      NOT NULL,
  bot_user_id     CHAR(36)    NOT NULL,
  balance_after   BIGINT      NOT NULL DEFAULT 0,
  remark          VARCHAR(255) NOT NULL DEFAULT '',
  created_at      DATETIME    NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_provider_date (provider_id, grant_date),
  KEY idx_bot_user (bot_user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

**索引说明**：
- `uk_provider_date` 是 **复合唯一键**，强制"每 provider 每天 1 次 grant"。
- `idx_bot_user` 让"按 bot 查 grant 历史"走索引（非必须，单条 grant 一行即可）。

### 3.2 已有表无变更

- `t_lsm_game_wallet`、`t_lsm_game_wallet_tx` — 仅多写一种
  `tx_type='admin_daily_grant'` 的流水，复用 `tx_type` 上现有单列索引。
- `t_lsm_game_llm_provider` — 不变。

### 3.3 默认金币从 1000 改 5000

`ServerGo/service/wallet_service.go` 常量 `DefaultInitialBalance=1000 → 5000`。
**Backfill 不做** — 现有 bot 钱包不动，避免对已经在对局中的玩家账目造成
回滚型不一致；新注册用户 / 新建 bot 自动获 5000。

## 4. 后端接口

### 4.1 `POST /api/admin/llm/bots/grant-daily`

权限：**超级管理员**（`user_type === 3`，与现有 `AdjustBotWallet` 对齐）。

请求体：
```json
{
  "provider_id": "<可选;空 = 全部 enabled provider>",
  "amount": 500,          // 1..1,000,000 整数
  "remark": "每日金币发放"  // 1..255 字符
}
```

响应（HTTP 200）：
```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "granted": [
      { "provider_id": "...", "provider_name": "豆包 2.0", "bot_user_id": "...", "amount": 500, "balance_after": 5500 }
    ],
    "skipped": [
      { "provider_id": "...", "provider_name": "DeepSeek V4-Pro", "bot_user_id": "...", "amount": 0 }
    ],
    "date": "2026-07-14"
  }
}
```

- `granted`: 本次成功发放的 provider 列表（含发放后余额）。
- `skipped`: 今日（UTC+8）已发放过的 provider 列表（重复点击去重结果）。
- `date`: UTC+8 日期 `YYYY-MM-DD`。

### 4.2 错误码

新增 `ErrAdminGrantAlreadyClaimed = 30022`（**保留**，目前接口返回
`skipped[]` 而不抛该错；将来 GET 端点拉历史时复用）。

### 4.3 三种典型失败场景

| 场景 | 处理 |
|------|------|
| provider 缺 bot user | 自动 `EnsureBotUserForProvider` 补建 |
| (provider, date) 已 grant | INSERT 收到 MySQL 1062 → 加入 `skipped[]`，不报错 |
| WalletService.Credit 失败 | DELETE 已插入的 dedup 行，`continue` 到下一个 provider，**log warn 不中断** |
| 全部 provider 都没启用 | 400 `ErrValidationFailed` |

整体设计：单 RPC + 后端循环逐 provider 调 `walletSvc.Credit`，每条失败仅
`warn` 日志，不中断批处理；与狼人杀结算（`agent/record_log.go:693-712`）
"失败仅 log 不阻塞游戏流"同语义。

## 5. 前端变更

| 文件 | 变更 |
|------|------|
| `ClientWeb/src/i18n/types.ts` | `Dict` 接口追加 16 个 `modelAdmin.detail.grant*` 键 |
| `ClientWeb/src/i18n/locales/zh-CN.ts` | 中文物色 |
| `ClientWeb/src/i18n/locales/en.ts` | 英文文案 |
| `ClientWeb/src/i18n/locales/ja.ts` | 日文文案 |
| `ClientWeb/src/api/modelAdmin.ts` | 新增 `grantDailyToAll()` 封装 + 类型 |
| `ClientWeb/src/pages/ModelAdminPage.tsx` | 顶部「🎁 每日金币发放」按钮 + `<GrantDialog>` 弹窗（form / submitting / result 三态） |
| `ClientWeb/src/pages/ModelDetailPage.tsx` | 钱包卡片头部加单 model grant 按钮（确认 → 调 `grantDailyToAll({provider_id})`） |

### 5.1 双入口设计

| 入口 | 位置 | 用途 |
|------|------|------|
| 列表页 toolbar | `/admin/models` | 一键对所有 enabled model 批量发放 |
| 详情页 wallet | `/admin/models/:providerId` | 单独为某个 model 补发（例如某模型连续几天失败管理员补贴） |

两个入口共用同一个后端 `POST /api/admin/llm/bots/grant-daily`：
- 列表页传 `provider_id` 缺省 → 后端遍历所有 enabled provider。
- 详情页传 `provider_id` 非空 → 后端只针对该 provider。

## 6. 测试与验证

### 6.1 数据库

```sql
-- 期望空（新表）
SELECT COUNT(*) FROM t_lsm_game_admin_grant;

-- 重复点击第二次跑 grant，第 2 次应进 skipped
-- 已有 grant 行（按 schema 复合唯一键）

-- 流水验证：每天每 bot 至多 1 行 admin_daily_grant
SELECT user_id, COUNT(*) AS cnt
FROM t_lsm_game_wallet_tx
WHERE tx_type = 'admin_daily_grant'
  AND created_at >= CURRENT_DATE
GROUP BY user_id;
```

### 6.2 curl 脚本

```bash
# 1) 登录测试超管账号(test_01 在本地数据库已配置 user_type=3)。
# 密码请从仓库根目录的 test_account.json 读取(不入 git),不要硬编码到脚本。
TOKEN=$(curl -ks -X POST https://127.0.0.1:39001/api/auth/login \
  -H 'Content-Type: application/json' \
  -d "{\"account\":\"test_01\",\"password\":\"$TEST_01_PASSWORD\"}" \
  | jq -r '.data.token')

# 2) 批量 grant(每天只生效 1 次)
curl -ks -X POST https://127.0.0.1:39001/api/admin/llm/bots/grant-daily \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"amount":500,"remark":"每日发放测试"}' \
  | jq .

# 期望:granted.length = 8, skipped.length = 0
# 再跑一次:granted.length = 0, skipped.length = 8
```

### 6.3 账户信息

- 测试账号: `test_01`(密码从 `test_account.json` 读取,不入 git)
- 详见 `docs/通用功能/测试账号凭证.md`

## 7. 边界与不做的事项

| 不做 | 理由 |
|------|------|
| 人类玩家每日 grant | 已有 `claim-daily`；如需"超级管理员强制给人类玩家发"应走单点 adjust |
| 每周 / 每月 grant 周期 | 一次性只做 daily；如要周期化，加 cron-like 后台服务 |
| 历史 grant 回滚 | 双簿记是 audit 事实，纠错走反向 Debit（`adjustBotWallet(amount < 0)`） |
| 与人类 claim-daily 跨类整合 | 两条线不同语义（人类每日签到 vs 运营每日给 bot 充值），分开存储更可审计 |
| 现有 bot 回填 5000 | 改常量只影响未来；存量不动 |

## 8. 相关既有 lessons

- §118 — 模型管理 + 模型玩家持久化：bot 是 `t_lsm_game_user` 的 IsBot=true 行，
  钱包复用人类 `t_lsm_game_wallet`。
- §121 — 后端 data 形状与前端类型不匹配：所有 admin 端点统一 `http<T>()` 封装
  + 显式 wrapper 类型。
- §135 — Bot user_id 与 provider_id 解耦：`GetBotUserForProvider` 优先
  `BotProviderID` 索引，详情页钱包路径用 bot_user_id 而不是 provider.id。
- §118.2 — AES-256-GCM 加密 API Key：本功能不涉及。

## 9. 后续可选工作（不在本 PR）

- 把 `t_lsm_game_admin_grant` 接入后台 dashboard，按日 / 按 provider 聚合。
- 失败回滚 UI：admin 端展示 "今日 grant 失败 X 条" 健康度。
- 把 grant 周期从 daily 升级到 cron-like（管理员可配置）。
