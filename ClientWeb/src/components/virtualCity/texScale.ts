/**
 * texScale — 虚拟城市**贴图物理周期登记表**（批次 37 · P1 · 方案
 * `lag_docs/虚拟城市/已实现/37-3D资产贴图匹配与渲染保真/01-方案设计.md` §4.2）。
 *
 * 为什么需要这张表：批次 37 之前盒面 UV 恒为 0..1（= 一个贴图周期铺满整个面），
 * 于是「一张 512x1024 立面图」被拉伸到 10 m 高的裙楼与 44 m 高的塔身上，
 * 且**两个体块的变形方向相反**（裙楼横向拉伸 3.40x / 塔身纵向拉伸 1.61x）。
 * 修法不是重画贴图，而是给引擎层一个**物理周期**（tile）：立面资产语义从
 * 「整栋立面图」改为「2 开间 x 4 层、四边无缝」的可平铺开间贴图
 * （`assets/images/virtualCity/facade_tiles/<stem>_{base,mid}.png`）。
 *
 * **强制不变式**（方案 §2.1「纹素密度恒定定理」）：
 *     tileMetersU / tileMetersV === texW / texH
 * 引擎层 `boxFacesUV` 取 `u = 面宽 / tileU`、`v = 面高 / tileV`（世界单位），
 * 只要该不变式成立，任意尺寸面（含不同朝向、不同体块）的横向/纵向纹素密度恒相等，
 * **各向异性 = 1.000，零变形**，且与面尺寸无关。`worldTileUV()` 每次调用都会校验。
 *
 * 换算纪律：米 → 世界单位的换算**只此一处**（÷ `METERS_PER_UNIT`），引擎层只见世界单位。
 *
 * 体量纪律（批次 37 返工）：贴图经 `import.meta.glob(..., { eager: true })` 直接打进
 * JS bundle，1024x2048 单张约 2.0 MB、32 城区 x2 = 314 MB（≈ 现有全量资产 3.5 倍），
 * 故立面**降一档到 512x1024**（约 0.5 MB/张、合计 ≈ 16 MB）。降档只改像素数、
 * **不改物理周期**（tileMetersU/V 不动）⇒ 不变式 `tileMetersU/V == texW/texH`
 * 与「纹素密度与面尺寸无关」的结论**完全不变**，只是常数从 170.7 降到 85.3 px/m
 * （与既有 `facades/` 512x1024 同档，肉眼分辨不出）。
 */

import type { BoxUVSpec } from '@/engine3d';
import { METERS_PER_UNIT } from './cityScale';

/**
 * 贴图的物理周期登记条目。
 *
 * 字段口径：
 *   - `texW` / `texH`  = 贴图像素尺寸（事实来源在美术侧生成脚本；本表须与实际文件一致，
 *                       由 `3d_script/audit_texture_fit.py` 判据 A2 逐张核对）；
 *   - `tileMetersU/V`  = 一个贴图周期在**真实米制**下覆盖的宽/高。
 *
 * 不变式：`tileMetersU / tileMetersV === texW / texH`（横向与纵向纹素密度必须相等，
 * 否则贴图在任意面上都带各向异性变形 —— 批次 37 §1.3 实测最高 20x）。
 */
export interface TextureTile {
  /** 贴图像素宽（px）。 */
  texW: number;
  /** 贴图像素高（px）。 */
  texH: number;
  /** 一个 U 周期覆盖的物理宽度（米）。 */
  tileMetersU: number;
  /** 一个 V 周期覆盖的物理高度（米）。 */
  tileMetersV: number;
}

/** 贴图长宽比与物理周期比的相对误差容限（判据 A1 阈值 0.5%，此处按浮点精度取 1e-6）。 */
const RATIO_TOL = 1e-6;

/**
 * 立面开间贴图周期（`facade_tiles/<stem>_{base,mid}.png`，**512x1024**）。
 * 物理周期 6 m x 12 m：6 m = 2 开间 x 3 m；12 m = 4 层 x 3 m
 * （`FLOOR_HEIGHT_M = 3`，来自 cityScale）。两个周期都与层高/开间严格整除
 * ⇒ 楼层在立面上永远从同一水平线起画。
 *
 * 分辨率口径：批次 37 返工由 1024x2048 **降一档**到 512x1024（bundle 体量见文件头
 * 「体量纪律」）。降的只是**像素数**，`tileMetersU/V` 保持 6/12 m 不动 ⇒ 不变式
 * `512/1024 = 0.5 === 6/12` 仍成立，全城纹素密度统一为 **85.3 px/m**
 * （= 512 px / 6 m；原 1024 档为 170.7 px/m），与既有 `facades/` 512x1024 同档。
 */
