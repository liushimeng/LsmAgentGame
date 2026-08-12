# LLM Provider 架构设计

> 本文档是后端「大模型调用模块 + 模型玩家持久化」的设计契约。
> 模块位于 `ServerGo/llm/` + `ServerGo/service/bot_user_service.go` + `ServerGo/api/model_admin_api.go`，
> 核心目标：
> 1. 通过**统一 Provider 接口**抽象不同厂商的模型调用（当前仅 Anthropic 协议，预留 OpenAI）。
> 2. 将模型从「配置文件条目」升级为「**数据库里的持久化玩家**」：DB-first + config 兜底。
> 3. 为「狼人杀 7 人局 Agent」提供可测、可限流、可观测、可对账的调用入口。
> 4. 配套管理 UI、金币钱包、对局日志全链路（详见 `docs/狼人杀-道具与经济/模型管理与持久化玩家设计.md`）。

---

## 1. 概述

### 1.1 背景与定位

LsmAgentGame 平台已上线 8 个 LLM 模型（豆包 / 智谱 / Kimi / MiniMax 等），统一通过
`ServerGo/llm/registry.go` 加载供狼人杀 Agent 调度。原实现存在三大短板：

| 短板 | 现象 | 影响 |
|------|------|------|
| 配置入口单一 | 增删改必须 SSH 改 `LsmAgentGame.conf` + 重启 | 运维门槛高、变更不可追溯、误改风险大 |
| 模型是「临时算力」 | 7 个 bot 调 LLM 后无任何持久化数据 | 无法复盘 / 调优 prompt / 分析决策 |
| 模型无法参与金币 | 5 款游戏的金币体系无法结算到模型 | 模型不是「玩家」 |

**本期改造目标**：

- 模型元数据持久化到 `t_lsm_game_llm_provider`，所有 CRUD 走 HTTP API，无需重启。
- 每个模型对应一个 **`t_lsm_game_user` 中的 bot 玩家行**（`IsBot=true`），享有与人类玩家完全相同的
  钱包 / 流水 / 对局日志。
- 狼人杀对局结束后，按胜/败给所有 bot 玩家自动结算金币（详见 `docs/狼人杀-道具与经济/模型玩家金币设计.md`）。

### 1.2 核心概念

| 概念 | 含义 |
|------|------|
| **Provider** | `t_lsm_game_llm_provider` 中的一行 = 一个大模型供应商条目 |
| **Bot User** | `t_lsm_game_user` 中 `IsBot=true` 的行，与 Provider 一对一绑定 |
| **Model Key** | `TLsmGameLlmProvider.Model` 字段（如 `DouBao-model`），是 Agent 调度的核心关联键 |
| **Game Log** | `t_lsm_game_model_game_log` 中的一行 = 一个 bot 玩家在一局游戏里的完整记录 |
| **Wallet** | `t_lsm_game_wallet` 中的一行 = 一个 bot 玩家的金币余额（默认 1000） |

### 1.3 加载策略一句话

> **DB-first**：启动时先看 `t_lsm_game_llm_provider` 有没有行，有则全从 DB 读；
> 没有则把 `cfg.LLM.Providers` 当 seed 写进 DB（API Key AES-GCM 加密）。
> 运行期所有 CRUD 通过 `Registry.Reload(ctx)` 立即生效，无需重启。

---

## 2. 目录与分层

### 2.1 整体目录树

```
ServerGo/
├── llm/
│   ├── llm.go              # LLMProvider 接口 + LLMRequest/LLMResponse 数据包
│   ├── anthropic/
│   │   ├── anthropic.go    # Anthropic Messages API 实现
│   │   └── stream.go       # SSE 流式 + Accumulator
│   ├── types/              # leaf 包：Anthropic wire 类型 + 接口 + ModelInfo
│   ├── registry.go         # DB-first 注册表 + Reload + SeedFromConfig
│   └── registry_db_test.go # 单元测试（LoadFromDB / SeedFromConfig / Reload）
│
├── models/
│   ├── t_lsm_game_llm_provider.go        # 【新】模型元数据表
│   ├── t_lsm_game_model_game_log.go      # 【新】模型对局日志
│   ├── t_lsm_game_model_chat_message.go  # 【新】模型聊天原文（含 thinking/tool_use）
│   ├── t_lsm_game_model_action.go        # 【新】模型动作决策记录
│   ├── t_lsm_game_kv.go                  # 【新】KV 表（存 AES-GCM 加密密钥）
│   ├── t_lsm_game_user.go                # 【改】加 IsBot / BotProviderID / LinkedProviderAccount
│   ├── t_lsm_game_wallet.go              # 【复用】模型钱包
│   └── t_lsm_game_wallet_tx.go           # 【复用】模型钱包流水
│
├── service/
│   ├── bot_user_service.go               # 【新】EnsureBotUserForProvider 注册 bot 玩家
│   ├── model_log_service.go              # 【新】对局日志查询 / 聚合
│   └── wallet_service.go                 # 【复用】ApplyTransaction 结算金币
│
├── util/
│   └── crypto.go                         # 【新】AES-GCM EncryptAPIKey / DecryptAPIKey
│
├── agent/
│   ├── record_log.go                     # 【新】RecordGameAction / RecordChatMessage hook
│   ├── tools.go                          # 【改】DispatchTool 后调 RecordGameAction
│   ├── run.go                            # 【改】handleSpeakFloorTick 后调 RecordGameAction
│   ├── prompt.go                         # 现有
│   └── memory.go                         # 现有
│
├── ws/
│   └── chat_service.go                   # 【改】SendFromBot 同步写 model_chat_message
│
├── game/werewolf/
│   └── activity_emitter.go               # 【改】EmitGameOver 内金币结算
│
├── api/
│   ├── llm_api.go                        # 现有 GET /api/llm/models
│   ├── model_admin_api.go                # 【新】CRUD + Reload + Test
│   ├── model_log_api.go                  # 【新】对局 / 消息 / 钱包查询
│   └── model_wallet_api.go               # 【新】管理员手动调整金币
│
├── config/
│   └── config.go                         # 【改】LLMConfig 加 EncryptionKey 字段；providers[] 标 deprecated
│
└── db/
    └── db.go                             # 【改】Init 中注册 5 张新表
```

