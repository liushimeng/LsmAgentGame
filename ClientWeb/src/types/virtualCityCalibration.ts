/**
 * virtualCityCalibration — 批次 71「校准与验证框架」前端契约类型。
 *
 * 事实来源：`lag_docs/虚拟城市/已实现/71-校准与验证框架落地/01-方案设计.md` §9
 * （`game.state.calibration` JSON 形状）与后端
 * `ServerGo/game/virtual_city/calibration.go`（InvariantCheck / RedlineHit /
 * CalibrationIndicators / MonthlyAudit）。
 *
 * ⚠ **字段名逐字对齐后端 JSON tag**（§9.3）：本文件的 snake_case 名字**不可改**，
 *   改了前端就恒读不到值（与批次 70 的 `wealth_pct` / `wealth_score` 同类事故）。
 *
 * ⚠ **整体可能不存在**：老对局 / 后端未升级 / 房间未跑满首月 ⇒
 *   `game.state.calibration` 整个键缺省 ⇒ 面板必须走空态（不得白屏、不得报错）。
 *
 * ⚠ **可空性**：契约要求 `invariants[]` / `redlines[]` / `history[]` / `indicators`
 *   恒存在，但前端按**可选**处理（老后端 / 截断帧 / 字段缺失都不得崩）——
 *   故数组与 indicators 在本类型面上声明为可选，渲染层用 `?? []` / `?? null` 兜底。
 *
 * 批次 72 追加（方案 §8.1，全部**可选键**，批次 71 老对局缺省不炸）：
 *   - `category_annualized`：市场级品类年化（小数年化，如 0.081 ⇒ 前端 ×100 展示）。
 *   - `invariants[].no_sample`：仅 I6 可能为 true（passed=true 且无样本可验）。
 *   - `runs[]`：最近 12 局完成运行（新→旧；`strategy` 为 "" 表示真实对局）。
 */

/** 单条回归不变量检查结果（后端 InvariantCheck）。 */
export interface VirtualCityCalibrationInvariant {
  /** 不变量 id：I1 / I2 / I3 / I4 / I5 / I6 / I8（**没有 I7**，顺序固定）。 */
  id: string;
  /** 后端兜底中文名（前端优先用 i18n；未知 id 回退到此字段）。 */
  name: string;
  /** 是否通过。 */
  passed: boolean;
  /** 严重级：fatal（致命） / serious（严重）。 */
  severity: string;
  /** 实测值；不适用的检查为 null。 */
  actual?: number | null;
  /** 期望值；不适用的检查为 null。 */
  expected?: number | null;
  /** 中文说明（后端权威口径文案）。 */
  detail: string;
  /**
   * 无样本位（批次 72）：仅 I6 可能为 true —— 本局无场景注入 ⇒ 无样本可验，
   * 后端仍判 passed=true（无违例事实）⇒ 前端渲染灰色「无样本」徽章
   * （不是 ✔ 也不是 ✘）。其余 6 条恒 false / 缺省。
   */
  no_sample?: boolean;
}

/** 校准指标集合（后端 CalibrationIndicators）。值可能为 null（口径未就绪）。 */
export interface VirtualCityCalibrationIndicators {
  /** D2 货币扩张倍数（总资产 ÷ 期初总现金；与央行 M2 口径**不同**）。 */
  money_expansion?: number | null;
  /** D5 破产率（0-1 小数）。 */
  bankruptcy_rate?: number | null;
  /** 净资产基尼系数（0-1）。 */
  gini?: number | null;
  /** 失业率（0-1 小数）。 */
  unemployment?: number | null;
  /** 居民消费价格指数（基期 1.0）。 */
  cpi?: number | null;
  /** 居民总财富（元）。 */
  total_wealth?: number | null;
  /** 存活座位数。 */
  alive_seats?: number | null;
  /** 总座位数。 */
  total_seats?: number | null;
  /** 账外调整净额（元；正 = 净增）。 */
  off_ledger_cny?: number | null;
}

/** 单条红线判定（后端 RedlineHit）。 */
export interface VirtualCityCalibrationRedline {
  /**
   * 红线 id：R-E3 / R-E4 / R-E5 / R-E6 / R-B1（顺序固定）+ 批次 72 新增
   * R-E1 / R-E2 追加在尾部。⚠ R-E1/R-E2 **前端不映射名称**（批次 72 契约：
   * 直接用后端 `name` 中文名），未知 id 走 `CALIBRATION_REDLINE_KEYS`
   * 未命中 ⇒ 回退后端 name 的既有兜底路径。
   */
  id: string;
  /** 后端兜底中文名（前端优先用 i18n；未知 id 回退到此字段）。 */
  name: string;
  /** 是否越界触发。 */
  triggered: boolean;
  /** 实测值。 */
  actual?: number | null;
  /** 阈值。 */
  threshold?: number | null;
  /** 中文说明。 */
  detail: string;
}

/** 历史审计条目（最近 12 月）。 */
export interface VirtualCityCalibrationHistoryEntry {
  /** 审计月份。 */
  month: number;
  /** 该月引擎状态哈希（16 位十六进制）。 */
  hash: string;
  /** 该月不变量是否全部通过。 */
  ok: boolean;
}

/**
 * 单条运行历史记录（后端 RunRecord，批次 72；JSONL 落盘同构）。
 *
 * ⚠ `strategy` 为后端策略 id："" = 真实对局；5 个蒙特卡洛策略 id 见
 *   `CALIBRATION_STRATEGY_KEYS`。未知 id 渲染层回落**原字符串**。
 */
