# 财商流游戏 · 人物卡字段字典（Schema v1.0）

> 适用对象：`docs/财商流游戏/玩家职业设计/<L1>/<L2>/<L3>/<L4>/<L5>/<编号>-<姓名>.md`
> 所有人物卡 **必须** 遵循本字典。字段增删须升 `schema_version` 并回填全库。

---

## 1. 文件骨架

```markdown
---
<11 组 YAML 字段（见 §2）>
_legacy_ids: [<旧编号>…]
_completeness: 0.95
_grounded: 0.54
_sources: {explicit: 32, derived: 20, assigned: 4, missing: 3}
_missing: [<缺失字段名>…]
_raw: {src_line: '<原档行原文>'}
---

# <姓名> · <性别> · <年龄> 岁 · <职业>
> **一句话画像**：…
| 项 | 值 |  ← 行业三级分类 / 收入 / 健康档 / 完整度速览
## 1. 基础档案
## 2. 家庭与抚养
## 3. 职业与收入
## 4. 财务快照
## 5. 健康与压力
## 6. 情感与人格
## 7. 人生目标与机会
## 8. 开局钩子
## 9. 数据溯源
```

**正文 9 个小节的标题与顺序固定**，不得增删或改序；小节内条目可缺（见 §4）。

---

## 2. 字段清单（59 个计数字段 · 11 组）

### 0. 元数据 meta（6）

| 字段 | 类型 | 含义 | 来源 |
|---|---|---|---|
| `id` | str | 全局唯一卡号，`N` + 数字；重号冲突者改入 `N9000000+` 保留段 | 生成器 |
| `name` | str | 2–4 字化名，全局唯一 | 原档 / 重建 |
| `schema_version` | str | 固定 `"1.0"` | 生成器 |
| `card_type` | enum | `person` 普通人物 / `archetype` 原型卡（P01–P18） | 生成器 |
| `richness` | enum | `skeleton` 机械映射 / `rich` SubAgent 富化 | 生成器 |
| `source_file` | str | 原始档案文件名（含 `.md`） | 生成器 |

### 1. 身份 identity（8）

| 字段 | 类型 | 含义 |
|---|---|---|
| `gender` | `男`/`女` | 来源：原档显式 → 配偶称谓 → 名字用字 → 哈希分配 |
| `age` | int | 岁；原档缺失时按职业阶段 + 编号哈希分配 |
| `age_band` | enum | `16-24`/`25-34`/`35-44`/`45-54`/`55-64`/`65+` |
| `birth_year` | int | `2026 - age` |
| `birth_province` | str | 籍贯省（详档型有「籍贯」时优先） |
| `birth_city` | str | 常住城市 |
| `generation` | enum | `40后`…`10后` |
| `education` | enum | 博士/硕士/本科/大专/中专/高中/初中/MBA/EMBA |

### 2. 健康 health（3）

| 字段 | 类型 | 含义 |
|---|---|---|
| `health_grade` | `A`/`B`/`C` | A 稳定 · B 可控慢病或劳损 · C 重大风险 |
| `health_conditions` | list[str] | 具体病症/身体状况（如 `腰椎`、`应酬肝`） |
| `health_risks` | list[str] | 由病症映射的职业健康风险（久站负重 / 应酬饮酒 …） |

### 3. 职业 career（8）

| 字段 | 类型 | 含义 |
|---|---|---|
| `industry_l1` | str | ICG 一级码 `A`–`Z` |
| `industry_l2` | str | ICG 二级码，如 `R01` |
| `industry_l3` | str | ICG 三级码，如 `R0102` |
| `occupation` | str | **原档职业串**（去除薪资括号后），逐字保留 |
| `employment` | enum | 全职/个体经营/自由职业/平台就业/灵活就业/退休返聘 |
| `employer` | str | 用人单位（模板生成） |
| `work_intensity` | map | `{weekly_hours, overtime, risk}` |
| `career_stage` | enum | 入门/初级/骨干/资深/管理 |