### 2.2 文件行数预算

每个 Go 文件 ≤ 800 行；单文件上限 1500 行（CLAUDE.md §4）。

- `ServerGo/llm/registry.go` ≤ 600 行（DB-first + Reload 主体逻辑）
- `ServerGo/service/bot_user_service.go` ≤ 300 行（幂等 seed + 钱包 + backfill）
- `ServerGo/api/model_admin_api.go` ≤ 400 行（6 个端点）
- `ServerGo/agent/record_log.go` ≤ 300 行（异步 hook）

---

## 3. 表结构设计

### 3.1 `t_lsm_game_llm_provider` — 模型元数据

每个 LLM 模型一行，**由模型管理页面直接 CRUD**。API Key **必须加密存储**。

```go
// ServerGo/models/t_lsm_game_llm_provider.go
type TLsmGameLlmProvider struct {
    ID               string    `gorm:"type:char(36);primaryKey"            json:"id"`
    AgentName        string    `gorm:"type:varchar(64);uniqueIndex;not null"  json:"agent_name"`     // "豆包 2.0"
    Model            string    `gorm:"type:varchar(64);uniqueIndex;not null"  json:"model"`         // "DouBao-model"
    ProviderType     string    `gorm:"type:varchar(32);not null;default:'anthropic'"  json:"provider_type"`
    APIKeyEnc        string    `gorm:"type:text;not null"                  json:"-"`         // AES-GCM 加密,永不出库到前端
    APIKeyHint       string    `gorm:"type:varchar(16);not null;default:''"  json:"api_key_hint"`  // "sk-abcd...wxyz"
    Endpoint         string    `gorm:"type:varchar(256);not null;default:''" json:"endpoint"`     // 覆盖全局 endpoint,空则用全局
    ThinkingRequired bool      `gorm:"not null;default:false"              json:"thinking_required"`
    ThinkingBudget   int       `gorm:"not null;default:0"                  json:"thinking_budget"`
    Enabled          bool      `gorm:"not null;default:true"               json:"enabled"`
    Remark           string    `gorm:"type:varchar(255);default:''"        json:"remark"`
    CreatedAt        time.Time `gorm:"autoCreateTime"                       json:"created_at"`
    UpdatedAt        time.Time `gorm:"autoUpdateTime"                       json:"updated_at"`
}
```

