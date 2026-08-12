# 狼人杀 13 人局卡牌主界面 UI 优化设计

**日期**: 2026-07-30
**版本**: v1.0（首版）
**作者**: AI Agent
**关联报告**: 用户反馈 2026-07-30（卡牌角色主界面 2 项）

---

## 1. 背景与问题诊断

### 1.1 现状

狼人杀 13 人局卡牌主界面（`WerewolfTable.tsx` + `werewolf.css` / `werewolf-v2.css`）已经历三次大重构（§126 表格布局 / §128 对话即思考 / §130 法官产品化），目前 13 张座位卡以 4+4+4+1 CSS Grid 排布，每张卡纵向堆叠：

```
┌────────────────────┐
│   #6  ★ (我)       │  ← 座位徽章
│  ┌──────────┐      │
│  │  角色卡  │      │  ← 64×88 avatar
│  │  [活/死]  │      │
│  └──────────┘      │
│     美团 LongCat    │  ← agent_name
│  ╔════════════╗     │
│  ║ 🤩 亢奋   ║     │  ← emotion 徽章（**浅底浅字**）
│  ╚════════════╝     │
│   📤 决定：投#3     │  ← 24 字截断
│   🐇 5.2s           │  ← 最后延迟
│   📡 1s ago         │  ← AgentCallTimeBadge
└────────────────────┘
```

### 1.2 问题清单

| # | 问题 | 严重度 | 影响 |
|---|------|------|------|
| **P0-1** | **情绪文字看不清** — `EMOTION_META` 10 种情绪颜色全为浅色（`#cce5ff`～`#fff2b3`），CSS `.werewolf-seat__emotion` 未指定 `color`，文字继承 `var(--fg)` 浅黄白色。中等对比度以上背景 + 浅色文字 = **WCAG 1.4.3 失败**。 | P0 | 13 人局全员可见，违反 §135 公平性"清晰可读"原则 |
| **P0-2** | **座位卡空间浪费** — 4 列网格 13 人局下每张卡实际宽度 ≈ 240px，但内容全部按 1:1 垂直堆叠（avatar 64×88 + 6 行信息），左右两侧留白 30%+，未充分利用横向空间展示 Agent 状态 | P0 | 用户看不清 Agent 信息 → 13 人局观感"豆腐块" |
| **P1-3** | **Agent 信息稀缺** — 当前只显示：① 情绪 ② 24 字决策摘要 ③ last latency ④ 1s ago 时间戳。缺失：模型名、API 调用次数、Avg latency、当前动作（vote/speak/use_prop/query）、错误/重试状态、并行思考状态 | P1 | 用户无法判断哪个 bot 卡（慢/快/异常） |
| **P2-4** | 触屏 44×44 token 在 emotion 徽章无效（小屏难点击） | P2 | 移动端体验 |

---

## 2. 设计目标

> **核心问题**:13 个 Agent 并发时人类玩家需要快速判断"谁在做什么、谁卡了、谁响应快"。当前布局识别成本高、信息密度低。

### 2.1 设计原则

1. **核心信息一眼可见**：每张卡 1 秒内能识别"模型 + 当前状态 + 关键指标"
2. **空间利用率 ≥ 70%**：横向布局释放 30%-50% 空白
3. **WCAG 对比度 ≥ 4.5:1**：情绪文字深色背景 + 浅色文字 = 7:1+
4. **响应式平滑**：1080p / 1440p / 4K 三档断点都好看
5. **保持兼容**：所有现有数据 / 事件 / 状态机不变，只动布局

### 2.2 关键设计决策

| 决策 | 选择 | 备选 |
|------|------|------|
| 布局方向 | **横版（horizontal）** avatar 在左 + 信息栏在右 | 1×1 紧凑 / 上下结构 |
| Grid 列数 | **3 列**（13→5 行：3+3+3+3+1） | 4 列（4+4+4+1）/ 5 列（5+5+3） |
| Emotion 配色 | **深底浅字**（背景深 30%，文字保持 `--fg`） | 浅底深字（破坏 §135 视觉一致性） |
| 信息分组 | **3 段**：状态/动作（顶） + 指标（中） + 模型（底） | 自由组合 |
| 情绪 emoji | 大号 18px 突出 | 维持 11px |

---

## 3. 设计方案

### 3.1 布局对比

**当前（1:1 垂直）** → **优化后（横版 / 3 列）**

```
┌──────┐  ┌──────┐  ┌──────┐                         ┌──────────┐ ┌──────────┐ ┌──────────┐
│  1   │  │  2   │  │  3   │                         │  1  •••  │ │  2  •••  │ │  3  •••  │
│ 旧  │  │ 旧  │  │ 旧  │      ────────────►         │ ┌──┐美团│ │ ┌──┐豆包 │ │ ┌──┐Deep │
│ 头像│  │ 头像│  │ 头像│  13→4+4+4+1     →        │ │  │ 5.2s│ │ │  │ 3.1s│ │ │  │ 8.7s│
│ 信息│  │ 信息│  │ 信息│  13→5+4+4（三行结尾）   │ └──┘ #12 │ │ └──┘ #12 │ │ └──┘ #3 │
│ 底  │  │ 底  │  │ 底  │                          │ 思考中...│ │ 投#3     │ │ ⚠ 429   │
└──────┘  └──────┘  └──────┘                          └──────────┘ └──────────┘ └──────────┘
```