### 4. 收入 income（5）

| 字段 | 类型 | 含义 |
|---|---|---|
| `income_monthly` | int | 个人月收入（元） |
| `income_range` | [int,int] | 原档区间；无区间时 `[月收入×0.85, 月收入×1.2]` |
| `income_structure` | enum | 固定薪资/提成+底薪/项目制/季节波动/计件/年薪制/合伙分红 |
| `income_stability` | enum | 高/中/低 |
| `household_monthly` | int | 家庭月收入（配偶收入可抽时相加，否则按婚姻状态乘系数） |

### 5. 财务 finance（7）

| 字段 | 类型 | 含义 |
|---|---|---|
| `monthly_expense` | int | 月支出（元） |
| `savings_stock` | int | 储蓄存量（元） |
| `debt_stock` | int | 负债存量（元） |
| `savings_rate` | float | `(月收入 − 月支出) / 月收入` |
| `net_worth` | int | `储蓄存量 − 负债存量` |
| `debts` | list[map] | `{type, balance, monthly, source}` |
| `assets` | list[map] | `{type, desc, value_cny}` |

### 6. 保障 protection（3）

| 字段 | 类型 | 含义 |
|---|---|---|
| `certs` | list[str] | 学历外的证书/资质 |
| `funds` | list[str] | 公积金等长期基金 |
| `insurance` | list[str] | 医保/养老/商业险 |

### 7. 家庭 family（6）

| 字段 | 类型 | 含义 |
|---|---|---|
| `marital` | enum | 已婚/未婚/离异/丧偶 |
| `children_count` | int | 子女数 |
| `children_ages` | list[int] | 子女年龄 |
| `elders_dependent` | int | 需赡养老人数 |
| `household_type` | enum | 三明治家庭/核心家庭/赡养家庭/新婚·丁克家庭/单人家庭 |
| `family_role` | enum | 主要经济支柱/共同经济支柱/辅助经济来源 |

### 8. 住房 housing（4）

| 字段 | 类型 | 含义 |
|---|---|---|
| `housing_tenure` | enum | 自有/自建/租赁/合租/单位·保障住房/父母产权/未知 |
| `housing_city` | str | 常住城市 |
| `housing_detail` | str | 原档住房描述原文 |
| `mortgage_left` | int/null | 房贷余额（元） |

### 9. 心理 psych（4）

| 字段 | 类型 | 含义 |
|---|---|---|
| `emotion_status` | str | 情感状况 |
| `stress_level` | enum | 高/中/低（由压力源条数判定） |
| `stress_sources` | list[str] | 压力源，取自原档「财商画像」列 |
| `biases` | list[str] | 行为金融偏差（见 §5 词典） |

### 10. 目标 goals（3）

| 字段 | 类型 | 含义 |
|---|---|---|
| `goals_short` | list[map] | `{horizon, text}`；原档有「目标/开局事件」时取原文 |
| `opportunities` | list[str] | 机会/应对 |
| `dream_cost` | int | 梦想成本估算（元） |

### 11. 出行 transport（2）

| 字段 | 类型 | 含义 |
|---|---|---|
| `transport_owned` | str | 自有车（中高端/经济型）/电动车摩托车/营运车辆/公共交通为主 |
| `transport_mode` | enum | 自驾/两轮车/营运车辆/公共交通 |

---

## 3. 来源标注（`_sources`）

**每个字段都必须能归入以下四类之一**，并在 `_sources` 里计数：

| 标注 | 含义 | 举例 |
|---|---|---|
| `explicit` | 原档直接可得，**逐字保留** | `income_monthly`、`health_conditions`、`occupation` |
| `derived` | 可由原档字段**唯一确定**地推导 | `age_band`（由 age）、`savings_rate`（由收入支出）、`health_risks`（由病症） |
| `assigned` | 原档无据，按**确定性规则**（哈希/查表/模板）分配的**合成值** | `age`（缺失时）、`gender`（无任何信号时）、`transport_*`、`dream_cost` |
| `missing` | 无任何数据源，如实置空 | `children_ages`、`mortgage_left`、`biases` |