**字段说明**：

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | char(36) | UUID v4 |
| `agent_name` | varchar(64) 唯一 | 人类可读名称，UI 展示 |
| `model` | varchar(64) 唯一 | 发送给代理的 `model` 字段值，**核心关联键** |
| `provider_type` | varchar(32) | 协议类型，当前仅 `"anthropic"`；预留 `"openai"` |
| `api_key_enc` | text | AES-GCM 加密后的密文，**json:"-"` 永不返回** |
| `api_key_hint` | varchar(16) | key 的前后 4 位明文摘要，用于 UI 展示，例 `sk-abcd...wxyz` |
| `endpoint` | varchar(256) | 覆盖全局 endpoint；空字符串使用 `cfg.LLM.Endpoint` |
| `thinking_required` | bool | 是否启用 Extended Thinking |
| `thinking_budget` | int | thinking token 预算 |
| `enabled` | bool | 软删除标志；`false` 时新建房间不出现，**历史数据保留** |
| `remark` | varchar(255) | 备注 |

**索引**：

| 索引 | 列 | 用途 |
|------|----|------|
| `PRIMARY` | `id` | 主键 |
| `uniq_agent_name` | `agent_name` | 唯一约束 |
| `uniq_model` | `model` | 唯一约束（**核心关联键**） |
| `idx_enabled` | `enabled` | 过滤可用模型 |

### 3.2 `t_lsm_game_user` 扩展字段

不新建表，复用现有 `t_lsm_game_user`，新增 3 字段：

```go
// ServerGo/models/t_lsm_game_user.go（追加）
IsBot                  bool       `gorm:"not null;default:false;index"        json:"is_bot"`
BotProviderID          *string    `gorm:"type:char(36);index"                 json:"bot_provider_id,omitempty"`
LinkedProviderAccount  string     `gorm:"type:varchar(64);not null;default:''"  json:"linked_provider_account"`
```

**字段语义**：

| 字段 | 含义 |
|------|------|
| `IsBot` | 标记这是模型玩家，非人类；**禁止人类用 bot 账号登录**（§11 安全） |
| `BotProviderID` | 外键指向 `t_lsm_game_llm_provider.id`，可空（人类用户为 NULL） |
| `LinkedProviderAccount` | 虚拟账号字符串，用于日志和诊断；可与 `account` 相同 |

**backfill**：现有行 `IsBot=false`、`BotProviderID=nil`、`LinkedProviderAccount=''`，无需迁移。

### 3.3 `t_lsm_game_model_game_log` — 模型对局日志

每个 bot 玩家在每局游戏里一条主记录：

```go
// ServerGo/models/t_lsm_game_model_game_log.go
type TLsmGameModelGameLog struct {
    ID            string     `gorm:"type:char(36);primaryKey"  json:"id"`
    ProviderID    string     `gorm:"type:char(36);index:idx_provider_created,priority:1;not null"  json:"provider_id"`
    BotUserID     string     `gorm:"type:char(36);index;not null"  json:"bot_user_id"`
    RoomID        string     `gorm:"type:char(36);index;not null"  json:"room_id"`
    GameKind      string     `gorm:"type:varchar(32);index;not null"  json:"game_kind"`     // xiangqi/chess/junqi/doudizhu/texasholdem/werewolf
    Seat          int        `gorm:"not null"            json:"seat"`
    Role          string     `gorm:"type:varchar(32);not null;default:''"  json:"role"`        // werewolf 里:werewolf/seer/witch/...
    StartedAt     time.Time  `gorm:"index:idx_provider_created,priority:2"  json:"started_at"`
    EndedAt       *time.Time `                              json:"ended_at,omitempty"`
    Result        string     `gorm:"type:varchar(32);not null;default:''"  json:"result"`     // win/lose/draw/abandoned
    CoinDelta     int64      `gorm:"not null;default:0"   json:"coin_delta"`     // 该局金币变化
    LLMCallCount  int        `gorm:"not null;default:0"   json:"llm_call_count"`
    InputTokens   int        `gorm:"not null;default:0"   json:"input_tokens"`
    OutputTokens  int        `gorm:"not null;default:0"   json:"output_tokens"`
    FinalHand     string     `gorm:"type:varchar(255);default:''"  json:"final_hand"`   // 残局/最终手牌摘要
}
```

**索引**：

| 索引 | 列 | 用途 |
|------|----|------|
| `PRIMARY` | `id` | 主键 |
| `idx_provider_created` | `(provider_id, started_at)` | 「按模型看时间序列对局」最热查询 |
| `idx_bot_user` | `bot_user_id` | 单 bot 玩家聚合 |
| `idx_room` | `room_id` | 按房间聚合（7 bot 局 7 行） |
| `idx_game_kind` | `game_kind` | 按游戏类型筛选 |

### 3.4 `t_lsm_game_model_chat_message` — 模型聊天原文

每条 LLM 输出/输入一条记录。**不**复用 `t_lsm_game_chat_message`（那个是公开聊天；这个含
thinking / system / tool_use 完整内容）。

```go
// ServerGo/models/t_lsm_game_model_chat_message.go
type TLsmGameModelChatMessage struct {
    ID         uint64    `gorm:"primaryKey;autoIncrement"  json:"id"`
    GameLogID  string    `gorm:"type:char(36);index:idx_gamelog_seq,priority:1;not null"  json:"game_log_id"`
    BotUserID  string    `gorm:"type:char(36);index;not null"  json:"bot_user_id"`
    ProviderID string    `gorm:"type:char(36);index;not null"  json:"provider_id"`
    RoomID     string    `gorm:"type:char(36);index;not null"  json:"room_id"`
    Seq        int64     `gorm:"not null;index:idx_gamelog_seq,priority:2"  json:"seq"`     // 该 game_log 内的递增序号
    Role       string    `gorm:"type:varchar(16);not null"  json:"role"`        // user/assistant/tool_result/system
    Content    string    `gorm:"type:mediumtext;not null"   json:"content"`     // 完整文本(JSON for tool blocks)
    Phase      string    `gorm:"type:varchar(32);not null;default:''"  json:"phase"`
    ToolName   string    `gorm:"type:varchar(64);not null;default:''"  json:"tool_name"`
    ToolInput  string    `gorm:"type:text;not null"         json:"tool_input"`
    Thinking   string    `gorm:"type:mediumtext;not null"   json:"thinking"`    // 思考块
    StopReason string    `gorm:"type:varchar(32);not null;default:''"  json:"stop_reason"`
    LatencyMs  int       `gorm:"not null;default:0"        json:"latency_ms"`
    CreatedAt  time.Time `gorm:"autoCreateTime"             json:"created_at"`
}
```

**索引**：

| 索引 | 列 | 用途 |
|------|----|------|
| `PRIMARY` | `id` | 主键 auto-increment |
| `idx_gamelog_seq` | `(game_log_id, seq)` | 「按 game_log 拉时序聊天」 |
| `idx_bot_provider` | `(bot_user_id, provider_id)` | 按 bot 玩家聚合 |
| `idx_room` | `room_id` | 房间维度排查 |

### 3.5 `t_lsm_game_model_action` — 模型动作决策记录

每条工具调用/游戏动作一行。**关键决策数据**。

```go
// ServerGo/models/t_lsm_game_model_action.go
type TLsmGameModelAction struct {
    ID           uint64    `gorm:"primaryKey;autoIncrement"  json:"id"`
    GameLogID    string    `gorm:"type:char(36);index:idx_gamelog_phase,priority:1;not null"  json:"game_log_id"`
    BotUserID    string    `gorm:"type:char(36);index;not null"  json:"bot_user_id"`
    Phase        string    `gorm:"type:varchar(32);index:idx_gamelog_phase,priority:2;not null"  json:"phase"`
    ActionType   string    `gorm:"type:varchar(32);index;not null"  json:"action_type"`   // speak/vote/wolf_kill/seer_check/witch_act/...
    ActionTarget string    `gorm:"type:varchar(64);not null;default:''"  json:"action_target"` // 目标座位/牌/玩家
    Payload      string    `gorm:"type:text;not null"         json:"payload"`        // 完整动作 JSON
    Reasoning    string    `gorm:"type:mediumtext;not null"   json:"reasoning"`      // 模型解释/思考
    Accepted     bool      `gorm:"not null;default:true"      json:"accepted"`
    CreatedAt    time.Time `gorm:"autoCreateTime"             json:"created_at"`
}
```

**索引**：

| 索引 | 列 | 用途 |
|------|----|------|
| `PRIMARY` | `id` | 主键 auto-increment |
| `idx_gamelog_phase` | `(game_log_id, phase)` | 单局按时段聚合 |
| `idx_action_type` | `action_type` | 按动作类型筛选 |
| `idx_bot` | `bot_user_id` | 单 bot 玩家聚合 |

### 3.6 `t_lsm_game_kv` — 通用 KV 存储

存 AES-GCM 加密密钥等敏感配置。

```go
// ServerGo/models/t_lsm_game_kv.go
type TLsmGameKV struct {
    Key       string    `gorm:"type:varchar(64);primaryKey"  json:"key"`
    Value     string    `gorm:"type:text;not null"           json:"value"`
    UpdatedAt time.Time `gorm:"autoUpdateTime"               json:"updated_at"`
}
```

**约定 Key**：

| Key | Value | 说明 |
|-----|-------|------|
| `llm_encryption_key` | 32 字节随机 key 的 base64 | AES-GCM 主密钥；首次启动生成；**丢失后所有 provider 需重新 seed** |

### 3.7 复用表（仅引用）

| 表 | 用途 |
|----|------|
| `t_lsm_game_wallet` | 模型玩家钱包（每个 bot 一行，余额默认 1000） |
| `t_lsm_game_wallet_tx` | 模型玩家钱包流水（所有金币变动双簿记） |
| `t_lsm_game_player` | 房间内玩家行（`model_key` 字段关联到 `t_lsm_game_llm_provider.model`） |

---

## 4. 加载策略（DB-first + Config 兜底）

### 4.1 启动加载顺序

`ServerGo/llm/registry.go::NewRegistry(cfg, gormDB)`：

```
1. 若 gormDB != nil：
     SELECT * FROM t_lsm_game_llm_provider ORDER BY created_at ASC
     若 N > 0 → 全部从 DB 读,使用 DecryptAPIKey(APIKeyEnc) 解密
     若 N == 0：
        若 cfg.LLM.Providers 非空 → 调 SeedFromConfig(ctx) 自动写库
        若 cfg.LLM.Providers 也空 → 启动空注册表,日志 WARN
