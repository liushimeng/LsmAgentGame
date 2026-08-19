# 德州扑克 Agent 前端设计（v1.0）

> 本文定义德州扑克 Bot 接入后的**前端 UI 改造**：Bot 座位卡、BetHistory 渲染、
> 创建房间弹窗 AI 配置块、BotThoughtPanel 内心独白显示、错误处理。代码落地前
> 请先阅读本文档 + [德州扑克Agent设计.md §8.2](./德州扑克Agent设计.md) +
> [CLAUDE.md §26](../../CLAUDE.md) 「前端 UI 颜色对比度与可读性规范」。

## 1. 改造范围

| 文件 | 改动 | 优先级 |
|---|---|---|
| `ClientWeb/src/components/texasholdem/RoomCreateModal.tsx` | 新建（AI 配置块，0-6 slider） | P0 |
| `ClientWeb/src/components/texasholdem/PlayerSeat.tsx` | Bot 徽章 + 思考中指示器 | P0 |
| `ClientWeb/src/components/texasholdem/TexasHoldemTable.tsx` | 接入 BotThoughtPanel + 行动流 | P0 |
| `ClientWeb/src/components/texasholdem/BotThoughtPanel.tsx` | 新建（最近 1 句内心独白） | P1 |
| `ClientWeb/src/pages/TexasHoldemLobbyPage.tsx` | 接入 RoomCreateModal | P0 |
| `ClientWeb/src/components/texasholdem/ActionControls.tsx` | Bot 决策时禁用人类按钮 + "🤖 AI 决策中" 提示 | P1 |
| `ClientWeb/src/styles/texasholdem.css` | 新建（隔离样式） | P0 |
| `ClientWeb/src/i18n/{zh,en,ja}.json` | 30 个新键 | P0 |

## 2. Bot 座位卡设计

`PlayerSeat.tsx` 改造（与狼人杀 AgentCallTimeBadge 模式同源）：

```tsx
// 新增 Bot 徽章 — 在座位右上角显示「🤖 AI」
{p.isBot && (
  <div className="thp-seat__bot-badge" data-agent-key={agentKey}>
    🤖 {modelName}
  </div>
)}

// 思考中指示器 — 在座位底部显示「⏳ 思考中…」或「⏱ 15s」
{p.isBot && p.isThinking && (
  <BotPhaseIndicator seat={p.seat} phase="thinking" />
)}

// Bot 内心独白 — hover 弹全文
{p.isBot && p.heartThought && (
  <div className="thp-seat__heart-thought" title={p.heartThought}>
    💭 {truncate(p.heartThought, 30)}
  </div>
)}
```

**颜色对比度**（CLAUDE.md §26.4）：
- 「Bot AI」徽章色：青绿（hsl(160, 70%, 45%)），底透明度 ≥ 0.55，字色浅青绿/白，字重 600 + text-shadow

## 3. RoomCreateModal 设计

`RoomCreateModal.tsx` 新建（沿用狼人杀 RoomCreateModal 模式，简化版）：

