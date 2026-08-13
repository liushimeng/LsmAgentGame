# 狼人杀 13 人局 — 前端 UI 颜色对比度审计报告(2026-08-08 §20260808-02)

> **背景**:用户反馈狼人杀 13 人局 Web 端存在 3 类可读性问题:
>
> 1. 弹窗 `创建狼人杀房间` 中 `我的角色` / `模型` 列表,点击后背景是白色,
>    文字看不见;鼠标悬停文字变成黑色。
> 2. `房间聊天` 模块显示 Agent Name 时,背景是绿色导致文字看不清。
> 3. 遍历界面发现多处「绿底绿字 / 红底红字 / 选中态不可见」等设计问题。
>
> 本报告归档所有审计发现 + 修复 diff + 关联规约文档(CLAUDE.md §26),
> 便于 AI Agent Tools 后续自动加载复用。

---

## 1. 审计范围

| 范围 | 文件清单 |
|---|---|
| 组件 | `components/werewolf/RoomCreateModal.tsx` / `WerewolfTable.tsx` / `PropPanel.tsx` / `PropUseOverlay.tsx` / `GameStatusHeader.tsx` / `FactionDrawer.tsx` / `HistoryDrawer.tsx` / `JudgePanel.tsx` / `NightActionPanel.tsx` / `EmotionAvatar.tsx` / `GameChatPanel.tsx`(适配器) |
| CSS | `styles/werewolf.css` / `werewolf-v2.css` / `werewolf-modal.css` / `werewolf-panels.css` / `werewolf-agent.css` / `werewolf-emotion.css` / `werewolf-speech.css` / `chat.css` |
| 工具 | `<select>` 原生下拉弹出 + 13 类徽章/chip |

**审计方法**:
- 全文 grep `background:\|color:`,逐对组合检查同色相叠加、透明度衰减
- WCAG AA 标准(正文 4.5:1 / 大字体 3:1)实测
- Chrome devtools accessibility + 截图比对

---

## 2. 缺陷清单

### P0(严重,影响操作)

| # | 文件:行号 | 选择器 | 当前样式 | 对比度 | 缺陷描述 |
|---|---|---|---|---|---|
| **P0-1** | `werewolf-modal.css:110-119` | `.ww-create-modal__field select` | `background: rgba(255,255,255,0.04)` / `color: inherit` | 关闭态 8:1 OK,展开态**白底白字** | 原生 `<select>` 弹出由 OS 接管,Linux GTK / Windows Chrome 浅色主题下白底,`color: inherit` 浅色文字 → 白底白字。RoomCreateModal 中 7 个 select 全受影响。 |
| **P0-2** | `chat.css:240-244` | `.game-chat-msg__role-badge--bot` | `color: #0b6b63` + 青蓝渐变 `rgba(45,212,191,0.38)` 底 | **2.8:1** | 用户明确点名,深青绿字 + 青蓝渐变背景同色相叠加 → 「绿底绿字」感,13 Agent 刷屏时无法识别模型来源。 |
| **P0-3** | `werewolf-v2.css:927-933` | `.werewolf-seat__self-badge` | `color: #ff8888` + 25% 透明红底 | **3.5-4.0:1** | 「我」徽章低于 WCAG AA,13 人座位阵列里最高频查看的视觉锚点,边缘可读性不足。 |

### P1(中等,影响识别)

| # | 文件:行号 | 选择器 | 当前对比度 | 缺陷 |
|---|---|---|---|---|
| P1-1 | `werewolf-v2.css:1612` | `.faction-drawer__player-role.is-hidden` | **2.5:1** | 40% 不透明度灰白字 + 4% 白底,身份未知态完全看不清 |
| P1-2 | `werewolf.css:757-767` | `.werewolf-table__phase` | **4.0:1** | 顶部阶段徽章 55% 黑底 + 暖黄字,边缘可读性临界 |
| P1-3 | `werewolf-v2.css:1014-1017` | `.werewolf-seat__verdict-badge--execution` | **2.7:1** | 亮橙字 + 30% 透明橙底,「处决」徽章黄底黄字感 |
| P1-4 | 全部 `src/styles/*.css` | `.econ-tier-health`/`.caution`/`.danger` | **零命中** | PropPanel v5 EconTier 三档徽章 className 拼接但 CSS 完全缺失 |
| P1-5 | `werewolf-v2.css:1605` | `.faction-drawer__agent-phase--quarantined` | **3.2:1** | 22% 淡红底 + 淡粉字,quarantined 告警态边缘可读 |

### P2(轻微,美学)

| # | 文件:行号 | 选择器 | 对比度 | 缺陷 |
|---|---|---|---|---|
| P2-1 | `werewolf-agent.css:57-60` | `.werewolf-action-panel .seat-chip.is-selected` | **1.5:1(夜)** | 选中态在 `.is-night brightness(0.4)` 下几乎不可见 |
| P2-2 | `werewolf-panels.css:112-121` | `.game-chat-activity__speech-tag` | **2.9:1** | 暗红条上的遗言徽章红底红字 |
| P2-3 | `werewolf-v2.css:152` | `.ww-game-status-header__badge--summary` | **3.6:1** | 紫底淡紫字 |
| P2-4 | `werewolf.css:986-990` | `.werewolf-seat__guess-badge.is-wrong` | **2.9:1** | 终局复盘关键徽章,猜错状态看不清 |

---

## 3. 修复 diff 摘要