2. 启动日志：'LLM registry loaded N models from {DB|config} (X anthropic, Y openai)'
3. 加解密密钥：从 t_lsm_game_kv['llm_encryption_key'] 读；不存在则生成 32 字节随机 key 并 persist
```

### 4.2 运行期 Reload 触发场景

`Registry.Reload(ctx context.Context) error`：

| 场景 | 触发方 | 是否热生效 |
|------|--------|-----------|
| admin 在 UI 上 POST 新模型 | `api/model_admin_api.go::CreateProvider` | 立即 Reload |
| admin 更新模型（key / endpoint / thinking） | `UpdateProvider` | 立即 Reload |
| admin 软删除模型 | `DeleteProvider` | 立即 Reload + 从新房间下拉中移除 |
| admin 点击「🔄 重新加载」 | `POST /api/admin/llm/providers/reload` | 立即 Reload |
| 测试「发个 dry-run ping」 | `POST /api/admin/llm/providers/:id/test` | 不 Reload，只做 health check |

**Reload 实现要点**：
- 读锁 → 复制 `providers` map → 写锁替换。
- 不影响正在进行中的 `Chat` / `ChatStream` 调用（旧 provider 实例继续工作直至返回）。
- 加新 provider / 改 key 走新实例。

### 4.3 Seed 幂等性

`SeedFromConfig(ctx)`：
- 对 `cfg.LLM.Providers` 中每条记录：按 `model` 查 DB，**已存在则跳过**（不覆盖，避免误覆盖 admin 改过的 key）。
- 写库时 `APIKeyEnc = EncryptAPIKey(plain)`，`APIKeyHint = formatHint(plain)`。
- 完成后 `Reload(ctx)`。

### 4.4 deprecation 警告

启动时若 `cfg.LLM.Providers` 仍非空，log 一条 WARN：

```
WARN: cfg.LLM.Providers is deprecated since v1.0; please use /api/admin/llm/providers
      DB has been auto-seeded with N models.
