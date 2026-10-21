/**
 * virtualCitySense — City-Human「感知」契约类型（see / hear / smell 三段式）。
 *
 * 拆分原因：CLAUDE.md §4「单文件 ≤ 1800 行」—— types/virtualCity.ts 已 1797 行触顶，
 * 批次 71 需同时补 L8 / L9 字段 + 校准段声明 ⇒ 本段类型迁出独立文件。
 * 导出路径保持不变：外部仍 `import { ... } from '@/types/virtualCity'`
 * （本文件被 virtualCity.ts `export *` 再导出）。
 *
 * 权威来源：后端 `ServerGo/game/virtual_city/room.go` 的 `SenseEntry` /
 * `SenseNeighborBrief`（JSON tag 逐字对齐）。
 */

/** 感知结果中的邻居摘要（来自已锚定的真实人物卡档案）。 */
export interface VirtualCityNeighborBrief {
  name: string;
  occupation?: string;
  /** 焦点层座位号（背景居民省略）。 */
  seat?: number;
  /** 人物卡号；档案未就绪时省略。 */
  card_id?: string;
  /**
   * ⚠ 批次71 L9 补齐：后端 `SenseNeighborBrief.Kind` 已下发
   *   （"seat" = 焦点层座位 / "resident" = 背景居民），前端此前未声明 ⇒ 恒 undefined。
   */
  kind?: 'seat' | 'resident';
  /** ⚠ 批次71 L9 补齐：邻居所属城区。 */
  district?: string;
  /** ⚠ 批次71 L9 补齐：情绪提示（人物卡锚定后的情绪描述）。 */
  mood_hint?: string;
}

/** see/hear/smell 工具的统一返回（确定性构造，JSON 键与后端 SenseEntry 逐字对齐）。 */
export interface VirtualCitySenseResult {
  /** 感知类型：see | hear | smell。 */
  kind?: 'see' | 'hear' | 'smell';
  district: string;
  /** see 专用：同区居民。 */
  people?: VirtualCityNeighborBrief[];
  /** see：挂牌/店铺/建筑。 */
  things?: string[];
  /** see/hear：本区本月事件。 */
  events?: string[];
  /** hear：近期公开发言摘录。 */
  utterances?: string[];
  /** smell：气味标签。 */
  smells?: string[];
  /** hear/smell 共用：环境声。 */
  sounds?: string[];
  /** ⚠ 批次71 L8 补齐：感知所属月（后端 `SenseEntry.Month` 已下发）。 */
  month?: number;
  /** ⚠ 批次71 L8 补齐：人读摘要文本（后端 `SenseEntry.Text`，≤160 字）。 */
  text?: string;
}