export const FACADE_TILE: TextureTile = {
  texW: 512, texH: 1024, tileMetersU: 6, tileMetersV: 12,
};

/**
 * 屋顶贴图周期（`roofs/<stem>.png`，512x512 正方形）。
 * 取 10 m x 10 m：正方形贴图 ⇒ `tileU === tileV` 天然满足不变式，
 * 保证**相邻楼栋屋顶纹素密度一致**（批次 37 §1.3 屋顶 1.55x 拉伸的修法）。
 */
export const ROOF_TILE: TextureTile = {
  texW: 512, texH: 512, tileMetersU: 10, tileMetersV: 10,
};

/**
 * 校验贴图周期登记的自洽性（方案 §2.1 不变式；判据 A1）。
 * @throws {Error} 尺寸非正有限数、或「物理周期比 ≠ 像素宽高比」时抛出 ——
 *   登记表写错必须在开发期炸掉，而不是渲染出一片各向异性拉伸的贴图。
 */
export function assertTileInvariant(label: string, t: TextureTile): void {
  const bad =
    !Number.isFinite(t.texW) || !Number.isFinite(t.texH) || t.texW <= 0 || t.texH <= 0 ||
    !Number.isFinite(t.tileMetersU) || !Number.isFinite(t.tileMetersV) ||
    t.tileMetersU <= 0 || t.tileMetersV <= 0;
  if (bad) {
    throw new Error(
      `texScale: 贴图周期登记「${label}」字段非法 -> ${t.texW}x${t.texH}px / ` +
      `${t.tileMetersU}x${t.tileMetersV}m（尺寸须为正有限数）`,
    );
  }
  const pxRatio = t.texW / t.texH;
  const mRatio = t.tileMetersU / t.tileMetersV;
  if (Math.abs(mRatio - pxRatio) / pxRatio > RATIO_TOL) {
    const wantV = t.tileMetersU / pxRatio;
    throw new Error(
      `texScale: 贴图周期登记「${label}」违反不变式 tileMetersU/V == texW/texH -> ` +
      `${mRatio.toFixed(6)} vs ${pxRatio.toFixed(6)}；按 ${t.texW}x${t.texH}px 计，物理周期应为 ` +
      `${t.tileMetersU}x${wantV.toFixed(3)}m，否则该贴图在任意立面上都带各向异性变形` +
      '（批次 37 §1.3 实测最高 20x）。',
    );
  }
}

/**
 * 米制贴图周期 → 引擎层世界单位 UV 投影参数。
 *
 * **全城唯一的「米 → 世界单位」换算处**（引擎层 `boxFacesUV` 只认世界单位）。
 * 消费方须配套 `useSharedTexture(url, { wrap: 'repeat' })`：UV 取值可 > 1（多周期），
 * clamp 包裹会把 `v = 3.67` 拉成边缘竖条。
 *
 * @param t       贴图周期登记条目。
 * @param repeatU 额外 U 方向周期倍率（默认 1 = 一周期 = tileMetersU）。
 * @param repeatV 额外 V 方向周期倍率（默认 1）。
 * @param snapV   整周期吸附步长（可选；缺省不吸附 ⇒ 各向异性严格为 1。
 *                开启后纵向纹素密度随面高离散，见 `BoxUVSpec.snapV` 的代价说明）。
 */
export function worldTileUV(
  t: TextureTile,
  repeatU = 1,
  repeatV = 1,
  snapV?: number,
): BoxUVSpec {
  assertTileInvariant('TextureTile', t);
  if (!Number.isFinite(repeatU) || repeatU <= 0 || !Number.isFinite(repeatV) || repeatV <= 0) {
    throw new Error(`texScale: 非法 repeatU/V = ${repeatU}/${repeatV}（须为正有限数）`);
  }
  return {
    tileU: (t.tileMetersU / repeatU) / METERS_PER_UNIT,
    tileV: (t.tileMetersV / repeatV) / METERS_PER_UNIT,
    ...(snapV !== undefined ? { snapV } : {}),
  };
}

// ── 模块级常驻投影参数（引用稳定 ⇒ 可直接进 useMemo 依赖）──────────────

/** 侧墙（侧 A / 侧 B）物理 UV：6 m x 12 m 周期。 */
export const FACADE_UV: BoxUVSpec = worldTileUV(FACADE_TILE);

/** 顶面物理 UV：10 m x 10 m 周期。 */
export const ROOF_UV: BoxUVSpec = worldTileUV(ROOF_TILE);
