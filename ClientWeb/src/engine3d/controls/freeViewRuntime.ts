/**
 * engine3d/controls/freeViewRuntime — 自由视角的**实时量**单例（非 React）。
 *
 * 批次 67「视觉系统优化 · 抬头自由」新增。
 *
 * 为什么不用 zustand（`freeViewStore.ts`）：那份 store 的写入纪律明确写着「只有低频量
 * （模式 / 帮助开关 / 档位 / 碰撞态）进 store；每帧变化的量一律走 ref」—— 仰角是
 * **每帧**变化的量，进 store 会让不相关的 HUD 订阅者被 60 fps 拖着重渲染。
 * 故独立成一个纯数据单例：控制器每帧**就地覆写**（零分配、零 React 通知），
 * DOM 层的 `<FreeViewHud>` 以 ~8 Hz 轮询读取并渲染。
 *
 * 引擎层不 import 任何游戏私有模块（CLAUDE.md §2.1 硬约束 5）：本文件零游戏依赖。
 */

import type { FreeViewMode } from './freeViewStore';

export interface FreeViewRuntime {
  /**
   * 视线**绝对仰角**（度）：+ = 朝上（天顶），− = 朝下（地面）。
   * 由相机实际世界方向反解 —— 已含 `lookPitch` 抬头偏移与飞行模式的自由 pitch。
   */
  lookAngleDeg: number;
  /**
   * 视线**俯仰偏移**（度，相对锚定基朝向）：仅锚定类模式（orbit / fly-选中）非零，
   * 自由飞行模式恒为 0（该模式的 pitch 本身就是自由视线）。
   */
  lookPitchDeg: number;
  /** 当前视角模式。 */
  mode: FreeViewMode;
}

/** 实时量单例。字段就地覆写，勿替换整个对象（消费方可能持有引用）。 */
export const freeViewRuntime: FreeViewRuntime = {
  lookAngleDeg: 0,
  lookPitchDeg: 0,
  mode: 'orbit',
};

/** 把控制器算出的值写进单例（每帧调用，零分配）。 */
export function publishFreeViewRuntime(
  mode: FreeViewMode,
  lookAngleDeg: number,
  lookPitchDeg: number,
): void {
  freeViewRuntime.mode = mode;
  freeViewRuntime.lookAngleDeg = lookAngleDeg;
  freeViewRuntime.lookPitchDeg = lookPitchDeg;
}
