/**
 * virtualCityBehavior — 批次 62「居民行为金融与泡沫反身性引擎」前端类型。
 *
 * 独立成文件（不塞进 `types/virtualCity.ts`）：该文件正被批次 61「全城连续正交方格路网」
 * 并行修改（同文件并发编辑易冲突），且 §2.1 要求「游戏私有代码留在游戏目录内」。
 *
 * 权威来源：
 *   - 设计：`lag_docs/虚拟城市/已实现/62-居民行为金融与泡沫反身性引擎/01-方案设计.md`
 *   - 实际产出：`ServerGo/game/virtual_city/view.go`（BubbleSnapshot / LiquiditySpreadSnapshot /
 *     BehaviorJSON / BiasProfileJSON / BiasImmunityJSON / DeferredPaymentJSON / MentalAccountJSON）
 *
 * ⚠ **教学闭环不变量（不可在前端绕过）**：认知检定未通过（`detected === false`）的偏差，
 *   后端已把 `name_cn` / `effect` / `explanation` **置空**。前端**必须**原样显示占位文案，
 *   **绝不能**用 `code` 反查中文名 —— 否则「玩家到复盘才知道自己踩了什么坑」的设计失效。
 */

/** 泡沫状态机（批次 62 §5.2；与既有市场四阶段周期**正交**）。 */
export type VirtualCityBubbleState = 'none' | 'overheat' | 'burst' | 'recession';

/** game.state.bubble —— 集体杠杆泡沫快照。 */
export interface VirtualCityBubbleSnapshot {
  /** none=常态 / overheat=信贷过热 / burst=破裂 / recession=泡沫后衰退。 */
  state: VirtualCityBubbleState;
  /** 市场杠杆率 = 全体居民总负债 ÷ 总净资产（> 0.60 触发信贷过热）。 */
  leverage_ratio: number;
  /** 资产换手率 = 本回合成交资产数 ÷ 全体持有资产数（> 0.30 触发投机过热）。 */
  turnover_ratio: number;
  /** 现金占比 = 全体现金 ÷ 总资产（< 0.10 触发流动性紧张）。 */
  cash_ratio: number;
  /** 当前状态已持续月数。 */
  months_in_state: number;
  /** 破裂播报原文（仅 burst 时非空；可能很长）。 */
  message: string;
}

/** game.state.liquidity_spreads —— 资产流动性买卖价差表（§7.2 + §7.3 周期修正）。 */
export interface VirtualCityLiquiditySpreadSnapshot {
  /** 股票 2% 基准。 */
  stock: number;
  /** 债券 / 理财 3% 基准。 */
  bond: number;
  /** 贵金属 5% 基准。 */
  gold: number;
  /** 房产 10% 基准。 */
  house: number;
  /** 企业股权 20% 基准。 */
  equity: number;
  /** 收藏品 / 另类 25% 基准。 */
  collectible: number;
  /** 市场周期修正系数（繁荣 ×0.7 / 衰退 ×1.8）。 */
  phase_multiplier: number;
}

/** 偏差类别（后端 Hook 分组：entry=买入 / exit=卖出 / spend=消费 / invest=投入）。 */
export type VirtualCityBiasCategory = 'entry' | 'exit' | 'spend' | 'invest' | string;

/** game.state.my.behavior.bias_profile[] —— 单条行为偏差。 */
export interface VirtualCityBiasProfileEntry {
  /** 偏差内部 key（**仅作 React key / 内部比较用，不可据此反查展示文案**）。 */
  code: string;
  /** 中文名；**未识破时后端置空** ⇒ 前端显示「未识破的倾向」占位。 */
  name_cn: string;
  /** 偏差类别。 */
  category: VirtualCityBiasCategory;
  /** 识破所需的认知点阈值。 */
  threshold: number;
  /** 认知检定是否通过（false ⇒ 后端已隐藏名称与效果）。 */
  detected: boolean;
  /** 生效效果描述（**检定后的有效乘子**；未识破 / 已免疫时后端置空或标注免疫）。 */
  effect: string;
  /** 一句话学术解释（未识破时置空）。 */
  explanation: string;
}

/** game.state.my.behavior.bias_immunity[] —— 单类偏差免疫进度（0~1）。 */
export interface VirtualCityBiasImmunityEntry {
  code: string;
  /** 0~1；≥0.80 ⇒ 该偏差免疫（本回合不生效）。 */
  progress: number;
}

/** game.state.my.behavior.deferred_payments[] —— 延期支付明细（双曲贴现）。 */
export interface VirtualCityDeferredPaymentRow {
  /** 本金（元）。 */
  amount_cny: number;
  /** 到期月序。 */
  due_month: number;
  /** 罚息率（0.3 = ×1.3）。 */
  penalty_rate: number;
}

/** game.state.my.behavior.mental_accounts[] —— 资金来源标签余额（不影响购买力）。 */
export interface VirtualCityMentalAccountRow {
  /** 标签 key（salary / investment / side / windfall / gift / refund）。 */
  tag: string;
  /** 标签中文名。 */
  label_cn: string;
  /** 该标签余额（元）。 */
  amount_cny: number;
}

/** game.state.my.behavior —— 本人行为画像快照（仅本人座位下发）。 */
export interface VirtualCityBehaviorSnapshot {
  /** 认知点。 */
  cognition: number;
  /** 心魔画像（2~3 条；生效中的偏差）。 */
  bias_profile: VirtualCityBiasProfileEntry[];
  /** 各类偏差的免疫进度。 */
  bias_immunity: VirtualCityBiasImmunityEntry[];
  /** 锚定指数（越高说明越受买入价牵制）。 */
  anchoring_index: number;
  /** 羊群指数（跟风次数 ÷ 全部决策次数）。 */
  herding_index: number;
  /** 连续正储蓄月数。 */
  savings_streak_months: number;
  /** 延期支付明细。 */
  deferred_payments: VirtualCityDeferredPaymentRow[];
  /** 延迟满足指数（0~1，年度结算用）。 */
  delayed_pay_index: number;
  /** 心理账户标签余额。 */
  mental_accounts: VirtualCityMentalAccountRow[];
  /** 心理账户是否已合并（合并后冲动放大消失）。 */
  mental_account_merged: boolean;
  /** 本月最后一次反思档位：none / light / deep / preview。 */
  last_reflect: 'none' | 'light' | 'deep' | 'preview' | string;
}

/** 反思档位（后端 `reflect_mode`）。 */
export type VirtualCityReflectMode = 'light' | 'deep' | 'preview';

/** 批次 62 新增的三个动作（后端 `ApplyAction` 已支持）。 */
export type VirtualCityBehaviorAction =
  | { type: 'reflect'; reflect_mode: VirtualCityReflectMode }
  | { type: 'defer_payment'; amount_cny: number; reason?: string }
  | { type: 'merge_accounts' };