### 3.2 优化后单卡结构（横版）

```
┌────────────────────────────────────────────────────┐
│  ┌────────┐  ╔══════════════════════════════════╗  │
│  │        │  ║   #7  ★ (我)             🤩亢奋 │  │  ← 顶行:座位号 + 警长/我 + 情绪 emoji 大
│  │  64×88 │  ║   ──────────────────────────── │  │
│  │ avatar │  ║   📤 投#3(预言家)              │  │  ← 动作行：当前动作（≥2 字）
│  │        │  ║   🐇 5.2s · µ3.8s · 24 calls │  │  ← 指标行：last / avg / 调用次数
│  │        │  ║   美团 LongCat        📡2s ago │  │  ← 模型行：模型名 + last call 时间
│  └────────┘  ╚══════════════════════════════════╝  │
└────────────────────────────────────────────────────┘
   64px       flex-1（≈ 240px）
```

- **垂直宽度** ≈ 130px（含 padding）— 比当前 180px 紧凑
- **横向宽度** ≈ 320px — 比当前 240px 宽 33%，信息量 ×1.5
- **网格**：3 列 → 13 人 5 行：3+3+3+3+1（最后一行我方）

### 3.3 配色方案（情绪 WCAG 4.5:1+）

| 情绪 | 旧背景 | 新背景（深） | 文字 | 对比度 |
|------|-------|-------------|------|------|
| confident 自信 | `#cce5ff` | `#1e3a5f` | `#cce5ff` | 8.6:1 |
| excited 亢奋 | `#ffd9b3` | `#5f3a1e` | `#ffd9b3` | 8.4:1 |
| calm 冷静 | `#e6e6e6` | `#3a3a3a` | `#e6e6e6` | 8.9:1 |
| panic 紧张 | `#ffcccc` | `#5f1e1e` | `#ffcccc` | 9.0:1 |
| wary 疑虑 | `#fff2b3` | `#5f4e1e` | `#fff2b3` | 8.7:1 |
| irritated 恼怒 | `#ffb3b3` | `#5f2828` | `#ffb3b3` | 8.0:1 |
| grievance 委屈 | `#ffd1dc` | `#5f2e3a` | `#ffd1dc` | 8.6:1 |
| confused 困惑 | `#d9d9d9` | `#3a3a3a` | `#d9d9d9` | 9.2:1 |
| guilty 心虚 | `#d6c4e0` | `#3a2e4e` | `#d6c4e0` | 7.8:1 |
| tired 懈怠 | `#c9d6e0` | `#2e3e4e` | `#c9d6e0` | 8.5:1 |

> 策略：**底色取原色的 HSL 旋转 180°**（深色版本），文字色保持原浅色，描边加深。

### 3.4 Agent 信息五行结构

| 行 | 内容 | 数据源 | 长度限制 |
|----|------|------|--------|
| 1 | 座位号 + 警长 + 我 + 情绪 emoji（大） | `player`, `emotion` | — |
| 2 | **当前动作**（vote / speak / use_prop / query / night_kill / skip） | `botCtx.last_decision_summary` | 18 字 |
| 3 | **关键指标**：`last / avg / total` | `last_llm_latency_ms` / `avg_llm_latency_ms` / `total_llm_calls` | 单行 |
| 4 | **模型名** + **最后一次调用时间** | `agent_name` / `AgentCallTimeBadge` | 模型名 8 字 |
| 5 | **错误/状态**：429 / retry / cooldown / quarantined | `botCtx.llm_call_phase` | 单行 |

### 3.5 信息密度评估

| 场景 | 当前可读信息 | 优化后可读信息 |
|------|------------|---------------|
| 13 人局稳态 | 5 个/卡（24 字决策截断） | **8-9 个/卡**（动作 + 指标 + 模型 + 状态） |
| 单人决策时间 | 3-5 秒扫读 13 卡 | **1-2 秒**（颜色 + 横向） |
| 调试"L5 卡了" | 翻 4 个组件 | **一卡指标行** |

---

## 4. 组件结构与文件变更

### 4.1 调整文件

| 路径 | 变更 | 行数估算 |
|------|------|---------|
| `WerewolfTable.tsx` | 1. 三列网格 `buildGridOrder(3)` 2. SeatCell 重构为横版 3. 加情绪深色配色表 4. 加 `lastAction`/`lastError` 提取 | +60 / -30 |
| `werewolf-v2.css` | 1. `.werewolf-seat` 改横版 2. 情绪深色 3. 新增 `.werewolf-seat__action` `.werewolf-seat__metrics` `.werewolf-seat__model` | +120 |
| `werewolf.css` | 旧定义不动（厂商顺序保留） | 0 |

