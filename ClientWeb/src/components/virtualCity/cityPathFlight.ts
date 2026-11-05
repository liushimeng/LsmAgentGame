/**
 * cityPathFlight —— 显示指令 `fly_path` 的折线飞行（游戏侧薄适配层）。
 *
 * 批次 84：`fly_path` 带 2–8 个 `{x, z}` 点，语义是「相机依次飞过这些点」。
 *
 * 实现方式（**不新增引擎能力**）：既有 `<FreeViewControls>` 的 `focusRef`
 * 通道本就是「外部请求平滑移焦」（写入后场景按阻尼把轨道聚焦点推向目标，
 * 到位自动清空）⇒ 只需按节奏依次写入各点，引擎自带的阻尼插值就形成了
 * 连续飞行。引擎层零改动（§2.1 约束 5：引擎不感知游戏语义）。
 *
 * 飞行只在 **orbit** 模式生效（focusRef 在 fly 模式被控制器忽略）⇒ 调用方
 * 需先切到 orbit。
 */

import type { MutableRefObject } from 'react';

/** 折线上的一个点（世界坐标）。 */
export interface CityPathPoint {
  x: number;
  z: number;
}

/** 折线点数上限（后端 wire 契约：≤ 8 点）。 */
export const CITY_PATH_MAX_POINTS = 8;
/** 折线点数下限（少于 2 点不构成路径）。 */
export const CITY_PATH_MIN_POINTS = 2;
/** 每个航点的停留时长（毫秒）—— 引擎阻尼约 0.3s 到位，留出观察余量。 */
const DWELL_MS = 1100;

/** 当前飞行任务的定时器句柄（新一轮飞行会取消上一轮）。 */
let timer: ReturnType<typeof setTimeout> | null = null;

/** 取消进行中的飞行（换房 / 新指令 / 卸载）。 */
export function cancelPathFlight(): void {
  if (timer !== null) {
    clearTimeout(timer);
    timer = null;
  }
}

/**
 * 启动一次折线飞行。
 *
 * @param points 航点（2–8 个；越界返回 false）
 * @param focusRef 与页面共用的相机聚焦 ref
 * @returns 是否成功启动
 */
export function flyPath(
  points: CityPathPoint[] | undefined,
  focusRef: MutableRefObject<{ x: number; z: number } | null>,
): boolean {
  cancelPathFlight();
  if (!Array.isArray(points)) return false;
  const valid = points.filter(
    (p) => p && Number.isFinite(p.x) && Number.isFinite(p.z),
  );
  if (valid.length < CITY_PATH_MIN_POINTS) return false;
  const seq = valid.slice(0, CITY_PATH_MAX_POINTS);

  let i = 0;
  const step = () => {
    if (i >= seq.length) {
      timer = null;
      return;
    }
    const p = seq[i];
    i += 1;
    focusRef.current = { x: p.x, z: p.z };
    timer = setTimeout(step, DWELL_MS);
  };
  step();
  return true;
}

/** 供非 React 代码（adapter）直接调用。 */
export const cityPathApi = {
  flyPath,
  cancel: cancelPathFlight,
};
