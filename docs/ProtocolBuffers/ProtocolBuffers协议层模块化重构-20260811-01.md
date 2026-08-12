# Protocol Buffers 协议层模块化重构方案

> **版本**：v1.0  
> **日期**：2026-08-11  
> **代号**：§20260811-01  
> **范围**：全栈 WebSocket 协议层 + Proto 契约 + 代码生成工具链

---

## 一、现状分析

### 1.1 核心事实

| 维度 | 现状 |
|------|------|
| **WS 帧格式** | 100% JSON（外层 Envelope JSON + 内层 Payload JSON） |
| **Proto 文件** | 5 个 .proto 文件，**仅作文档**，从未编译 |
| **Proto 覆盖率** | ~10%（只覆盖象棋基础操作、基础聊天、房间、用户） |
| **Go 生成代码** | 无（`google.golang.org/protobuf` 仅 indirect 依赖） |
| **前端 Protobuf** | 零依赖、零生成代码 |
| **工具链** | 无 protoc、无 protoc-gen-go、无 @protobuf-ts/plugin |
| **WS 消息类型** | C→S 约 41 种，S→C 约 50+ 种 |
| **游戏数量** | 6 款（xiangqi / chess / junqi / doudizhu / texasholdem / werewolf） |

### 1.2 现有 Proto 文件与实际差距

| 模块 | Proto 定义 | 实际实现 | 对齐度 |
|------|-----------|---------|--------|
| common (Envelope) | Envelope + Error + Heartbeat + Ack | Envelope + Error（map 构造） | 70% |
| chat | 8 种消息 | 11+ 种（缺 whisper/activity/stream_*） | 50% |
| game | 仅象棋 9 种 | 6 款游戏 30+ 种 | ~10% |
| room | 6 种 | 8 种（缺 state/player_status 推送） | 60% |
| user | 5 种 | 8 种（缺 batch_delete/revoke_super/分页） | 30% |
| wallet | 0 种 | 1 种（balance 推送） | 0% |

### 1.3 存在的问题

