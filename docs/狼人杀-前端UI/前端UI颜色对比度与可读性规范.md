# 前端 UI 颜色对比度与可读性规范

> **2026-08-08 §20260808-02 用户反馈响应**。狼人杀 13 人局在前端交付后被报告
> 存在多处「背景色与文字色对比度不足」/「选中态不可见」/「白底白字」「绿底绿字」
> 等可读性 / 美观缺陷。本文为通用规约,所有 Agent 在新增/修改狼人杀(以及未来
> 其他 4 款游戏)前端 UI 时**必须**遵循。
>
> **实战案例**:审计 + 修复 diff 见
> [`狼人杀13人局-前端UI颜色对比度审计报告-20260808-02.md`](狼人杀13人局-前端UI颜色对比度审计报告-20260808-02.md)
>
> **CLAUDE.md 引用**:在 §26 留有指向本文档的简短索引,本文是事实来源。

---

## 1. 暗色主题对比度硬阈值

| 元素类型 | WCAG 等级 | 最小对比度 | 项目内硬阈值 | 说明 |
|---|---|---|---|---|
| 正文 / 列表项 / 表单文字 | AA | 4.5:1 | **≥ 4.5:1** | 13 人局长时间盯屏,边缘可读性必须稳定 |
| 大字体(≥18pt / 14pt 加粗) | AA | 3:1 | **≥ 4.0:1** | 标题、徽章常用大字号,允许略放宽 |
| 状态徽章 / badge | AA | 4.5:1 | **≥ 5.0:1** | 13 个同屏刷屏时必须一眼分层 |
| selected/active 焦点态 | AAA | 7:1 | **≥ 6.0:1** | 选中后视觉权重必须显著高于未选 |

> **判据**:同一颜色色相 + 透明度叠加 + 同色系文字 = 必失败。设计师为保持
> 「低饱和度美学」常使用 `color: #d98c8c + background: rgba(180,92,92,0.22)`
> 这种同色相组合,在暗色背景上对比度普遍 ≤ 3.0:1,必须改为「白字 + 提高
> 背景透明度(≥0.45) + 加粗」。

## 2. 五大反模式(代码 review 时一票否决)

1. **「红底红字 / 绿底绿字 / 蓝底蓝字」** —— 同色相叠加再高透明度也没救。
   文字与背景**必须**分色相,或文字改白 + 背景 ≥ 45% 不透明。

2. **「浅色文字 + 25% 透明背景」** —— 暗色主题常见反模式。`rgba(255, 200, 100, 0.22)`
   + `color: #ffd76b` 实测约 4.0:1,刚好卡 AA 边缘。改成 `rgba(..., 0.45)` 起跳。

3. **「`color: inherit` + 半透明背景」** —— 父元素继承下来的浅色文字,在子
   元素半透明背景上对比度会进一步衰减(被叠加打散)。子元素必须显式声明
   `color`,且对比度按 **最终视觉合成色** 测算。

4. **「选中态与未选态差异 < 1.5 倍明度差」** —— 选中态必须:加宽 border +
   box-shadow 光晕 + 加粗 + 背景透明度 ≥ 1.5 倍未选态之一以上手段。夜间模式
   `is-night brightness(0.4)` 滤镜下会进一步压低所有透明度,选中态必须有
   box-shadow 这类不被滤镜衰减的视觉锚点。

5. **「`opacity` 表示 disabled」** —— 用 `opacity: 0.4` 会让选中态也变浅,
   与 disabled 冲突。disabled 应用单独的灰色 `background + color`,且
   `cursor: not-allowed`。

## 3. 原生 `<select>` 跨平台主题规约

原生 `<select>` 的渲染分两段,**关闭态与展开态由不同栈渲染**:

| 段 | 控制方 | 暗色主题风险 | 修复 |
|---|---|---|---|
| 关闭态(select 自身) | 我们自己的 CSS | 通常 OK | 设 `background + color` 即可 |
| 展开态(下拉弹出 + option) | 浏览器/操作系统 | **白底白字 / 系统主题不一致** | 见下方四管齐下 |