```

`LsmAgentGame.conf.example` 中 `providers[]` 留空数组 `[]` + 注释「deprecated since v1.0」。

---

## 5. Bot 用户注册

### 5.1 接口

```go
// ServerGo/service/bot_user_service.go
package service

func EnsureBotUserForProvider(ctx context.Context, p *models.TLsmGameLlmProvider) (*models.TLsmGameUser, error)
```

### 5.2 行为规约

| 步骤 | 行为 |
|------|------|
| 1. 查 user | `SELECT * FROM t_lsm_game_user WHERE account = 'bot_<snake_case(model)>'` |
| 2. 不存在则 INSERT | account = `bot_<snake_case(model)>`（例 `bot_doubao_model`）；nickname = `p.AgentName`；password_hash = bcrypt(随机 64 字节) |
| 3. 写 IsBot / BotProviderID | `IsBot=true`、`BotProviderID=&p.ID`、`LinkedProviderAccount=account` |
| 4. 钱包 seed | `WalletService.EnsureWallet(user.ID)` — 已有则不动；没有则创建，balance=1000 |
| 5. 幂等 | 若 account 已存在，仅更新 `BotProviderID / LinkedProviderAccount`；**不覆盖** nickname / password |

### 5.3 调用方

- `Registry.SeedFromConfig()` — 首次启动 seed 8 个模型后批量调用
- `api/model_admin_api.go::CreateProvider` — admin 新建模型后立即调用
- `api/model_admin_api.go::UpdateProvider` — 若 model 改名（极少），重新确保新 bot user（旧的保留历史）

### 5.4 鉴权约束

bot 账号 `password_hash` 是随机 64 字节不可猜，**且** `auth_service.go::Login` 对 `IsBot=true`
的用户必须返回 `ErrBotAccountLoginForbidden = 30020`，防止人类误用 bot 账号登录。

---

## 6. Provider 接口 + Registry

### 6.1 接口（沿用现有 `ServerGo/llm/types/`）

```go
// LLMProvider（无变更）
type LLMProvider interface {
    Chat(ctx context.Context, key string, req LLMRequest) (LLMResponse, error)
    ChatStream(ctx context.Context, key string, req LLMRequest) (io.ReadCloser, error)
    ProviderType() string
}
```

### 6.2 Registry 新签名

```go
// ServerGo/llm/registry.go
type Registry struct {
    mu        sync.RWMutex
    providers map[string]*registeredProvider // key = Model（如 "DouBao-model"）
    crypto    *util.CryptoBox                // AES-GCM 加解密
    gormDB    *gorm.DB                       // DB 访问句柄
    cfg       config.LLMConfig               // 全局 endpoint / timeout / max_retries 兜底
}