export interface VirtualCityCalibrationRun {
  /** 运行 id（如 CR3）。 */
  run_id: string;
  /** 本局种子。 */
  seed: number;
  /** 策略 id（"" = 真实对局）。 */
  strategy: string;
  /** 本局已推进月数（总步数）。 */
  total_steps: number;
  /** D2 货币扩张倍数（终局口径）。 */
  money_expansion: number;
  /** D5 破产率（0-1 小数，终局口径）。 */
  bankruptcy_rate: number;
  /** 结局计数（键 = 结局 id，值 = 座位数）。 */
  outcome_counts?: Record<string, number>;
  /** 未通过的不变量 id 列表。 */
  invariant_fails?: string[];
  /** 触发的红线 id 列表。 */
  redlines_triggered?: string[];
  /** 终局引擎状态哈希（16 位十六进制）。 */
  state_hash: string;
  /** 完成时间（unix 秒）。 */
  finished_at: number;
}

/** game.state.calibration —— 校准与验证快照（批次 71）。 */
export interface VirtualCityCalibration {
  /** 审计快照所属月（= 当前 game.state.month）。 */
  month: number;
  /** 引擎状态哈希（16 位十六进制；FNV-1a 64）。 */
  state_hash: string;
  /** 回归不变量（恒 7 条，顺序固定 I1/I2/I3/I4/I5/I6/I8）。 */
  invariants?: VirtualCityCalibrationInvariant[];
  /** 校准指标。 */
  indicators?: VirtualCityCalibrationIndicators;
  /** 红线判定（批次 72 起 7 条：R-E3/R-E4/R-E5/R-E6/R-B1 + 尾部 R-E1/R-E2）。 */
  redlines?: VirtualCityCalibrationRedline[];
  /** 最近 12 月历史（**恒为数组**，可能为空）。 */
  history?: VirtualCityCalibrationHistoryEntry[];
  /**
   * 市场级品类年化收益（批次 72，R-E1/R-E2 判定输入）。键：stock_index /
   * gold / house / bond；值为**小数年化**（0.081 = 8.1%）。月数 < 12 的局
   * 或老后端缺省 ⇒ 前端空态（'—'），不得白屏。
   */
  category_annualized?: Record<string, number>;
  /** 最近 12 局完成运行（新→旧；批次 72）。老对局 / 无终局记录缺省。 */
  runs?: VirtualCityCalibrationRun[];
}

/**
 * 不变量 id → i18n 键（§12 纪律：后端新增枚举值必须有前端标签表）。
 *
 * ⚠ **漏项即白屏/裸 key**：本表未命中的 id ⇒ 渲染层回退**后端 `name` 字段**
 *   （绝不显示裸 undefined）。
 */
export const CALIBRATION_INVARIANT_KEYS: Record<string, string> = {
  I1: 'virtualCity.calib.inv.i1',
  I2: 'virtualCity.calib.inv.i2',
  I3: 'virtualCity.calib.inv.i3',
  I4: 'virtualCity.calib.inv.i4',
  I5: 'virtualCity.calib.inv.i5',
  I6: 'virtualCity.calib.inv.i6',
  I8: 'virtualCity.calib.inv.i8',
};

/**
 * 红线 id → i18n 键（同上纪律）。
 *
 * ⚠ 红线 id 含短横线（`R-E3`）⇒ 用普通对象键表，**不要**写进 i18n 键名。
 */
export const CALIBRATION_REDLINE_KEYS: Record<string, string> = {
  'R-E3': 'virtualCity.calib.redline.re3',
  'R-E4': 'virtualCity.calib.redline.re4',
  'R-E5': 'virtualCity.calib.redline.re5',
  'R-E6': 'virtualCity.calib.redline.re6',
  'R-B1': 'virtualCity.calib.redline.rb1',
  // ⚠ R-E1 / R-E2（批次 72）**刻意不映射**：契约规定红线名直接用后端 name
  //   中文名；未命中本表 ⇒ 渲染层回退 rl.name（既有兜底路径），勿补键。
};

/**
 * 策略 id → i18n 键（批次 72）。"" = 真实对局。
 *
 * ⚠ **漏项回落原字符串**（[[i18n-map-miss-blanks-panel]] 教训）：后端新增
 *   策略 id 时本表未命中 ⇒ 渲染层显示原始 id 字符串，绝不渲染空白。
 */
export const CALIBRATION_STRATEGY_KEYS: Record<string, string> = {
  '': 'virtualCity.calibration.strategy.live',
  safety_net_only: 'virtualCity.calibration.strategy.safety_net_only',
  minimum_effort: 'virtualCity.calibration.strategy.minimum_effort',
  balanced: 'virtualCity.calibration.strategy.balanced',
  aggressive_leverage: 'virtualCity.calibration.strategy.aggressive_leverage',
  stress: 'virtualCity.calibration.strategy.stress',
};

/**
 * 品类年化键 → i18n 键（批次 72）。品类名复用既有资产/宏观标签键，
 * 未在 §8.3 的 21 键清单内 ⇒ 不新增键。未知品类 ⇒ 回落原始键字符串。
 */
export const CALIBRATION_CATEGORY_KEYS: Record<string, string> = {
  stock_index: 'virtualCity.stockIndex',
  gold: 'virtualCity.asset.gold',
  house: 'virtualCity.macro.spreadHouse',
  bond: 'virtualCity.asset.bond',
};

/** 品类年化固定展示顺序（与后端计算口径一一对应，见方案 §6.2）。 */
export const CALIBRATION_CATEGORY_ORDER: readonly string[] = [
  'stock_index',
  'gold',
  'house',
  'bond',
];