**修复四管齐下**(已应用于 `werewolf-modal.css` 的所有 select 控件):

```css
select {
  color-scheme: dark; /* A. 告诉浏览器用暗色主题 */
  appearance: none;
  background-image: url("data:image/svg+xml,..."); /* B. 自定义箭头 */
}
select option {
  background-color: #1f1f23; /* C. 显式深底浅字 */
  color: #f5f5f7;
  padding: 6px 10px;
}
select option:checked,
select option:hover,
select option:focus {
  background: linear-gradient(135deg, #8b5cf6, #6d28d9) !important;
  color: #ffffff !important;
  font-weight: 700;
}
```

- **A (`color-scheme: dark`)** —— Chrome 95+/Firefox 100+/Safari 15+ 会用
  暗色背景渲染下拉弹层,具体色由 UA 决定但不会是白底。
- **B (自定义箭头)** —— Firefox 默认黑色箭头在暗色 select 上看不见,自
  定义 SVG 保证跨浏览器一致。
- **C (option 深底浅字)** —— 即使 A 失效,option 颜色仍可控。
- **D (`:checked` / `:hover` 高亮)** —— 选中/悬停时紫底白字加粗,与未
  选对比 ≥ 6:1,选中态可见性高于未选。

**禁用态**: `select:disabled` 不要用 `opacity: 0.4`(会污染选中态判定),
应单独设 `opacity: 0.55 + cursor: not-allowed`。

## 4. 状态徽章三档色相规约

13 人局同屏刷屏时,徽章的色相分类比饱和度更重要。统一约定:

| 语义 | 推荐色相 | 底色透明度 | 文字色 | 字重 |
|---|---|---|---|---|
| 「我」(self) | 红 `#ff8888` 系 | ≥ 0.45 | `#ffb3b3` 或 `#ffffff` | 700 |
| 「警长」 | 金 `#ffd76b` 系 | ≥ 0.45 | `#ffe8a8` 或 `#ffffff` | 700 |
| 「白痴翻牌」 | 紫 `#a855f7` 系 | ≥ 0.55 | `#ffffff` | 700 |
| 「处决」 | 橙 `#ffb347` 系 | ≥ 0.7 | `#ffffff` | 700 |
| 「死亡」 | 灰 `#9ca3af` 系 | ≥ 0.45 | `#ffffff` | 700 |
| 「Bot AI」(聊天) | 青绿 `#14b8a6` 系 | ≥ 0.55 | `#a7f3d0` 或 `#ffffff` | 600 + text-shadow |
| 「法官」 | 金黄 `#f59e0b` 系 | ≥ 0.35 | `#8a6d1b` / `#fff` | 700 |
| 「quarantined/告警」 | 红 `#ef4444` 系 | ≥ 0.55 | `#ffffff` | 700 |

任何新的徽章 class 在写入前必须 grep 这张表,沿用色相,**不要新创第 9 种颜色**。

## 5. 选中态(`is-selected` / `is-active`)规约

任何可点击元素(座位 chip / 目标 button / tab)的选中态**必须同时满足**:

1. **背景透明度 ≥ 未选态的 1.5 倍**(例:未选 0.2,选中 ≥ 0.45)
2. **边框宽度加 1px**(例:`1px` → `2px`)
3. **`box-shadow: 0 0 8px <accent>` 光晕**——不被 night mode brightness 滤镜衰减
4. **`font-weight: 700`** —— 字重差异 ≥ 300

> 不要用 `outline` —— 在自定义 `:focus-visible` 体系里 outline 与 border 视觉重叠,
> 且 box-shadow 更可控(可独立调色)。

## 6. 阶段徽章 / 时间显示

`werewolf-table__phase` 等**全天可见的关键状态指示**,对比度优先于美学:

- 背景透明度 ≥ 0.7(例:`rgba(0,0,0,0.75)`),不要用 0.5 以下
- 文字用 `var(--fg)`(暖黄白) + 字重 600
- 边框 + 1px `var(--accent)`(金色),即使在 cinematic overlay 上也能识别

## 7. 经济档位 / 风险档位徽章缺失规约

> **历史教训**:PropPanel 的 `econ-tier-${econTier}` 类(health/caution/danger)
> 在 2026-08-08 之前**全部 src/styles/*.css 中零命中**(P1-4 缺陷),导致
> 5 档经济档位完全无视觉差异。

**新规约**:任何后端下发枚举值并在 JSX 中拼接为 className 的样式,**必须**
在同一次提交里完成 CSS 三件事:

1. JSX 拼接 className(如 `econ-tier-${econTier}`)
2. CSS 编写对应类规则(`econ-tier-health` / `caution` / `danger`)
3. CSS 添加 `@keyframes` + `prefers-reduced-motion` 兜底(脉动类动画)

**自检方法**:写完 JSX 后立即 `grep -rn "econ-tier-" ClientWeb/src/styles/`,
零命中即 P1 缺陷。

## 8. 验收 checklist(新增 UI 颜色改动前必过)

- [ ] 改动后用浏览器 devtools 的「Inspect → Accessibility → Contrast」检查
      主要文字(徽章、列表项、按钮)对比度 ≥ 4.5
- [ ] `<select>` 控件验证:Chrome / Firefox / Safari **三浏览器**展开下拉都
      是深底浅字,不是白底白字
- [ ] `prefers-reduced-motion: reduce` 下没有动画干扰对比度判定
- [ ] `is-selected` 选中态在 `.is-night brightness(0.4)` 滤镜下仍肉眼可辨
- [ ] 三个相邻徽章(我/警长/白痴 / Bot/法官/quarantined)色相区分清晰
- [ ] `tsc --noEmit` + `npm run build` 通过
- [ ] 在 `ClientWeb/src/styles/werewolf-*.css` 中 grep 新 className 至少
      一处命中(防止「声明了却从不接线」型静默失效)

## 9. 教训(避免重犯)

| 教训 | 反例 | 正例 |
|---|---|---|
| **「暗色 = 浅色字 + 任意深色底」即可**是错觉 | `rgba(45,212,191,0.38) + #0b6b63` | `rgba(13,148,136,0.55) + #a7f3d0` 或白字 |
| **同色相叠加** ≈ 自杀 | `#d98c8c` + `rgba(180,92,92,0.22)` | `#ffffff` + `rgba(180,92,92,0.7)` |
| **className 拼接但 CSS 缺失** = 静默失效 | `econ-tier-${econTier}` 零规则 | 三件套同步提交 |
| **opacity:0.4** 表达 disabled = 污染 selected | `disabled { opacity: 0.4 }` | `disabled { opacity: 0.55 + cursor: not-allowed }` |
| **box-shadow 没有 = 夜间模式必失明** | 选中只改背景 | 加 `box-shadow: 0 0 8px` 兜底 |
| **`<select>` 由 OS 接管** | 关闭态设暗底,以为完事 | 四管齐下(color-scheme + option + 箭头 + 选中态) |

## 10. 关联文档

- 实战案例 / 修复 diff:[`狼人杀13人局-前端UI颜色对比度审计报告-20260808-02.md`](狼人杀13人局-前端UI颜色对比度审计报告-20260808-02.md)
- 房间总运行时间 / 历史抽屉(§23):[`docs/通用功能/底部玩家布局设计.md`](../通用功能/底部玩家布局设计.md)
- 前端目录结构(§2.1 / §136):[`docs/狼人杀-前端UI/狼人杀13人局-前端代码结构优化-20260807-03.md`](狼人杀13人局-前端代码结构优化-20260807-03.md)
- 房间聊天(§16):[`docs/狼人杀-前端UI/Agent聊天显示优化-解决和设计方案-20260805-02.md`](Agent聊天显示优化-解决和设计方案-20260805-02.md)