func NewRegistry(cfg config.LLMConfig, gormDB *gorm.DB) *Registry
func (r *Registry) Get(modelKey string) (LLMProvider, string, error)      // provider, apiKey, err
func (r *Registry) List() []ModelInfo                                       // 不含 key
func (r *Registry) ListEnabled() []ModelInfo                                // 仅 enabled=true（狼人杀随机分配用）
func (r *Registry) Count() int
func (r *Registry) ThinkingFor(modelKey string) (bool, int)                 // 查 thinking 配置
func (r *Registry) SetUserAgent(ua string)
func (r *Registry) SetBillingHeader(bh string)
func (r *Registry) SetStreamTimeouts(idle, total time.Duration)
func (r *Registry) Reload(ctx context.Context) error                        // 【新】DB-first 重新加载
func (r *Registry) SeedFromConfig(ctx context.Context) (int, error)         // 【新】从 cfg seed 到 DB
```

**关键不变量**：
- `Get()` 拿不到时返回 `ErrModelNotFound`，**不**fallback 到默认。
- `Enabled=false` 的 model 在 `ListEnabled()` 中不出现（狼人杀随机分配用），但 `Get()` 仍可拿到
  （保留历史对局可继续调用 LLM 完成 action）。
- 加密 key 丢失（`t_lsm_game_kv` 没行）时 `Get` 返回 `ErrEncryptionKeyMissing`，所有 model 不可用，
  admin 必须重新 seed。

### 6.3 占位符 key 行为（沿用）

`api_key` 仍为 `API-KEY-PLACEHOLDER` 时，`Available=false`，`Get()` 返回明确错误，**不**发往代理。

---

## 7. HTTP API

### 7.1 Provider 管理 API（`ServerGo/api/model_admin_api.go`）

| 方法 | 路径 | 权限 | 功能 |
|------|------|------|------|
| `GET`    | `/api/admin/llm/providers` | admin | 列出所有模型（`enabled=true/false` 均可，含 hint/endpoint/thinking，**不含 api_key**） |
| `POST`   | `/api/admin/llm/providers` | admin | 新建模型（自动 seed bot_user + wallet；立即 `Reload`） |
| `PUT`    | `/api/admin/llm/providers/:id` | admin | 更新模型（热生效；走 `Reload`） |
| `DELETE` | `/api/admin/llm/providers/:id` | admin | 软删除（`Enabled=false`，不真删；保留 bot 历史；走 `Reload`） |
| `POST`   | `/api/admin/llm/providers/:id/test` | admin | 发一个 dry-run ping（构造最小 `LLMRequest` + 1 token 输出） |
| `POST`   | `/api/admin/llm/providers/reload` | admin | 显式触发 Registry 从 DB 重新加载 |

### 7.2 对局日志 API（`ServerGo/api/model_log_api.go`）

| 方法 | 路径 | 权限 | 功能 |
|------|------|------|------|
| `GET` | `/api/admin/llm/providers/:id/games` | admin | 该模型的所有对局（分页 `?page=1&size=20` + 时间筛选 `?from=&to=`） |
| `GET` | `/api/admin/llm/games/:gameLogID` | admin | 单局详情（参与 chat / action 聚合） |
| `GET` | `/api/admin/llm/games/:gameLogID/messages` | admin | 该局完整聊天 / 思考 / 动作时间线（`?from_seq=&limit=200`） |
| `GET` | `/api/admin/llm/bots/:botUserID/wallet` | admin | bot 钱包余额 + 最近 20 条流水 |

### 7.3 钱包调整 API（`ServerGo/api/model_wallet_api.go`）

| 方法 | 路径 | 权限 | 功能 |
|------|------|------|------|
| `POST` | `/api/admin/llm/bots/:botUserID/wallet/adjust` | **super** | 管理员手动加减金币（写 ledger `tx_type=admin_adjust`） |

### 7.4 通用鉴权与错误码

| 错误码 | 含义 |
|--------|------|
| `30010` | `ErrProviderNotFound` — model_id 不存在 |
| `30011` | `ErrProviderAgentNameTaken` — agent_name 重复 |
| `30012` | `ErrProviderModelKeyTaken` — model 字段重复 |
| `30013` | `ErrProviderKeyInvalid` — 占位符或解密失败 |
| `30014` | `ErrProviderEncryptionKeyMissing` — t_lsm_game_kv 无 llm_encryption_key |
| `30015` | `ErrProviderTestFailed` — `/test` 端点 dry-run 失败 |
| `30020` | `ErrBotAccountLoginForbidden` — 人类尝试登录 bot 账号 |
| `30021` | `ErrBotWalletAdjustForbidden` — admin 权限不足（需 super） |

**鉴权硬约束**：
- `admin` 角色可看 + 改模型元数据、查日志、看钱包流水。
- `super` 角色才可调 `/wallet/adjust`（防止 admin 误改 bot 余额）。
- 钱包调整**必须**走 `WalletService.ApplyTransaction`（ledger 双簿记），**禁止**直接 UPDATE balance。

---

## 8. 前端 UI

### 8.1 入口与路由

- 菜单入口：`ClientWeb/src/components/layout/AppSidebar.tsx` 在 `nav.adminUsers` 之前插入
  `{ to: '/admin/models', labelKey: 'nav.adminModels', icon: '🤖', minUserType: 2 }`
- i18n 新增 key：`nav.adminModels = 模型管理`（zh-CN / en / ja 三语）
- 路由：
  - `/admin/models` → `<ModelAdminPage />`
  - `/admin/models/:providerId` → `<ModelDetailPage />`
  - `/admin/models/:providerId/games/:gameLogId` → `<ModelGameLogPage />`

### 8.2 `ModelAdminPage.tsx` — 模型列表

- 表格列：`AgentName` / `Model` / `ProviderType` / `APIKeyHint` / `Enabled` / `UpdatedAt` / 操作
- 操作列按钮：编辑 / 测活（POST `/test`） / 软删 / 重新加载（POST `/reload`）
- 顶部「+ 新增模型」按钮 → 弹窗表单：
  - `AgentName`（必填，唯一）
  - `Model`（必填，唯一）
  - `ProviderType`（下拉，anthropic / openai）
  - `APIKey`（必填，明文 → 后端 AES-GCM 加密入库）
  - `Endpoint`（可选，留空用全局）
  - `ThinkingRequired`（勾选） + `ThinkingBudget`（数字）
  - `Remark`（可选）

### 8.3 `ModelDetailPage.tsx` — 模型详情

- **顶部信息卡**：表单字段只读（点击「编辑」切到编辑态）
- **中部「💰 钱包」区块**：
  - 余额大字（来自 `GET /wallet`）
  - 最近 20 条流水（time / type / amount / balance_after / remark）
- **底部「🎮 对局历史」表格**：
  - 列：`GameKind` / `StartedAt` / `Result` / `CoinDelta` / `LLMCallCount` / 操作
  - 操作列：查看详情（跳 `ModelGameLogPage`）

### 8.4 `ModelGameLogPage.tsx` — 单局时间线

- 顶部游戏信息条：Bot 昵称 / Model / 房间号 / 起止时间 / Result / CoinDelta
- 时间线视图：左侧 phase，右侧该 phase 内 chat_message + action 双列表
  - 聊天卡：`role=user/assistant`，`thinking` 折叠展开，`tool_use` 块用彩色边框
  - 动作卡：`ActionType` + `Target` + `Reasoning`（折叠显示完整 LLM 解释）
- 顶部 filter 开关：
  - 「仅显示工具调用」—— 隐藏 `Role=user/system`
  - 「仅显示思考块」—— 隐藏非 thinking 行
  - 「仅显示动作」—— 切换到 `t_lsm_game_model_action` 单列表
- 默认按 `Seq` 升序，可跳到指定 seq

### 8.5 通用约束

- 复用 `useAuthStore` / `useUiStore` 已有模式
- i18n key 全部走 `useT()`，加到 `ClientWeb/src/i18n/zh-CN.ts` + `en.ts` + `ja.ts`
- 样式：复用现有 `app-page / app-card / table` 等通用 CSS（参考 `AdminUsersPage.tsx`）
- 提交前 `tsc --noEmit` + `npm run build` 必须通过

---

## 9. 模型玩家与人类玩家的关系

### 9.1 一对一绑定

| 模型 | Bot User |
|------|----------|
| `t_lsm_game_llm_provider` 一行 | `t_lsm_game_user` 一行（`IsBot=true`） |
| `Model` 字段（如 `DouBao-model`） | `account` 字段（如 `bot_doubao_model`） |
| `BotProviderID` 外键 | 反向指针（可选冗余） |

**绑定保证**：
- 一个 model ↔ 一个 bot user（一对一）；同一个 model 多次启停不会创建多个 bot user（§5.2 幂等）。
- 删除 model（`Enabled=false`）**不**删除 bot user；历史对局日志、钱包流水完整保留。

### 9.2 与房间的关系

`ServerGo/models/t_lsm_game_player.go::TLsmGamePlayer.ModelKey` 字段是房间内绑定的证据：

```
模型在房间内参与对局
  → t_lsm_game_player 写入一行 (room_id, seat, model_key="DouBao-model")
  → t_lsm_game_model_game_log 写入一条主记录 (provider_id, bot_user_id, room_id, game_kind, seat)
  → t_lsm_game_model_chat_message 累计 N 条原文
  → t_lsm_game_model_action 累计 M 条决策