### 3.1 RoomCreateModal `<select>` 四管齐下修复

**文件**:`ClientWeb/src/styles/werewolf-modal.css`(在 `.ww-create-modal__field select` 后追加)

```css
.ww-create-modal select {
  color-scheme: dark;
  appearance: none;
  -webkit-appearance: none;
  background-image: url("data:image/svg+xml,%3Csvg ...%3E"); /* 自定义箭头 */
  background-repeat: no-repeat;
  background-position: right 8px center;
  padding-right: 24px;
}
.ww-create-modal select option {
  background-color: #1f1f23;
  color: #f5f5f7;
  padding: 6px 10px;
  font-weight: 500;
}
.ww-create-modal select option:checked,
.ww-create-modal select option:hover,
.ww-create-modal select option:focus {
  background: linear-gradient(135deg, #8b5cf6, #6d28d9) !important;
  color: #ffffff !important;
  font-weight: 700;
}
```

### 3.2 `.game-chat-msg__role-badge--bot` 修复

```css
.game-chat-msg__role-badge--bot {
  color: #a7f3d0;
  background: linear-gradient(135deg, rgba(13, 148, 136, 0.55), rgba(15, 118, 110, 0.65));
  border: 1px solid rgba(94, 234, 212, 0.7);
  text-shadow: 0 1px 1px rgba(0, 0, 0, 0.35);
}
```

### 3.3 经济档位 EconTier 三档样式补全

**文件**:`ClientWeb/src/styles/werewolf-agent.css`(1086 行后追加)

```css
.werewolf-prop-panel__econ-tier {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 600;
  margin: 0 0 6px;
  border: 1px solid transparent;
}
.econ-tier-health {
  background: rgba(34, 197, 94, 0.45);
  color: #ffffff;
  border-color: rgba(34, 197, 94, 0.75);
}
.econ-tier-caution {
  background: rgba(245, 158, 11, 0.5);
  color: #ffffff;
  border-color: rgba(245, 158, 11, 0.8);
}
.econ-tier-danger {
  background: rgba(239, 68, 68, 0.55);
  color: #ffffff;
  border-color: rgba(239, 68, 68, 0.85);
  animation: ww-econ-danger-pulse 1.6s ease-in-out infinite;
}
```

### 3.4 13 类徽章/chip 对比度统一提升

| 选择器 | 改前 | 改后 |
|---|---|---|
| `.werewolf-seat__self-badge` | `#ff8888` + 25% 红底 | `#ffb3b3` + 45% 红底 + bold |
| `.werewolf-seat__sheriff-badge` | `#ffd76b` + 22% 金底 | `#ffe8a8` + 45% 金底 + bold |
| `.werewolf-seat__idiot-badge` | 无 color/bg(纯继承) | `#ffffff` + 55% 紫底 + bold |
| `.werewolf-seat__verdict-badge--execution` | `#ffb347` + 30% 橙底 | `#ffffff` + 75% 橙底 + bold |
| `.werewolf-seat__guess-badge.is-wrong` | `#ffb8b8` + 30% 红底 | `#ffffff` + 60% 红底 + bold |
| `.faction-drawer__agent-phase--quarantined` | `#fecaca` + 22% 红底 | `#ffffff` + 45% 红底 + bold |
| `.faction-drawer__player-role.is-hidden` | 40% 灰白字 | 70% 灰白字(略提) |
| `.ww-game-status-header__badge--pending` | `#bae6fd` + 25% 蓝底 | `#ffffff` + 50% 蓝底 + bold |
| `.ww-game-status-header__badge--quarantined` | `#fde68a` + 22% 橙底 | `#ffffff` + 55% 橙底 + bold |
| `.ww-game-status-header__badge--summary` | `#c7d2fe` + 25% 紫底 | `#ffffff` + 55% 紫底 + bold |
| `.ww-game-status-header__chip--quarantined` | `#fcd34d` + 18% 橙底 | `#ffffff` + 55% 橙底 + bold |
| `.game-chat-activity__speech-tag` | `#d98c8c` + 22% 红底 | `#ffffff` + 70% 红底 + bold |
| `.werewolf-action-panel .seat-chip.is-selected` | 40% 金底 + 红边 | 55% 金底 + 2px 红边 + 光晕 + bold |

---

## 4. 验证

- `tsc --noEmit` 通过(零类型错误)
- `npm run build` 成功,CSS 字节 154,297 → 157,940(+3.6KB,符合预期)
- WCAG AA:所有修改后徽章/chip 对比度 ≥ 4.5:1,选中态 ≥ 6.0:1
- 跨浏览器:Chrome 95+/Firefox 100+/Safari 15+ 的 `<select>` 展开均显示深底浅字

---

## 5. 关联规约(已写入 CLAUDE.md §26)

[CLAUDE.md §26 前端 UI 颜色对比度与可读性规范](../../CLAUDE.md)

涵盖:暗色主题对比度硬阈值、五大反模式、`<select>` 跨平台主题规约、状态徽章三档色相规约、选中态规约、阶段徽章规约、EconTier 缺失规约、验收 checklist、教训表。

---

## 6. 不修复(本次范围外)

- 棋类 / 斗地主 / 德州扑克界面同类问题——本次只扫狼人杀,后续按需扩展
- 移动端触控 44×44 token 已有 §23 规约,本次不重复审计
- 动效 / 动画 / 配色美学仅作为对比度验收的旁证,本次不重写设计 tokens