> **诚实性底线**：`assigned` 是合成值，**永不与 `explicit` 混同**。
> 引用本知识库做数值校准时，若需真实数据支撑，请只采信 `explicit` 字段
> —— 即 `_grounded` 比率所指代的部分。

### 两个完整度指标

| 指标 | 公式 | 含义 |
|---|---|---|
| `_completeness` | `(59 − 缺失数) / 59` | **字段填充率**，硬约束 ≥ 0.80 |
| `_grounded` | `有据字段数 / 59` | **有据率**（仅 `explicit`），如实披露、无硬约束 |

---

## 4. 缺失约束

> **硬约束**：每张卡 `_completeness ≥ 0.80`，即缺失字段 **≤ 11 个（≤ 20%）**。
> 允许不同人物因原始数据丰俭而缺不同字段 —— 这正是「人的差异」的一部分。

差异化的具体表现举例：

- 详档型人物（P01–P18 及 1794 张详档卡）通常 `_grounded` 更高，`goals_short` / `health_conditions` 来自原文；
- 表格型人物 `transport_*`、`biases` 多为缺或分配；
- 婚姻/子女字段在原档为「未婚」时的 `children_ages` 必然缺失 —— 属于**语义性缺失**，不计为数据质量问题。

---

## 5. 行为金融偏差词典（`biases` 取值）

| 偏差 | 触发关键词（原档「财商画像」列） |
|---|---|
| 损失厌恶 | 损失 / 亏损 / 怕亏 / 割肉 |
| 过度自信 | 自信 / 乐观 / 侥幸 / 赌 / 执念 |
| 锚定效应 | 锚定 / 锚 / 参照 / 原价 |
| 羊群效应 | 跟风 / 羊群 / 从众 / 跟买 |
| 心理账户 | 心理账户 / 专款 / 挪用 |
| 禀赋效应 | 禀赋 / 舍不得 / 惜售 |
| 现状偏见 | 现状 / 惯性 / 路径依赖 / 稳定偏好 |
| 确认偏误 | 确认偏误 / 只听 / 选择性 / 信息茧房 |
| 沉没成本 | 沉没 / 已投入 / 回本 |
| 即时满足 | 即时 / 拖延 / 月光 / 冲动 / 精致穷 |
| 风险厌恶 | 保守 / 不敢 / 稳健 |
| 风险寻求 | 激进 / 杠杆 / 加仓 / 豪赌 |
| 心理韧性 | 韧性 / 抗压 / 坚韧 |
| 情感劳动 | 情感透支 / 情感劳动 / 情绪劳动 |

---

## 6. 合法取值枚举（生成器强校验）

- `gender` ∈ {男, 女}
- `health_grade` ∈ {A, B, C, null}
- `age_band` ∈ {16-24, 25-34, 35-44, 45-54, 55-64, 65+, null}
- `industry_l1` ∈ A–Z 共 26 个字母
- `industry_l2` ∈ `03-行业分类体系-ICG.md` 中的合法码，且首字母 == `industry_l1`
- `housing_tenure` ∈ {自有, 自建, 租赁, 合租, 单位/保障住房, 父母产权, 未知}
- `household_type` ∈ {三明治家庭, 核心家庭, 赡养家庭, 新婚/丁克家庭, 单人家庭}
- `income_structure` ∈ {固定薪资, 提成+底薪, 项目制, 季节波动, 计件, 年薪制, 合伙分红}

---

## 7. 维护规约

1. **不改已有字段语义** —— 只允许新增字段（升 `schema_version`）。
2. **不手工编辑** 卡片文件 —— 一律改 `scripts/财商流游戏知识库/` 下的解析器/推导器后全量重建。
3. **`_raw.src_line` 不得删除** —— 它是「信息零丢失」的可回溯凭据。
4. 富化（`richness: rich`）只允许**填写已有字段**与补写正文小节，**不得新增未在 §2 登记的字段**。
