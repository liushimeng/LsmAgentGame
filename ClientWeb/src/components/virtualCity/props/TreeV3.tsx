/**
 * TreeV3 —— **批次 44 起：本文件只剩「程序化降级树的形态来源」，组件本体已删除**。
 *
 * 保留的导出全部服务于 `TreesInstanced` 的降级链（GLB 缺失 / 加载中 / 失败 /
 * `disable-blender-models=1` 时的程序化三层树）：
 *   · 几何与配色常量：TRUNK_* / BRANCH_* / CROWN_COLORS
 *   · 逐树确定性形态：treeSeed（种子）/ treeShape（分枝数 + 5 球错落冠）
 *
 * 形态（沿用批次 30 P0-3 归一到 REAL_DIMS_M.streetTree 9 m 真实行道树尺度）：
 * 主干（圆柱 h4.6 m ⌀0.28~0.36 m）+ 2~3 分枝 + **5 球 2~3 层错落冠**（冠底 3.7 m、
 * 冠顶 9.4 m），逐实例色取 CROWN_COLORS 三档。
 *
 * 19-Blender3D模型集成曾在此导出 `TreeV3` / `TreeV3Fallback` 组件（含全仓唯一的
 * `<Model url={oak_tree}>` 真实 GLB 树路径），但**零渲染点** ⇒ 批次 44 D1 删除，
 * 详见文件末的清理说明。真实 GLB 树现由 `TreesInstanced` 渲染（三变体）。
 */
import { u } from '../cityScale';

export const TRUNK_COLOR = '#5a4634';
export const CROWN_COLORS = ['#2f7a3a', '#3a8a45', '#4a9a55'];

/**
 * 主干/分枝几何参数（TreesInstanced 与 TreeV3Fallback **同源**，批次 30 P0-3
 * 随树形整体放大到真实行道树尺度：干高 4.6 m、⌀0.28~0.36 m）。
 */
export const TRUNK_GEOM: [number, number, number, number] = [u(0.14), u(0.18), u(4.6), 8];
export const TRUNK_Y = u(2.3);
export const BRANCH_GEOM: [number, number, number, number] = [u(0.06), u(0.09), u(1.4), 6];
export const BRANCH_Y = u(3.9);
export const BRANCH_R = u(0.5);

/** 与 DistrictBlock.tsx 同款的 24 行 FNV-1a（避免主路径跨文件引用）。 */
function hashStr(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

/**
 * 树形态种子（StreetPropsLayer / TreesInstanced 共用，保证逐实例形态一致）。
 * ⚠ `stableTruncate` 也用它排序截断 —— 改哈希会改变全城保留哪 1800 棵。
 */
export function treeSeed(x: number, z: number): number {
  return hashStr(`tree:${x.toFixed(3)}:${z.toFixed(3)}`);
}

/**
 * 确定性树形态（主干/分枝/**5 球 2~3 层错落冠**）—— 降级树与 GLB 树形态同源。
 * 批次 30 P0-3/A5：原 3 球紧凑冠（总高仅 3.4~3.7 m × scale 0.5~1.2 ⇒ 1.7~4.4 m
 * "棒棒糖"）改为分层不规则冠：低层 2 球 + 中层 2 球 + 顶 1 球，包围盒归一到
 * cityScale.REAL_DIMS_M.streetTree（高 9.0）。
 */
export function treeShape(effectiveSeed: number): {
  branchCount: number;
  crowns: Array<{ x: number; y: number; z: number; r: number; color: number }>;
} {
  const h = (s: number) => {
    let xh = (s * 2654435761) >>> 0;
    xh ^= xh >>> 13;
    xh = Math.imul(xh, 2246822519) >>> 0;
    xh ^= xh >>> 16;
    return xh / 0xffffffff;
  };
  const r1 = h(effectiveSeed);
  const r2 = h(effectiveSeed * 31 + 1);
  const r3 = h(effectiveSeed * 131 + 7);
  /** 确定性抖动：±amp/2 比例。 */
  const j = (r: number, amp: number) => 1 + (r - 0.5) * amp;
  return {
    branchCount: r1 > 0.6 ? 3 : 2,
    crowns: [
      // 低层（大冠幅，主视觉质量）
      { x: 0, y: u(5.3) * j(r2, 0.12), z: 0, r: u(1.6) * j(r1, 0.2), color: 0 },
      // 中层（左右错开）
      { x: u(0.8) * j(r1, 0.3), y: u(6.6), z: u(0.3) * j(r3, 0.3), r: u(1.3) * j(r2, 0.2), color: 1 },
      { x: -u(0.7) * j(r3, 0.3), y: u(6.5) * j(r1, 0.12), z: -u(0.5) * j(r2, 0.3), r: u(1.3) * j(r3, 0.2), color: 2 },
      // 顶（收分）
      { x: u(0.2) * j(r2, 0.3), y: u(7.8), z: -u(0.6) * j(r1, 0.3), r: u(1.1) * j(r1, 0.15), color: 0 },
      { x: -u(0.3) * j(r3, 0.3), y: u(8.5) * j(r3, 0.1), z: u(0.4) * j(r2, 0.3), r: u(0.9) * j(r2, 0.15), color: 1 },
    ],
  };
}

// ── 批次 44 D1：死代码清理 ────────────────────────────────────────────────
// 本文件此前还导出 `TreeV3` / `TreeV3Fallback` 两个组件，是**全仓唯一的
// `<Model url={modelUrl('nature','oak_tree')}>` 真实 GLB 树路径** —— 但两个组件
// **零渲染点**（`StreetPropsLayer` 只 import 具名常量；其尾注释自认「组件本体不渲染」）。
// 后果是：城内 1800 株树从未走过 GLB，而 oak_tree.glb 只服务东森林 150 株
// （批次 44 §2 T2 / §130「声明了却从不接线」）。
//
// 批次 44 已把真实 GLB 树接到 `TreesInstanced`（三变体 street_tree / pine_tree /
// palm_tree），本文件的组件使命完成 ⇒ 删除，**只保留**程序化降级链需要的：
//   · TRUNK_COLOR / CROWN_COLORS / TRUNK_GEOM / TRUNK_Y / BRANCH_GEOM / BRANCH_Y /
//     BRANCH_R —— 降级树的三层几何与配色
//   · treeSeed / treeShape —— 逐树确定性形态（降级树与 GLB 树的形态同源）
// import 侧同步删掉 Model / blenderModelsEnabled / useSynthPBR / sizeTargetFor / u
// （否则会因「导入未使用」被 tsc --noEmit 判错）。