1. **Proto 契约形同虚设** — 定义与实现严重脱节，失去契约价值
2. **消息定义分散** — Go 端大量匿名 struct，TS 端分散在各 types/*.ts
3. **无运行时类型校验** — JSON + `as unknown as X` 断言，字段缺失静默失败
4. **序列化效率低** — 狼人杀 `game.state` 帧体量大（10KB+），JSON 解析开销高
5. **无代码生成** — 前后端类型需手写同步，易漂移
6. **工具链缺失** — 没有统一的 proto 编译流程

---

## 二、重构目标与原则

### 2.1 总体目标

建立完整的 **Protocol Buffers 驱动的 WS 协议层**，实现：

1. **契约优先** — `.proto` 文件是唯一事实来源，前后端代码均由 proto 生成
2. **模块化组织** — 按功能模块 + 游戏划分 proto 文件目录
3. **双协议兼容** — JSON 和 protobuf 二进制并行，渐进迁移
4. **类型安全** — 前端 TS 类型 + 后端 Go struct 均由 proto 生成，零漂移
5. **工具链自动化** — 一条命令生成所有语言的代码

### 2.2 设计原则

| 原则 | 说明 |
|------|------|
| **向后兼容第一** | 现有 JSON WS 协议不破坏，新 proto 协议并行运行 |
| **渐进式迁移** | 按模块逐个迁移，不搞"大爆炸"式重构 |
| **契约驱动** | 先写 proto，再生代码，再写业务逻辑 |
| **字段编号永不复用** | 遵循 proto3 消息演进规则，reserved 标记废弃字段 |
| **snake_case 统一** | 所有 proto 字段使用 snake_case，生成代码自动转换 |
| **游戏隔离** | 每款游戏独立 proto 包，互不引用 |
| **共享模块收敛** | 通用类型放在 common / shared 包，禁止跨游戏依赖 |

### 2.3 收益预期

| 收益 | 量化预期 |
|------|---------|
| **消息体积减小** | 30%~60%（vs JSON，取决于嵌套深度和数字字段比例） |
| **序列化速度** | 2~5x 提升（vs JSON） |
| **类型安全** | 前后端零漂移，编译期校验 |
| **契约清晰度** | proto 文件 = API 文档，自动生成可读 |
| **多语言扩展** | 新增客户端（iOS/Android/Unity）只需生成对应语言代码 |

---

## 三、Proto 文件模块化设计

### 3.1 目录结构

```
proto/
├── gen.sh                      # 统一编译脚本（Go + TS）
├── buf.yaml                    # buf 配置（lint + breaking change 检测）
├── common/
│   ├── envelope.proto          # WS 帧信封
│   ├── error.proto             # 通用错误
│   ├── heartbeat.proto         # 心跳
│   └── types.proto             # 通用类型（Position、PlayerBase 等）
├── chat/
│   ├── chat.proto              # 聊天订阅/发送/历史
│   ├── whisper.proto           # 私聊/耳语
│   ├── activity.proto          # 游戏活动事件
│   └── stream.proto            # 流式发言
├── room/
│   ├── room.proto              # 房间管理（list/create/join/leave）
│   └── events.proto            # 房间事件推送（state/player_status）
├── user/
│   ├── user.proto              # 用户列表/删除/权限
│   └── events.proto            # 用户事件（deleted/role_changed）
├── wallet/
│   └── wallet.proto            # 钱包余额推送
└── game/
    ├── common.proto            # 游戏通用帧（join/leave/state/over/error）
    ├── spectator.proto         # 观战通用帧（spectate/unspectate）
    ├── xiangqi/
    │   └── xiangqi.proto       # 象棋专属消息
    ├── chess/
    │   └── chess.proto         # 国际象棋专属消息
    ├── junqi/
    │   └── junqi.proto         # 军棋专属消息
    ├── doudizhu/
    │   └── doudizhu.proto      # 斗地主专属消息
    ├── texasholdem/
    │   └── texasholdem.proto   # 德州扑克专属消息
    └── werewolf/
        ├── werewolf.proto      # 狼人杀通用帧
        ├── actions.proto       # 夜间动作（wolf/seer/witch/guard/knight/demon_hunter）
        ├── vote.proto          # 投票/警长/公投
        ├── speak.proto         # 发言/遗言/警徽流
        ├── roles.proto         # 角色枚举、阵营枚举
        ├── state.proto         # ClientGameState 大状态
        ├── prop.proto          # 道具系统
        └── restart.proto       # 重开局投票
```

### 3.2 包命名规范

```
lsm.common        — 通用类型
lsm.chat          — 聊天
lsm.room          — 房间
lsm.user          — 用户
lsm.wallet        — 钱包
lsm.game.common   — 游戏通用
lsm.game.xiangqi  — 象棋
lsm.game.chess    — 国际象棋
lsm.game.junqi    — 军棋
lsm.game.doudizhu — 斗地主
lsm.game.texas    — 德州扑克
lsm.game.werewolf — 狼人杀
```

### 3.3 Go 包映射

| Proto 包 | Go 包路径 | 说明 |
|----------|----------|------|
| lsm.common | `LsmWebGame/proto/pb/common` | 通用类型 |
| lsm.chat | `LsmWebGame/proto/pb/chat` | 聊天 |
| lsm.room | `LsmWebGame/proto/pb/room` | 房间 |
| lsm.user | `LsmWebGame/proto/pb/user` | 用户 |
| lsm.wallet | `LsmWebGame/proto/pb/wallet` | 钱包 |
| lsm.game.common | `LsmWebGame/proto/pb/game/common` | 游戏通用 |
| lsm.game.xiangqi | `LsmWebGame/proto/pb/game/xiangqi` | 象棋 |
| lsm.game.chess | `LsmWebGame/proto/pb/game/chess` | 国际象棋 |
| lsm.game.junqi | `LsmWebGame/proto/pb/game/junqi` | 军棋 |
| lsm.game.doudizhu | `LsmWebGame/proto/pb/game/doudizhu` | 斗地主 |
| lsm.game.texas | `LsmWebGame/proto/pb/game/texas` | 德州扑克 |
| lsm.game.werewolf | `LsmWebGame/proto/pb/game/werewolf` | 狼人杀 |

### 3.4 TS 命名空间映射

```typescript
// ClientWeb/src/proto/
//   common.ts    — namespace lsm.common
//   chat.ts      — namespace lsm.chat
//   room.ts      — namespace lsm.room
//   user.ts      — namespace lsm.user
//   wallet.ts    — namespace lsm.wallet
//   game/
//     common.ts  — namespace lsm.game.common
//     xiangqi.ts — ...
```

---

## 四、Envelope 帧格式升级

### 4.1 双协议并行策略

**关键设计决策**：不立即切换到纯二进制，而是采用 **JSON + Proto 双轨并行**，逐步迁移。

```
┌─────────────────────────────────────────────────┐
│              WebSocket 连接                       │
│                                                   │
│  ┌──────────┐      ┌──────────────────────┐      │
│  │ JSON 帧  │◄────►│  LegacyMessageRouter │      │
│  │ (现有)   │      │  (保留不动)           │      │
│  └──────────┘      └──────────────────────┘      │
│                                                   │
│  ┌──────────┐      ┌──────────────────────┐      │
│  │ Proto 帧 │◄────►│  ProtoMessageRouter  │      │
│  │ (新增)   │      │  (新增)               │      │
│  └──────────┘      └──────────────────────┘      │
│                                                   │
│  自动识别：首字节 0x00 = Proto binary             │
│            首字节 {    = JSON text               │
└─────────────────────────────────────────────────┘
```

### 4.2 自动识别机制

- **JSON 帧**：`websocket.TextMessage`，首字符为 `{`
- **Proto 二进制帧**：`websocket.BinaryMessage`，首字节为 tag (0x08 = field 1 varint)

Go 端 `ReadPump` 根据消息类型（Text vs Binary）自动分发到不同路由。

### 4.3 Proto Envelope 定义

```protobuf
// proto/common/envelope.proto
syntax = "proto3";

package lsm.common;

option go_package = "LsmWebGame/proto/pb/common";

// WebSocket 帧信封 —— 所有消息的统一容器
message Envelope {
  // 消息类型字符串（与现有 JSON 帧的 type 字段一致，便于平滑迁移）
  // 格式：<module>.<action>，如 "game.werewolf_vote"、"chat.send"
  string type = 1;

  // 请求序号（客户端递增，服务端回显，用于请求-响应匹配）
  int64 seq = 2;

  // 消息载荷（protobuf 二进制字节，具体结构由 type 决定）
  bytes payload = 3;
}
```

### 4.4 消息类型命名规范

与现有 JSON 帧完全一致（保持迁移期查找表一致）：

| 模块 | 前缀 | 示例 |
|------|------|------|
| 聊天 | `chat.` | `chat.send`, `chat.message`, `chat.activity` |
| 房间 | `room.` | `room.join`, `room.state` |
| 用户 | `user.` | `user.list`, `user.deleted` |
| 钱包 | `wallet.` | `wallet.balance` |
| 通用游戏 | `game.` | `game.join`, `game.state`, `game.over` |
| 狼人杀 | `game.werewolf_` | `game.werewolf_vote`, `game.werewolf_action` |

---

## 五、后端（Go）重构设计

### 5.1 架构分层

```
┌──────────────────────────────────────────────────┐
│               ws 包（改造后）                      │
├──────────────────────────────────────────────────┤
│                                                  │
│  handler.go    — WS 升级入口（不变）              │
│  client.go     — ReadPump/WritePump               │
│                  ├─ TextMessage → legacy 路径     │
│                  └─ BinaryMessage → proto 路径    │
│                                                  │
│  hub.go        — Hub + Envelope 定义              │
│                  ├─ Envelope（JSON，保留）        │
│                  └─ ProtoEnvelope（新增）         │
│                                                  │
│  proto_router.go — Proto 消息路由器（新增）       │
│                  ├─ 注册所有 proto 消息 handler   │
│                  └─ 按 type 分发到对应 handler    │
│                                                  │
│  proto_codecs.go — 编解码工具（新增）             │
│                  ├─ MarshalEnvelope               │
│                  ├─ UnmarshalEnvelope             │
│                  └─ 消息类型注册表                 │
│                                                  │
│  chat/room/user/game_service*.go                 │
│    （逻辑不变，仅新增 proto 变体入口）             │
│                                                  │
└──────────────────────────────────────────────────┘
```

### 5.2 消息注册表模式

```go
// proto_codecs.go
type ProtoHandler func(c *Client, env *commonpb.Envelope, payload proto.Message)

type ProtoRegistry struct {
    handlers map[string]proto.Message  // type → message instance (for unmarshal)
    handlersFn map[string]ProtoHandler // type → handler function
}

func (r *ProtoRegistry) Register(msgType string, factory func() proto.Message, handler ProtoHandler)
func (r *ProtoRegistry) UnmarshalPayload(msgType string, data []byte) (proto.Message, error)
func (r *ProtoRegistry) Dispatch(c *Client, env *commonpb.Envelope) error
```

### 5.3 Hub 广播改造

现有 `BroadcastRoom` / `BroadcastTo` 等方法签名不变，内部增加 proto 变体：

```go
// 保留（JSON 通道）
func (h *Hub) BroadcastRoom(roomID string, env Envelope)

// 新增（Proto 通道）
func (h *Hub) BroadcastRoomProto(roomID string, env *commonpb.Envelope)
```

**迁移策略**：广播端先双写（同时发 JSON 和 Proto），客户端根据能力接收。
待所有客户端迁移完成后，关闭 JSON 通道。

### 5.4 游戏状态（ClientGameState）迁移

各游戏的 `ClientGameState` struct 是最大的消息体，也是 protobuf 收益最大的地方：

| 游戏 | 预估大小 (JSON) | Proto 预估 | 压缩比 |
|------|----------------|-----------|--------|
| 象棋 | ~1KB | ~400B | 60% |
| 斗地主 | ~2KB | ~800B | 60% |
| 德州扑克 | ~3KB | ~1.2KB | 60% |
| 狼人杀 | ~10KB~30KB | ~4KB~12KB | 55%~65% |

**迁移优先级**：狼人杀 > 德州扑克 > 斗地主 > 军棋 > 象棋 > 国际象棋

### 5.5 改造范围（按文件）

| 文件 | 改动量 | 改动类型 |
|------|--------|---------|
| `ws/client.go` | +40 行 | ReadPump 增加 BinaryMessage 分支 |
| `ws/hub.go` | +80 行 | 增加 Proto 广播方法 |
| `ws/proto_router.go` | +150 行 | 新增：Proto 消息注册与分发 |
| `ws/proto_codecs.go` | +100 行 | 新增：编解码工具 + 注册表 |
| `ws/chat_service.go` | +50 行 | 新增 proto 变体入口 |
| `ws/game_service.go` | +60 行 | 新增通用游戏 proto handler |
| `ws/game_service_werewolf.go` | +100 行 | 新增狼人杀 proto handler |
| `ws/game_service_*.go`（其他5款） | +30 行/文件 | 各游戏新增 proto handler |
| `ws/user_service.go` | +30 行 | 新增 proto 变体入口 |
| `ws/room_service.go` | +30 行 | 新增 proto 变体入口 |
| `ws/wallet_service.go` | +15 行 | 新增 proto 变体入口 |

**后端新增/改动约 15 个文件，~800 行**

---

## 六、前端（TS）重构设计

### 6.1 工具链选择

**推荐方案：@protobuf-ts/runtime + @protobuf-ts/plugin**

| 方案 | 优点 | 缺点 |
|------|------|------|
| **@protobuf-ts** | TS 原生、tree-shakable、支持 proto3 optionals、bundle 小 | 社区相对小 |
| protobufjs | 生态成熟、使用广泛 | 体积较大、TS 支持一般 |
| google-protobuf | 官方出品 | 体积大、TS 支持差 |
| ts-proto | 生成强类型代码 | 依赖 protoc |

选择 `@protobuf-ts` 的理由：
- 纯 TS 运行时，无额外依赖
- 生成的代码天然支持 TypeScript
- 支持 `proto3 optional`
- 体积小（runtime ~10KB gzip）
- 支持 `BinaryWriter` / `BinaryReader` 直接操作

### 6.2 前端架构改造

```
ClientWeb/src/
├── proto/                      # 生成的 proto 代码（gitignore）
│   ├── common.ts
│   ├── chat.ts
│   ├── room.ts
│   ├── user.ts
│   ├── wallet.ts
│   └── game/
│       ├── common.ts
│       ├── xiangqi.ts
│       ├── chess.ts
│       ├── junqi.ts
│       ├── doudizhu.ts
│       ├── texasholdem.ts
│       └── werewolf.ts
├── services/
│   ├── ws.ts                   # 改造：支持 BinaryMessage
│   │   ├─ WsEnvelope 类型保留（JSON 兼容）
│   │   ├─ sendJson()  → 发送 JSON 帧
│   │   ├─ sendProto() → 发送 Proto 二进制帧
│   │   ├─ onJson()    → 订阅 JSON 帧
│   │   └─ onProto()   → 订阅 Proto 二进制帧
│   └── protoRegistry.ts        # 新增：前端消息类型注册表
├── hooks/
│   └── useWerewolf.ts 等       # 逐步迁移到 proto 类型
└── types/
    └── werewolf.ts 等          # 逐步由 proto 生成替代
```

### 6.3 双协议客户端策略

```typescript
// ws.ts 改造点
class WsClient {
  // 现有 JSON 通道（保留）
  send(type: string, payload?: unknown, seq?: number): void
  on(type: string, handler: Listener): () => void

  // 新增 Proto 通道
  sendProto<T extends Message<T>>(type: string, message: T, seq?: number): void
  onProto<T extends Message<T>>(type: string, cls: MessageType<T>, handler: (msg: T, env: ProtoEnvelope) => void): () => void

  // 协议协商
  private negotiatedProto: boolean = false
  private negotiateProto(): void  // 连接建立后发送 proto_capability 帧
}
```

**协商流程**：
1. 客户端建立 WS 连接
2. 客户端发送 `system.proto_capability` 帧（JSON）声明支持 proto
3. 服务端回 `system.proto_ack` 确认
4. 之后双方使用 BinaryMessage 发送 proto 帧
5. 若服务端不支持，客户端降级到 JSON（向后兼容）

### 6.4 类型迁移策略

**目标**：types/*.ts 中的 WS 消息类型逐步由 proto 生成替代。

**迁移顺序**：
1. **Phase 1**：proto 类型与现有 TS 类型并行，运行时可选
2. **Phase 2**：新功能直接用 proto 类型，旧功能逐步迁移
3. **Phase 3**：所有 WS 消息使用 proto 类型，删除手写类型

---

## 七、工具链建设

### 7.1 安装依赖

**后端（Go）**：
```bash
# protoc 编译器
apt install -y protobuf-compiler

# Go 插件
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
```

**前端（TS）**：
```bash
cd ClientWeb
npm install --save @protobuf-ts/runtime
npm install --save-dev @protobuf-ts/plugin
```

### 7.2 编译脚本（proto/gen.sh）

```bash
#!/bin/bash
# Protocol Buffers 代码生成脚本
# 用法：./proto/gen.sh

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROTO_DIR="$ROOT/proto"
GO_OUT="$ROOT/ServerGo/proto/pb"
TS_OUT="$ROOT/ClientWeb/src/proto"

# 清理旧生成文件
rm -rf "$GO_OUT" "$TS_OUT"
mkdir -p "$GO_OUT" "$TS_OUT"

# 生成 Go 代码
protoc \
  --proto_path="$PROTO_DIR" \
  --go_out="$GO_OUT" \
  --go_opt=paths=source_relative \
  $(find "$PROTO_DIR" -name "*.proto")

# 生成 TS 代码
npx protoc \
  --proto_path="$PROTO_DIR" \
  --ts_out="$TS_OUT" \
  --ts_opt=long_type_number \
  --ts_opt=optimize_code_size \
  --ts_opt=ts_nocheck \
  $(find "$PROTO_DIR" -name "*.proto")

echo "✅ Protocol Buffers 代码生成完成"
echo "   Go:  $GO_OUT"
echo "   TS:  $TS_OUT"
```

### 7.3 Buf 配置（可选）

使用 `buf` 工具做 lint 和 breaking change 检测：

```yaml
# proto/buf.yaml
version: v1
breaking:
  use:
    - FILE
lint:
  use:
    - DEFAULT
```

### 7.4 Makefile 集成

```makefile
# Makefile
proto:
	cd proto && ./gen.sh

proto-lint:
	cd proto && buf lint

proto-breaking:
	cd proto && buf breaking --against '.git#branch=main'
```

---

## 八、分阶段实施计划

### Phase 0：基础设施搭建（本阶段）

**目标**：建立工具链 + 基础 proto 定义 + 生成脚本

**交付物**：
- [ ] protoc + protoc-gen-go + @protobuf-ts/plugin 安装
- [ ] `proto/` 目录结构重构
- [ ] `proto/gen.sh` 编译脚本
- [ ] `proto/common/envelope.proto` + 基础类型
- [ ] Go 端 `proto/pb/common` 生成代码验证
- [ ] TS 端 `src/proto/common` 生成代码验证
- [ ] `docs/ProtocolBuffers/protobuf-dev-官方技术栈指南.md`（已完成）
- [ ] 本文档（重构方案）

**预估工作量**：2~3 小时

### Phase 1：核心框架 + 聊天模块

**目标**：打通 proto 端到端通路，用最简单的模块验证全链路

**交付物**：
- [ ] 后端 `ProtoRegistry` + `proto_router.go`
- [ ] 后端 `client.go` BinaryMessage 分发
- [ ] 后端 `hub.go` Proto 广播方法
- [ ] 前端 `ws.ts` BinaryMessage 支持
- [ ] 前端 `protoRegistry.ts` 消息注册表
- [ ] proto 文件：chat 模块全量定义
- [ ] 聊天模块 JSON + Proto 双写验证
- [ ] 协议协商机制（system.proto_capability）

**预估工作量**：6~8 小时

### Phase 2：房间 + 用户 + 钱包模块

**目标**：迁移公共模块，验证 proto 注册表模式可扩展

**交付物**：
- [ ] proto 文件：room 模块全量
- [ ] proto 文件：user 模块全量
- [ ] proto 文件：wallet 模块
- [ ] 后端三个模块的 proto handler
- [ ] 前端三个模块的 proto 订阅
- [ ] 双协议回归测试

**预估工作量**：4~6 小时

### Phase 3：游戏通用 + 观战系统

**目标**：迁移游戏通用帧（join/leave/state/over）+ 观战

**交付物**：
- [ ] proto 文件：game/common.proto + spectator.proto
- [ ] 后端通用游戏 proto handler
- [ ] 前端通用游戏 hook 迁移
- [ ] 观战系统 proto 迁移
- [ ] 6 款游戏 join/leave/state 全双写

**预估工作量**：6~8 小时

### Phase 4：棋类游戏（xiangqi + chess + junqi）

**目标**：迁移三款棋类的专属帧

**交付物**：
- [ ] proto 文件：xiangqi/chess/junqi 各 1 个
- [ ] 后端三款游戏的 proto handler
- [ ] 前端三款游戏的 hook 迁移
- [ ] 双协议回归测试

**预估工作量**：4~6 小时

### Phase 5：卡牌游戏（doudizhu + texasholdem）

**目标**：迁移两款卡牌游戏

**交付物**：
- [ ] proto 文件：doudizhu + texasholdem
- [ ] 后端两款游戏的 proto handler
- [ ] 前端两款游戏的 hook 迁移
- [ ] 双协议回归测试

**预估工作量**：4~6 小时

### Phase 6：狼人杀（最大模块）

**目标**：迁移狼人杀全部 WS 消息

**交付物**：
- [ ] proto 文件：werewolf 6 个文件（actions/vote/speak/roles/state/prop/restart）
- [ ] 后端狼人杀 proto handler（19 种 C→S + 8 种 S→C 专属帧）
- [ ] 前端 useWerewolf hook 迁移
- [ ] ClientGameState proto 定义 + 生成
- [ ] 道具系统 proto 迁移
- [ ] 流式发言 proto 迁移
- [ ] 双协议回归测试

**预估工作量**：12~16 小时

### Phase 7：清理 JSON 通道（远期）

**目标**：确认所有客户端已迁移后，移除 JSON 通道

**交付物**：
- [ ] 移除 hub JSON 广播方法
- [ ] 移除 JSON handler
- [ ] 移除旧类型定义
- [ ] 移除 proto 文件中的 JSON 注释
- [ ] 性能对比报告

**预估工作量**：4~6 小时

### 总工作量预估

| 阶段 | 工作量 | 累计 |
|------|--------|------|
| Phase 0：基础设施 | 2~3h | 2~3h |
| Phase 1：核心框架 + 聊天 | 6~8h | 8~11h |
| Phase 2：房间/用户/钱包 | 4~6h | 12~17h |
| Phase 3：游戏通用 + 观战 | 6~8h | 18~25h |
| Phase 4：棋类三款 | 4~6h | 22~31h |
| Phase 5：卡牌两款 | 4~6h | 26~37h |
| Phase 6：狼人杀 | 12~16h | 38~53h |
| Phase 7：清理（远期） | 4~6h | 42~59h |

---

## 九、消息类型完整清单（Proto 映射）

### 9.1 通用模块

#### common/envelope.proto
- `Envelope` — WS 帧信封
- `ErrorPayload` — 通用错误
- `HeartbeatPayload` — 心跳
- `AckPayload` — 通用确认

#### common/types.proto
- `Position` — 棋盘坐标 (x, y)
- `PlayerBase` — 玩家基础信息 (id, nickname, avatar)
- `RoomBase` — 房间基础信息 (id, game_kind, capacity, status)

### 9.2 Chat 模块

#### chat/chat.proto
- `ChatScope` — 枚举 (LOBBY / ROOM)
- `ChatSubscribe` — C→S 订阅
- `ChatUnsubscribe` — C→S 取消订阅
- `ChatSend` — C→S 发送
- `ChatHistoryReq` — C→S 历史请求
- `ChatMessage` — S→C 单条消息
- `ChatHistoryResp` — S→C 历史响应
- `ChatSubscribeAck` — S→C 订阅确认

#### chat/whisper.proto
- `ChatWhisperSend` — C→S 发送私聊
- `ChatWhisperMessage` — S→C 私聊消息

#### chat/activity.proto
- `ActivityEvent` — S→C 游戏活动事件
- `ActivityKind` — 枚举 (phase_change / vote_result / player_died / ...)

#### chat/stream.proto
- `ChatStreamStart` — S→C 流式开始
- `ChatStreamDelta` — S→C 流式增量
- `ChatStreamEnd` — S→C 流式结束

### 9.3 Room 模块

#### room/room.proto
- `RoomListReq` — C→S 列表请求
- `RoomCreateReq` — C→S 创建
- `RoomJoinReq` — C→S 加入
- `RoomLeaveReq` — C→S 离开
- `RoomInfo` — 房间简要信息
- `RoomPlayerInfo` — 玩家信息
- `RoomDetail` — 房间详情
- `RoomListResp` — 列表响应
- `RoomLeaveResp` — 离开确认

#### room/events.proto
- `RoomStateEvent` — S→C 房间状态变更
- `RoomPlayerStatusEvent` — S→C 玩家状态变更

### 9.4 User 模块

#### user/user.proto
- `UserListReq` — C→S 列表请求（分页/排序/过滤）
- `UserDeleteReq` — C→S 删除
- `UserBatchDeleteReq` — C→S 批量删除
- `UserRevokeSuperReq` — C→S 撤销超管
- `UserItem` — 用户条目（按权限裁剪字段）
- `UserListResp` — 列表响应
- `UserDeleteResp` — 删除回执
- `UserBatchDeleteResp` — 批量删除回执
- `UserRevokeSuperResp` — 撤销回执

#### user/events.proto
- `UserDeleted` — S→C 用户被删除广播
- `UserRoleChanged` — S→C 角色变更广播

### 9.5 Wallet 模块

#### wallet/wallet.proto
- `WalletBalanceUpdate` — S→C 余额变动推送

### 9.6 游戏通用

#### game/common.proto
- `GameJoinReq` — C→S 加入
- `GameLeaveReq` — C→S 离开
- `GameResignReq` — C→S 认输
- `GameStateReq` — C→S 状态请求
- `GameJoined` — S→C 加入确认
- `GameLeft` — S→C 离开确认
- `GameStarted` — S→C 游戏开始
- `GameOver` — S→C 游戏结束
- `GameError` — S→C 游戏错误
- `GamePeerJoined` — S→C 对手加入
- `GameRemoved` — S→C 房间被移除

#### game/spectator.proto
- `SpectateReq` — C→S 观战
- `UnspectateReq` — C→S 取消观战
- `SpectatorsReq` — C→S 观战者列表
- `Spectated` — S→C 观战确认
- `Unspectated` — S→C 取消观战确认
- `SpectatorsResp` — S→C 观战者列表

### 9.7 各游戏专属（概要）

#### game/xiangqi/xiangqi.proto
- `MoveReq`, `MoveResp`, `GameStateXiangqi`

#### game/chess/chess.proto
- `MoveReq` (含 promotion), `PromoteReq`, `MoveResp`, `GameStateChess`

#### game/junqi/junqi.proto
- `LayoutReq`, `LayoutAccepted`, `LayoutSubmitted`, `MoveReq`, `MoveResp`, `GameStateJunqi`

#### game/doudizhu/doudizhu.proto
- `BidReq`, `BidResp`, `PlayReq`, `PlayResp`, `PassReq`, `PassResp`, `Redealt`, `GameStateDoudizhu`

#### game/texasholdem/texasholdem.proto
- `ActionReq`, `ActionAccepted`, `GameStateTexas`

#### game/werewolf/（最大模块）
- `werewolf.proto` — 通用帧 + 枚举
- `actions.proto` — 夜间动作（6 种角色动作）
- `vote.proto` — 投票 / 警长 / 公投 / 猎人开枪
- `speak.proto` — 发言 / 遗言 / 警徽流
- `roles.proto` — 角色/阵营/阶段枚举
- `state.proto` — ClientGameState
- `prop.proto` — 道具系统
- `restart.proto` — 重开局投票

---

## 十、风险与应对

| 风险 | 概率 | 影响 | 应对措施 |
|------|------|------|---------|
| **双协议维护成本高** | 高 | 中 | 采用适配器模式，业务逻辑只写一份，proto/JSON 各有薄适配层 |
| **消息类型多、迁移周期长** | 高 | 中 | 分阶段实施，Phase 1~3 优先验证框架，Phase 4~6 可并行 |
| **前端包体积增加** | 中 | 低 | @protobuf-ts runtime 仅 ~10KB gzip；按需 import |
| **调试难度增加** | 中 | 中 | 提供 devtool 解析工具；开发环境同时输出 JSON 日志 |
| **向后兼容问题** | 中 | 高 | JSON 通道至少保留 2 个大版本；协议协商机制自动降级 |
| **Proto 定义迭代失控** | 中 | 中 | buf lint + breaking change 检测；字段编号永不复用 |
| **狼人杀 state 超大** | 高 | 中 | 分拆为多个子消息 + oneof；增量更新优先于全量刷新 |

---

## 十一、验证标准

### 11.1 功能验证

- [ ] 所有消息类型在 proto 中有定义
- [ ] Go 代码生成成功，无编译错误
- [ ] TS 代码生成成功，无 tsc 错误
- [ ] JSON 客户端可正常连接（向后兼容）
- [ ] Proto 客户端可正常连接
- [ ] 双客户端同房间互通（服务端双写）
- [ ] 6 款游戏全部正常运行
- [ ] 聊天/用户/房间/钱包全部正常

### 11.2 性能验证

- [ ] 消息体体积对比测试（JSON vs Proto）
- [ ] 序列化/反序列化耗时对比
- [ ] 狼人杀 13 人局全状态帧大小对比
- [ ] 前端 bundle size 变化

### 11.3 质量验证

- [ ] `go build ./...` 通过
- [ ] `go test ./...` 通过
- [ ] `tsc --noEmit` 通过
- [ ] `npm run build` 通过
- [ ] `buf lint` 通过
- [ ] `buf breaking` 检测通过

---

## 十二、本次实施范围（§20260811-01）

> **明确**：本次提交不做全量迁移（工作量过大、风险过高）。
> 本次完成 **Phase 0 + Phase 1 核心框架**，为后续渐进迁移打下基础。

### 本次交付物

1. ✅ **官方技术栈文档** — `docs/ProtocolBuffers/protobuf-dev-官方技术栈指南.md`
2. ✅ **本重构方案** — `docs/ProtocolBuffers/ProtocolBuffers协议层模块化重构-20260811-01.md`
3. **Proto 目录结构** — 按模块化设计整理 proto/ 目录
4. **核心 proto 定义** — common / chat / room / user / wallet / game.common
5. **编译工具链** — `proto/gen.sh` + Go/TS 双端代码生成
6. **后端 Proto 框架** — ProtoRegistry + proto_router + BinaryMessage 分发
7. **前端 Proto 框架** — ws.ts BinaryMessage 支持 + protoRegistry
8. **聊天模块验证** — 用 chat 模块打通端到端 proto 链路
9. **协议协商机制** — system.proto_capability 握手

### 本次不包含

- ❌ 5 款游戏的完整 proto 迁移（xiangqi/chess/junqi/doudizhu/texasholdem）
- ❌ 狼人杀完整迁移（单独 Phase 6）
- ❌ HTTP API 的 protobuf 改造（仅 WS）
- ❌ JSON 通道的移除（保留至少两个大版本）
- ❌ 移动端代码生成

---

## 附录 A：参考资料

- [Protocol Buffers 官方站点](https://protobuf.dev/)
- [Proto3 语言指南](https://protobuf.dev/programming-guides/proto3/)
- [编码规范](https://protobuf.dev/programming-guides/encoding/)
- [风格指南](https://protobuf.dev/programming-guides/style/)
- [Go 代码生成指南](https://protobuf.dev/reference/go/go-generated/)
- 本项目 `docs/ProtocolBuffers/protobuf-dev-官方技术栈指南.md`
- 本项目 `CLAUDE.md` 第 1 节（技术栈）、第 6 节（网络）
- 本项目 `proto/` 目录（现有 5 个 proto 文件）
