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
  /** 红线 id：R-E3 / R-E4 / R-E5 / R-E6 / R-B1（顺序固定）。 */
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
  /** 红线判定（恒 5 条，顺序固定 R-E3/R-E4/R-E5/R-E6/R-B1）。 */
  redlines?: VirtualCityCalibrationRedline[];
  /** 最近 12 月历史（**恒为数组**，可能为空）。 */
  history?: VirtualCityCalibrationHistoryEntry[];
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
};