```

`model_key` 是核心关联键：
- 前端选择「让哪些模型参与房间」时调 `GET /api/admin/llm/providers?enabled=true` 拉可用列表
- 后端在 `POST /api/games/werewolf/rooms` 携带 `agent_seats=[modelKey1, ...]` 时：
  1. 按 `model_key` 查 `t_lsm_game_llm_provider.enabled=true`
  2. 按 `BotProviderID` 查 `t_lsm_game_user` 拿 bot user
  3. 走 `t_lsm_game_player` 插入
  4. 启动 in-process Agent goroutine（`ServerGo/agent/`）

### 9.3 与 5 款游戏的关系

| 游戏 | 现状 | 模型接入 | 备注 |
|------|------|---------|------|
| 象棋 / 国际象棋 / 军棋 / 斗地主 / 德州 | 有人类房间 | **本期不接入** | 无 bot 玩家，agent 模块暂未对接 |
| 狼人杀 | 7-bot 房间 | **本期接入** | 走 `ServerGo/agent/`，对接 5 款对局日志表 |

后续 5 款棋牌接入时，每款独立 `tx_type` 标识（`xiangqi_game` / `chess_game` / …）；
本期仅 `model_game` 一种 `tx_type`。

---

## 10. 测试策略

### 10.1 单元测试

`ServerGo/llm/registry_db_test.go`：

| 测试 | 验证点 |
|------|--------|
| `TestRegistry_LoadFromDB` | DB 有 N 行 → Registry 加载 N 条；key 正确解密 |
| `TestRegistry_SeedFromConfig_OnEmptyDB` | DB 空 + cfg 有 8 条 → 写库后正好 8 条；二次启动不再 seed |
| `TestRegistry_ReloadAfterCRUD` | 调 Reload → 新增/更新的 model 立即在 `ListEnabled()` 中可见 |
| `TestRegistry_PlaceholderKey` | `api_key=API-KEY-PLACEHOLDER` → `Get()` 返回 `ErrProviderKeyInvalid` |
| `TestRegistry_EncryptionKeyMissing` | 删 `t_lsm_game_kv['llm_encryption_key']` → `Get()` 返回 `ErrEncryptionKeyMissing` |

### 10.2 集成测试

`ServerGo/service/bot_user_service_test.go`：

| 测试 | 验证点 |
|------|--------|
| `TestEnsureBotUserForProvider_New` | 调一次 → user / wallet 各新增 1 行，余额 1000 |
| `TestEnsureBotUserForProvider_Idempotent` | 调两次 → 仍 1 个 user，BotProviderID 一致 |
| `TestBotUser_CannotLoginAsHuman` | `POST /api/auth/login` with bot account → 30020 |

`ServerGo/agent/record_log_test.go`：

| 测试 | 验证点 |
|------|--------|
| `TestRecordGameAction_PersistsToDB` | 调 `DispatchTool('speak', ...)` → `t_lsm_game_model_action` 有 1 行 |
| `TestRecordChatMessage_PersistsToDB` | 调 `Chat` mock → `t_lsm_game_model_chat_message` 有完整原文（含 thinking） |
| `TestRecordLog_FailureDoesNotBlockGame` | DB 写失败（mock error）→ 游戏流程不 panic，仅 log |

`ServerGo/game/werewolf/model_coin_settle_test.go`：

| 测试 | 验证点 |
|------|--------|
| `TestGameOver_BotCoinSettlement` | 7-bot 局结束 → 7 个 wallet delta 正确（胜方+100，败方-100），ledger 双写 |
| `TestGameOver_AbandonedNoChange` | 房间异常中断 → 写 ledger `tx_type=game_abandon_no_change`，amount=0 |

### 10.3 端到端烟测

参考 `docs/狼人杀-道具与经济/模型管理与持久化玩家设计.md §8 验证流程`。

---

## 11. 安全

### 11.1 API Key 加密

- **存储**：`t_lsm_game_llm_provider.api_key_enc` 是 AES-GCM(plaintext) 后的 base64 密文；
  `json:"-"` 永不出现在 JSON 响应中。
- **密钥**：32 字节随机 key，base64 存在 `t_lsm_game_kv['llm_encryption_key']`。
  首次启动自动生成；**手工丢失后所有 provider 需重新 seed**（无任何回退路径）。
- **接口**：`ServerGo/util/crypto.go::EncryptAPIKey(plain)` / `DecryptAPIKey(enc)`，单例 CryptoBox。
- **日志**：明文 API Key **永不**写入日志（linter 规则）。

### 11.2 鉴权

| 操作 | 权限 |
|------|------|
| 查看模型列表 | admin |
| 新建/更新/软删模型 | admin |
| `/test` / `/reload` | admin |
| 查看对局日志 | admin |
| 查看钱包 + 流水 | admin |
| `/wallet/adjust` | **super**（admin 也不可） |

### 11.3 密码处理

- bot 用户的 `password_hash` 是 bcrypt(随机 64 字节)：
  - 不允许人类用 bot 账号登录（`ErrBotAccountLoginForbidden = 30020`）。
  - 明文密码永不返回给前端；前端展示 `api_key_hint` 即可。
- admin 创建模型时输入的 API Key 走 HTTPS POST → 后端立即加密入库，**不**在 URL / 日志 / 响应中留存。

### 11.4 软删除而非物理删除

- `DELETE /api/admin/llm/providers/:id` 仅置 `Enabled=false`：
  - bot user 保留（`t_lsm_game_user` 行不删）
  - 钱包保留（`t_lsm_game_wallet` 行不删）
  - 历史对局日志保留（`t_lsm_game_model_game_log` 等 3 张表不删）
  - 后续 admin 想恢复只需 `PUT /:id { enabled: true }` 即可

### 11.5 钱包流水不可篡改

- 所有金币变动走 `WalletService.ApplyTransaction`：
  - 自动写 `t_lsm_game_wallet_tx`（双簿记 + 幂等锁）
  - `BalanceAfter` 校验必须与 ApplyTransaction 计算结果一致
  - `tx_type` 枚举：`model_game` / `game_abandon_no_change` / `admin_adjust` /
    `register_reward` / `login_reward` 等

### 11.6 LLM 调用观测

- 每次 `Chat` / `ChatStream` 记 zap 日志：provider_type / model / input_tokens /
  output_tokens / stop_reason / error / latency_ms
- `t_lsm_game_model_chat_message` 持久化**所有**请求体与响应体（脱敏 agent_name 即可），
  便于审计与 prompt 调优

---

> **文档维护说明**：本文件是「大模型调用模块」的总规约。任何 Provider 协议扩展（OpenAI 接入）、
> DB 字段调整、UI 交互变更、bot 玩家注册逻辑变动，都需要同步更新本文件 + 对应子文档。