```tsx
// AI 玩家数量 slider (0..6)
const MAX_AI_SEATS = 6;
const ALL_SEATS = Array.from({ length: MAX_AI_SEATS }, (_, i) => i);

function RoomCreateModal({ open, onClose, onSubmit, submitting = false }) {
  const [agentCount, setAgentCount] = useState(0);
  const [seats, setSeats] = useState<AgentSeatInput[]>([]);
  
  // 沿用狼人杀的 Fisher-Yates 模型分配
  useEffect(() => {
    if (agentCount === 0) { setSeats([]); return; }
    // ... 沿用狼人杀模式 ...
  }, [agentCount, models]);
  
  return (
    <div className="thp-create-modal" role="dialog" aria-modal="true">
      <div className="thp-create-modal__card">
        <header>
          <h2>创建德州扑克房间</h2>
          <button onClick={onClose}>×</button>
        </header>
        
        <div className="thp-create-modal__body">
          {/* ROW 1: 房间名 + AI 数量 slider */}
          <div className="thp-create-modal__row">
            <label>
              <span>房间名</span>
              <input value={name} onChange={...} maxLength={32} />
            </label>
            
            <div className="thp-create-modal__field">
              <div className="thp-create-modal__field-head">
                <span>AI 玩家数量: {agentCount}</span>
                <span>{agentCount === 0 ? '全人类' : `${6 - agentCount} 人类 + ${agentCount} AI`}</span>
              </div>
              <input type="range" min={0} max={6} value={agentCount} onChange={...} />
            </div>
          </div>
          
          {/* ROW 2: AI 座位区（沿用狼人杀弹性主体布局） */}
          {agentCount > 0 && models.length > 0 && (
            <div className="thp-create-modal__seatblock">
              <div className="thp-create-modal__seatblock-head">
                <span>AI 座位 ({seats.length}/6)</span>
                <button onClick={reshuffle}>🎲 重新分配</button>
              </div>
              <div className="thp-create-modal__seats">
                {seats.map((s, i) => (
                  <div key={i} className="thp-create-modal__seatrow">
                    <span>AI {i + 1}</span>
                    <select value={s.seat} onChange={...}>
                      {ALL_SEATS.map((n) => (
                        <option key={n} value={n} disabled={usedSeats.has(n) && n !== s.seat}>
                          {n + 1} 号位
                        </option>
                      ))}
                    </select>
                    <select value={s.model_key} onChange={...}>
                      {models.map((m) => (
                        <option key={m.model} value={m.model}>{m.agent_name}</option>
                      ))}
                    </select>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
        
        <footer>
          <button onClick={onClose}>取消</button>
          <button disabled={!valid || submitting} onClick={handleSubmit}>创建</button>
        </footer>
      </div>
    </div>
  );
}
```

**与狼人杀 RoomCreateModal 的差异**：
- 容量上限 6（狼人杀 13）
- **无法官模式**（v1.0 不实现）
- **无角色选择**（v1.0 不实现）
- **无难度档位**（v1.0 不实现）
- **无 AI 解说**（v1.0 不实现）

**v1.1 增量**：补难度档位 + 玩家画像。

## 4. BotThoughtPanel 设计

`BotThoughtPanel.tsx` 新建（沿用狼人杀 AgentThoughtPanel 模式，简化版）：

```tsx
// 显示最近 1 句内心独白 + 折叠历史
function BotThoughtPanel({ seat, visible = true }: Props) {
  const heartThought = useBotThought(seat);
  if (!visible || !heartThought) return null;
  
  return (
    <details className="thp-bot-thought">
      <summary>💭 最近决策</summary>
      <p className="thp-bot-thought__text">{heartThought}</p>
    </details>
  );
}
```

**位置**：贴在 `TexasHoldemTable.tsx` 右上角，与 `GameInfoPanel` 同行。

**颜色**（CLAUDE.md §26.4）：
- 内心独白卡片背景：`rgba(155, 89, 182, 0.18)`（紫色，Bot 标识色）
- 字色：`#f5e8ff`（浅紫白，对比度 ≥ 7.5:1）

## 5. 错误处理（CLAUDE.md §7.1）

### 5.1 创建房间失败

`onSubmit` 返回 `Promise<boolean>`：
- `true` → 父组件已关闭弹窗（成功）
- `false` → 弹窗不关闭 + 显示 `formError` 红条（沿用狼人杀 `BUG-R210-04` 模式）

```tsx
const [formError, setFormError] = useState<string | null>(null);

const handleSubmit = async () => {
  setFormError(null);
  try {
    const ok = await onSubmit({ name, agent_seats: seats });
    if (ok === false) setFormError('创建失败，请检查后重试');
  } catch (e: any) {
    setFormError(`创建异常: ${e.message}`);
    reportGlobalError({ message: e.message, severity: 'error' });
  }
};
```

