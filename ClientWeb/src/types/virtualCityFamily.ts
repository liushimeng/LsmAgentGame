/**
 * virtualCityFamily — 批次52 §20261002-01 代际财富转移引擎类型与常量（leaf 模块）。
 *
 * 拆自 types/virtualCity.ts（该文件超 §4 的 1800 行硬上限），纯类型/常量零依赖；
 * virtualCity.ts 再导出（export *），外部一律继续 `import ... from '@/types/virtualCity'`。
 * snake_case JSON 键与批次52 实施设计 §7 逐字对齐（backend 并行契约）。
 */

/** 父母段（批次52 §7 ParentsJSON）。health: good|fair|poor。 */
export interface VirtualCityFamilyParents {
  alive: boolean;
  age: number;
  health: string;
  /** 后端人读（zh 兜底）；i18n 展示以 health 枚举为准。 */
  health_cn: string;
}

/** 子女明细行（批次52 §7 KidJSON）。education: public|private。 */
export interface VirtualCityFamilyKid {
  age: number;
  education: string;
  /** 后端人读（zh 兜底）；i18n 展示以 education 枚举为准。 */
  education_cn: string;
}

/** 累计三项（批次52 §7 FamilyTotals，键名与后端逐字对齐）。 */
export interface VirtualCityFamilyTotals {
  support: number;
  education: number;
  child_received: number;
}

/** 房间级家庭汇总（批次52 §7 FamilyStatsJSON，观战者可见）。 */
export interface VirtualCityFamilyStats {
  parents_alive_count: number;
  children_count: number;
  private_edu_count: number;
}

/** 遗产继承事件（批次52 §7 InheritanceEventJSON；to_seats 恒空数组 = 板外继承）。 */
export interface VirtualCityInheritanceEvent {
  from_seat: number;
  to_seats: number[];
  amount_cny: number;
  month: number;
}

// ── 批次52 家庭动作与数值常量（P1-3 §5 / 批次52 §2 D11/D13）──────────────

/** 私立教育升级一次性费用（批次52 §3.2 PrivateEduInitCNY）。 */
export const WEALTH_PRIVATE_EDU_INIT_CNY = 200000;
/** 自愿加赡养上限 = NetWorth × 30%（批次52 §2 D13）。 */
export const WEALTH_FAMILY_SUPPORT_CAP_RATIO = 0.3;
/** 可升私立的子女年龄区间（批次52 §2 D11：年龄 ∈ [3,18] 且未私立）。 */
export const WEALTH_EDU_UPGRADE_MIN_AGE = 3;
export const WEALTH_EDU_UPGRADE_MAX_AGE = 18;

/**
 * 家庭动作载荷（game.virtual_city_action，批次52 §2 D13）。
 * 线上名与后端 actions.go 常量逐字对齐：ActUpgradeEducation="upgrade_education" /
 * ActPaySupportExtra="pay_support_extra"（D13 裁决的 ToolFamily sub-command 名）。
 * - upgrade_education：child_idx 合法（年龄∈[3,18] 且 public 且 Cash≥200000）→ 扣款置 private。
 * - pay_support_extra：自愿加赡养 amount_cny>0 且 ≤ NetWorth×30%。
 */
export type VirtualCityFamilyAction =
  | { type: 'upgrade_education'; child_idx: number }
  | { type: 'pay_support_extra'; amount_cny: number };