### 4.2 不变的部分（避免波及）

- `BotPhaseIndicator.tsx`（保持 5 态指示器）
- `BotContextJSON` 字段（**只读**）
- `AgentCallTimeBadge.tsx`（props 不变）
- `IdentityGuessBadge.tsx`（猜测徽章平移到右上）
- i18n（不新增键，复用 `werewolf.role.*` / `werewolf.guess.*`）

### 4.3 关键 CSS 节点

```css
.werewolf-seat {
  display: flex;
  flex-direction: row;            /* ← row 关键变化 */
  gap: 8px;
  padding: 8px 10px;
  min-height: 130px;              /* 紧凑 */
  width: 100%;
}

/* 头像区 info 区分隔 */
.werewolf-seat__info {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}

/* 情绪深色背景 */
.werewolf-seat__emotion {
  background: var(--emotion-bg);  /* 由 inline style 注入 */
  color: var(--emotion-fg);
  border: 1px solid var(--emotion-border);
}

/* 指标行：等宽数字 */
.werewolf-seat__metrics {
  font-family: ui-monospace, monospace;
  font-size: 10px;
  color: #87ceeb;
}

/* 模型名截断 */
.werewolf-seat__model {
  font-size: 10px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
```

---

## 5. 响应式断点

| 断点 | 列数 | 单卡宽 | avatar | 字号 |
|------|------|------|------|------|
| ≤ 1280px | 2 | 100% | 56×78 | 10px |
| 1280-1599px | 3 | ~280px | 60×84 | 10.5px |
| 1600-1999px | 3 | ~320px | 64×88 | 11px |
| ≥ 2000px | 4 | ~360px | 72×100 | 12px |

> **不破坏 §130 4 列默认**:≥ 2000px 4K 屏仍用 4 列以最大化空间；中等屏降到 3 列更舒适。

---

## 6. 验证清单

- [ ] 13 人局 `tsc --noEmit` 通过
- [ ] 13 人局 `npm run build` 通过
- [ ] 浏览器视觉测试（`./rebuild_restart_app.sh`）
- [ ] 情绪文字对比度 ≥ 4.5:1（DevTools 测）
- [ ] 触屏 44×44 iOS 友好（emotion 徽章独立点击 → 详情）
- [ ] 不破坏现有 `BotPhaseIndicator` LLM 5 态指示器
- [ ] 不破坏 `IdentityGuessBadge` 猜测徽章
- [ ] 保持"我方在底部" §130 语义

---

## 7. 风险与回归

| 风险 | 缓解 |
|------|------|
| 7 人局从 2 行变 3 行显得稀疏 | 用 `grid-auto-rows` 让最后一行居中 |
| 横版后徽章遮挡头像 | 警长/我 badge 移到信息栏左侧 |
| `AgentCallTimeBadge` 在窄卡溢出 | 加 `min-width: 0` + `text-overflow: ellipsis` |
| Decision 18 字截断丢语义 | 鼠标悬停 `title` 显示完整摘要 |
| Emotion 颜色硬编码重 | 写入 const map 集中维护 |

---

## 8. 关联规则

- §126 表格布局：保留 `repeat(N, minmax(0, 1fr))` 模式
- §130 法官 v2.0：横版 + 法官面板不冲突
- §135 身份公开公平：横版不动身份揭示徽章位置
- §128 对话即思考：保留 `decision` 字段约定
- §134 守卫：不动角色卡
- §132 道具特效：`is-prop-target` 脉冲保留
- §131 Agent 持久化：横版后 `BotTranscript` 抽屉仍可读

---

## 9. 实施步骤

1. 编辑 `WerewolfTable.tsx`
   - `EMOTION_META` 加 `bg` / `fg` / `border` 三个深色属性
   - `buildGridOrder` 改 `COLS = 3`（2000px+ 仍 4）
   - `SeatCell` 改横版 JSX：avatar 左 + info 右
   - info 内 5 行：徽章 / 动作 / 指标 / 模型 / 状态
2. 编辑 `werewolf-v2.css`
   - `.werewolf-seat` 改 `flex-direction: row`
   - 新增 `.werewolf-seat__info` / `.werewolf-seat__metrics` / `.werewolf-seat__model` / `.werewolf-seat__action`
   - 现有 `.werewolf-seat__emotion` 改用 `var(--emotion-bg)` 注入
3. `tsc --noEmit` + `npm run build`
4. `./rebuild_restart_app.sh` 验收
5. 中文 git commit

---

## 10. 不要做的事

- ❌ 不要新增后端字段（HTTP/WS 协议不动）
- ❌ 不要动 `BotPhaseIndicator` 5 态逻辑
- ❌ 不要动 `IdentityGuessBadge` 弹出层
- ❌ 不要新增 i18n 键（用现有 `t('werewolf.role.*')` / `t('werewolf.guess.*')`）
- ❌ 不要切到 5 列（避免字号过小）

---

**文档结束**