### 5.2 Bot 决策失败

若 Bot 30s 内未决策（LLM 超时），服务端兜底 fold，前端收 `game.action_accepted{type:"fold"}` 帧，
无需额外错误展示（这是「Bot 不行动」自然结果，不是 bug）。

### 5.3 LLM 调用全局错误（registry 不可用）

`listModels()` 失败 → 显示「模型不可用，请稍后再试」+ 隐藏 AI 玩家配置区，
强制纯人类房间（沿用狼人杀模式）。

## 7. i18n 三语种

新增 30 个键到 `i18n/{zh,en,ja}.json`：

```json
// zh-CN
{
  "texasholdem.createModal.title": "创建德州扑克房间",
  "texasholdem.createModal.roomName": "房间名",
  "texasholdem.createModal.aiCount": "AI 玩家数量: {count}",
  "texasholdem.createModal.allHuman": "全人类对局",
  "texasholdem.createModal.humanAiMix": "{human} 人类 + {ai} AI",
  "texasholdem.createModal.aiSeats": "AI 座位 ({count}/6)",
  "texasholdem.createModal.reshuffle": "🎲 重新分配",
  "texasholdem.createModal.aiSeatLabel": "AI {index} 座位",
  "texasholdem.createModal.aiModelLabel": "AI {index} 模型",
  "texasholdem.createModal.loadingModels": "加载模型中...",
  "texasholdem.createModal.modelsUnavailable": "模型加载失败: {error}",
  "texasholdem.createModal.allHumanEmptyState": "全人类对局，无 AI 玩家",
  "texasholdem.createModal.commentaryRowHint": "提示：AI 玩家将与人类实时博弈",
  "texasholdem.createModal.submitFailed": "创建失败，请检查后再试",
  "texasholdem.createModal.submitError": "创建异常: {message}",
  "texasholdem.bot.badge": "🤖 AI",
  "texasholdem.bot.thinking": "⏳ 思考中…",
  "texasholdem.bot.heartThought": "💭 内心独白",
  "texasholdem.bot.recentDecision": "最近决策",
  "texasholdem.bot.actionTimeout": "⏱ 决策超时（已弃牌）",
  "texasholdem.table.botSeatLabel": "AI 玩家",
  "texasholdem.table.actionDisabled": "AI 决策中...",
  // ... 共 30 键
}
```

## 8. CSS 样式约定

新建 `ClientWeb/src/styles/texasholdem.css`（与 `werewolf-v2.css` 隔离）：

- 房间卡片背景：`#1a1f2e`（深色主题）
- Bot 徽章：青绿色 + 紫色光晕（与法官徽章区分）
- 内心独白卡片：紫色边框 + 半透明白底
- hover 弹全文：`title` 属性 + `cursor: help`

**色相对比度**（CLAUDE.md §26.4）：
- 「Bot AI」徽章：青绿（hsl(160, 70%, 45%)）+ 白字 + 600 字重 + text-shadow → 对比度 ≥ 5.5:1
- 「思考中」指示器：紫色（hsl(280, 60%, 55%)）+ 白字 → 对比度 ≥ 5.0:1

## 9. 测试用例

- `RoomCreateModal.test.tsx` — slider 0→6 切换，seat 列表更新（沿用狼人杀测试）
- `BotThoughtPanel.test.tsx` — 渲染/折叠/hover 弹全文
- `PlayerSeat.test.tsx` — Bot 徽章 + 思考中指示器
- `texasholdem_lobby_e2e_test.ts` — 创建 6 AI 房间 → 自动开局 → 30s 内必有 action

## 10. 不实现的部分（v1.0 边界）

- ❌ Bot 决策延时可视化（进度条）
- ❌ Bot 决策历史时间线（每个 bot 的「这手牌做了什么」回放）
- ❌ 玩家画像 UI（v1.1 再做）
- ❌ AI 解说（v1.1 再做）