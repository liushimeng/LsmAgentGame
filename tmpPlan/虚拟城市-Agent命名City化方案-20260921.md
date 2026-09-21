# 虚拟城市 — Agent 命名 City 化方案（2026-09-21）

> 工作面：`ServerGo/**`（backend-dev 职责线）。
> 背景：游戏已由「财商流」更名「虚拟城市」（commit 38b415c5f4），但 AgentClassName
> 仍残留 `LsmAgentGame-Wealth-*`。按 CLAUDE.md §24 命名规则 `<Game>-<Role>` 统一为 `City`。

---

## 1. 改名映射

| 旧常量 | 旧值 | 新常量 | 新值 |
|---|---|---|---|
| `AgentClassWealthPlayer` | `LsmAgentGame-Wealth-Player` | `AgentClassCityPlayer` | `LsmAgentGame-City-Player` |
| `AgentClassWealthCityVoice` | `LsmAgentGame-Wealth-CityVoice` | `AgentClassCityVoice` | `LsmAgentGame-City-Voice` |

常量名同步去 `Wealth` 化（防 §130「名实不符」），包名 `agent/wealthplayer`、
`agent/wealthtypes` 本次**不动**（目录改名波及面大，留待单独重构批次）。

## 2. 改动文件清单（已全量 grep 核实）

| 文件 | 行 | 改动 |
|---|---|---|
| `ServerGo/agent/class_names.go` | ~156/158, 204~220 | 常量改名 + 改值 + 注释同步（`AllAgentClassNames()` 列表同步） |
| `ServerGo/agent/class_names_test.go` | 38, 64 | 断言新字符串值；断言常量非空（§24 防不接线） |
| `ServerGo/agent/wealthplayer/agent.go` | 7(注释), 123 | `return agentroot.AgentClassCityPlayer`；注释同步 |
| `ServerGo/agent/wealthtypes/context.go` | 210(注释), 260 | 字面量 `"LsmAgentGame-Wealth-Player"` → 新值；**建议直接引用常量**而非字面量（§130 接线原则） |
| `ServerGo/game/wealth/agent_runner.go` | 1014 | 字面量 `AgentClass: "LsmAgentGame-Wealth-Player"` → `string(agentroot.AgentClassCityPlayer)`（消灭硬编码字面量） |
| `ServerGo/game/wealth/city/voice.go` | 6(注释), 126 | `string(agentroot.AgentClassCityVoice)`；注释同步 |

## 3. 不改的部分

- `lag_docs/**` 历史设计文档保留旧名原样（历史快照性质，代码为唯一事实来源）。
- `CLAUDE.md §24` 表格未登记虚拟城市 Agent，无需改动。
- wire 协议 / 数据库 / 前端均不含 AgentClassName，零波及。

## 4. 验收

1. `cd ServerGo && go build -o LsmAgentGame main.go` 通过。
2. `go test ./...` 全绿（重点 `agent/class_names_test.go`）。
3. `git grep -n "LsmAgentGame-Wealth" -- ServerGo/` 零命中。
4. `git grep -n "AgentClassWealth" -- ServerGo/` 零命中。
