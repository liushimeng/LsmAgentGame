/**
 * 北环轻轨（18-AA · §5.2 RailViaduct）—— 批次 49「高架铁路真实感」重制。
 *
 * ── 重制理由（详见 `lag_docs/虚拟城市/已实现/49-高架铁路真实感/01-方案设计.md`）──
 *   批次 18-AA 的程序化版本有三类硬伤：
 *   1) **三处注释与代码互相矛盾**：注释写「箱梁 60 m × 0.16 m」「桥墩每 5 m 一根」，
 *      代码却是 `BoxGeometry(60, u(1.6), u(4.5))`（600 m × 1.6 m × 4.5 m 的平板）与
 *      `for (i<13) x = -30 + i*5`（每 5 **世界单位** = 50 m 一根）。本文件把两侧
 *      统一到 30.5 m 标准跨口径。
 *   2) **不是箱梁**：1.6 m 厚的平板，无底板/斜腹板/梗腋/桥面系/接触网。
 *   3) **桥墩无承台/无盖梁/无支座**；且 50 m 跨超出 GB/T 51234-2017 的 25~30 m
 *      经济区间。
 *
 * ── 架构：标准跨 GLB × 12 实例 + 车站 GLB × 2 实例 ────────────────────
 *   610 m 走廊不可能是一件 GLB（§27.3 单件 ≤ 500 KB），真实工程本身就是「标准跨
 *   预制箱梁逐孔架设」⇒ `civic/rail_span.glb`（30.5 m 一孔，含墩）× 20 个墩位，
 *   其中 **8 个墩位被两座车站占用而跳过**（车站是自持的加宽结构，见
 *   `civic/rail_station.glb` 的头注释），实际渲染 12 跨。
 *   `GlbInstanced` ⇒ draw call = GLB 子网格数（6 / 7），**与实例数无关**。
 *
 * ── 走廊参数（**不是拍脑袋**，见设计 49 §1.2 的净距数值解）───────────────
 *   走廊 z = 46（沿用批次 20 的北迁决策）。落在 z∈[44.2,47.8] 的路段有 3 条，
 *   其中两条是 x = ±26 的方格骨干（14 m 主路）。判据
 *   `净距 = 点到路段中心线距离 − 路半宽 − 墩半宽(0.18)`，对
 *   「N 跨 × 跨距 s，走廊居中」做 0.005 u 步长网格搜索：
 *   **唯一可行跨距是 3.05 u（30.5 m）**，20 跨 610 m，全部 20 根墩离路缘 ≥3.1 m。
 *   （30 m 跨会有一根墩落在 arterial 路面上，偏差 −0.2 m。）
 *   30.5 m 是有工程先例的非整数跨距（某地铁区间碎石道床段 25.815 m），
 *   不是为了凑相位而虚构的数字。
 *
 * ── 坐标系 ─────────────────────────────────────────────────────────────
 *   建模脚本在 Blender 的 **XY 平面**顺走廊铺场（X = 走廊、Y = 横向、Z = 高），
 *   导出 Yup 后 three: `x = Blender x`、`y = Blender z`、`z = -Blender y`。
 *   ⇒ **Blender +Y（声屏障侧 / 出入口塔侧）落在 three 的负 z**，正对 z=40 的
 *   体育新城与湿地公园（噪声敏感面 + 主要客流方向）。
 *   下面的 `bz()` / `by()` 就是这两个换算的单一事实来源。
 *
 * ── 降级链（§27.3-3）────────────────────────────────────────────────
 *   `blenderModelsEnabled()` → `modelUrl` → GLB 载入 → 否则本文件的程序化几何
 *   （**按新尺寸重建**，§27.3-7 双路径同尺寸）。尺寸守卫 `sizeTargetFor`。
 */
import { useEffect, useMemo, useRef } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { blenderModelsEnabled, boxPart, cylPart, mergeParts, useSharedGLTF, type MergePart } from '@/engine3d';
import { modelUrl } from '@/assets/models';
import { GlbInstanced, type GlbInstanceTRS } from '../edge/glbInstanced';
import { sizeTargetFor, u } from '../cityScale';
import { getDayNight } from '../cityTimeStore';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

// ── 走廊参数（设计 49 §1.2）────────────────────────────────────────────
/** 走廊中心线世界 z（沿用批次 20 北迁决策）。 */
const RAIL_Z = 46;
/** 标准跨（米）。30.5 = 设计 49 §1.2 净距数值解的唯一可行解。 */
const SPAN_M = 30.5;
/** 世界单位下的跨距。 */
const SPAN = SPAN_M / 10;
/** 走廊起点（世界单位）—— 20 跨整体居中后再让开两条 arterial。 */
const PIER_X0 = -30.235;
/** 桥墩位数。 */
const PIER_COUNT = 20;
/** 被两座车站占用的跨号（车站自持结构，标准跨在此整段跳过）。 */
const STATION_SPANS: ReadonlyArray<ReadonlyArray<number>> = [[2, 3, 4, 5], [10, 11, 12, 13]];
/** 两座车站的中心世界 x（= 所占 4 跨的中点）。 */
export const STATION_X: readonly number[] = STATION_SPANS.map((g) => {
  const first = PIER_X0 + g[0] * SPAN;
  return first + (g.length * SPAN) / 2;
});

/**
 * `civic/rail_span.glb` 里**墩心**的局部 x（世界单位）。
 * ⚠ 这不是包围盒中心（模块 X 包围盒是 [-1.6445, +1.6445]，中心为 0，而墩在 -1.4115）。
 *   实例原点必须落在墩心对应的世界位置上，否则 20 根墩会整体错位 1.41 m。
 *   数值由 `3d_script/build_rail_span.py` 收尾时打印（`center_content_xz` 的返回值
 *   = 墩心位移），建模侧改动后**必须**同步本常量（§130：声明了就必须接线）。
 */
const RAIL_PIER_LOCAL_X = -1.4115;

/**
 * Blender 横向 y（米）→ three z（世界单位；导出 Yup 翻转 + 米→单位）。
 * ⚠ **两个换算都必须过 `u()`** —— 首版写成 `RAIL_Z - blenderY` / `blenderZ`
 *   直接返回米，tsc / 构建 / 出图全无感，CDP 审计读世界包围盒才发现：
 *   列车被甩到 18 m 外、120 m 高（不在画面里），程序化 fallback 整体放大 10 倍。
 *   （§130 的典型形态：单位换算错了，任何「同一份数据」断言都抓不到，
 *    只有把**世界坐标**读回来比对才暴露。）
 */
const bz = (blenderY: number): number => RAIL_Z - u(blenderY);
/** Blender 竖向 z（米）→ three y（世界单位）。 */
const by = (blenderZ: number): number => u(blenderZ);

// ── 标高（米，与 build_rail_span.py / build_rail_station.py 逐位对齐）────
//   地面 0 / 承台顶 1.60 / 盖梁底 7.80（桥下净空）/ 盖梁顶 9.20 /
//   箱梁底 9.36 / **桥面顶 11.36** / 道床顶 11.66 / **轨顶 11.836** /
//   防撞墙顶 12.46 / 声屏障顶 14.46 / 雨棚 16.20 / **接触线 16.836** /
//   承力索 17.296 / 门架横梁 17.60 / 门架柱顶 17.90 /
//   站台面 12.936（轨顶 +1.10）/ 站厅底 18.196
const TRACK_C = 1.8;              // 线间距 3.6 m
const PARAPET_HY = 4.32;
const BARRIER_HY = 4.42;

// ── 昼夜自发光（材质名匹配 GLB 槽）─────────────────────────────────────
/** 区间桥面照明灯（冷白 LED）。 */
const SPAN_LIGHT_DAY = 0.15;
const SPAN_LIGHT_NIGHT = 1.8;
/** 站台 / 站厅 / 塔顶障碍灯。 */
const STATION_LIGHT_DAY = 0.15;
const STATION_LIGHT_NIGHT = 2.4;
/** 站名牌 / 广告灯箱（蓝色背光）。 */
const SIGN_DAY = 0.25;
const SIGN_NIGHT = 1.5;

const CONCRETE = '#b6b3ad';
const CONCRETE_DARK = '#9a978f';
const STEEL = '#9aa0a6';
const WARN = '#e0a020';
const GLASSY = '#b4c8ce';
const STATION_LAMP = '#f2f6ff';

// ── 列车涂装（批次 49 新增语义键，见 CITY_PALETTE）─────────────────────
const TRAIN_BODY = '#c8d2dc';
/** 车身腰线（线路识别色带，窗下 0.35 m）：现代城轨新车几乎都有一条，
 *  没有它整列车在俯视/远景里只是一条「白肥皂」。 */
const TRAIN_BAND = '#1a6f8e';
const TRAIN_SKIRT = '#22262c';
const TRAIN_GLASS = '#2c4356';
const TRAIN_HEADLIGHT = '#fff6dc';

// ────────────────────────────────────────────────────────────────────────
// 程序化 fallback：按新尺寸重建的区间标准跨 / 车站 / 列车
// ────────────────────────────────────────────────────────────────────────

/** 单孔标准跨的 fallback 部件（Blender 坐标 → three）。`ox` = 该跨的墩心世界 x。 */
function spanFallbackParts(ox: number): MergePart[] {
  const p: MergePart[] = [];
  const cx = ox + u(SPAN_M) / 2;               // 跨中
  p.push(boxPart(u(4.6), u(1.6), u(4.6), ox, by(0.8), bz(0), CONCRETE));            // 承台
  p.push(boxPart(u(4.66), u(0.34), u(4.66), ox, by(0.62), bz(0), WARN));              // 防撞警示带
  p.push(boxPart(u(1.7), u(5.0), u(2.4), ox, by(4.1), bz(0), CONCRETE));              // 墩身
  p.push(boxPart(u(1.7), u(1.4), u(3.6), ox, by(7.3), bz(0), CONCRETE));              // 墩顶扩大头
  p.push(boxPart(u(3.4), u(1.4), u(10.4), ox, by(8.5), bz(0), CONCRETE));             // 盖梁
  p.push(boxPart(u(SPAN_M), u(0.3), u(5.2), cx, by(9.51), bz(0), CONCRETE));           // 底板
  p.push(boxPart(u(SPAN_M), u(0.25), u(9.0), cx, by(11.235), bz(0), CONCRETE));       // 顶板
  for (const sy of [-1, 1]) {
    p.push(boxPart(u(SPAN_M), u(1.46), u(0.4), cx, by(10.385), bz(sy * 2.525), CONCRETE));  // 腹板
    p.push(boxPart(u(SPAN_M), u(0.3), u(2.6), cx, by(11.51), bz(sy * TRACK_C), CONCRETE_DARK)); // 整体道床
    p.push(boxPart(u(SPAN_M), u(1.1), u(0.36), cx, by(11.91), bz(sy * PARAPET_HY), CONCRETE));  // 防撞墙
    p.push(cylPart(u(0.14), u(0.14), u(5.44), 6, ox, by(15.18), bz(sy * PARAPET_HY), STEEL));  // 接触网门架柱
  }
  p.push(boxPart(u(0.3), u(0.34), u(9.04), ox, by(17.6), bz(0), STEEL));              // 门架横梁
  p.push(boxPart(u(SPAN_M - 1.4), u(2.0), u(0.06), ox + u(SPAN_M / 2) + u(0.7), by(13.58), bz(BARRIER_HY), GLASSY)); // 声屏障
  return p;
}

/** 车站的 fallback 部件（Blender 坐标 → three）。`ox` = 站台中心世界 x。 */
function stationFallbackParts(ox: number): MergePart[] {
  const p: MergePart[] = [];
  for (const dx of [-40, 0, 40]) {
    p.push(boxPart(u(5.2), u(1.6), u(5.2), ox + u(dx), by(0.8), bz(0), CONCRETE));
    p.push(boxPart(u(1.8), u(5.0), u(2.4), ox + u(dx), by(4.1), bz(0), CONCRETE));
    p.push(boxPart(u(4.0), u(1.4), u(15.6), ox + u(dx), by(8.5), bz(0), CONCRETE));
  }
  p.push(boxPart(u(120), u(0.35), u(8.4), ox, by(9.535), bz(0), CONCRETE));           // 站区底板
  p.push(boxPart(u(120), u(0.25), u(14.4), ox, by(11.235), bz(0), CONCRETE));          // 站区顶板
  for (const sy of [-1, 1]) {
    p.push(boxPart(u(120), u(0.3), u(2.6), ox, by(11.51), bz(sy * TRACK_C), CONCRETE_DARK));
    p.push(boxPart(u(120), u(1.576), u(4.0), ox, by(12.148), bz(sy * 5.2), CONCRETE)); // 站台板
    p.push(boxPart(u(119), u(1.4), u(0.05), ox, by(13.636), bz(sy * 3.3), GLASSY));    // 半高屏蔽门
    p.push(boxPart(u(120), u(0.22), u(4.6), ox, by(16.2), bz(sy * 4.9), CONCRETE_DARK)); // 雨棚
    for (let i = 0; i <= 8; i++) {
      p.push(cylPart(u(0.12), u(0.12), u(3.264), 6, ox + u(-60 + i * 15), by(14.568), bz(sy * 4.9), STEEL));
    }
    for (const dx of [-38, 38]) {
      for (const cx2 of [-1, 1]) {
        for (const cy2 of [-1, 1]) {
          p.push(boxPart(u(0.55), u(13.236), u(0.55), ox + u(dx) + u(cx2 * 4.72), by(6.468), bz(9.5 + cy2 * 2.22), CONCRETE));
        }
      }
      p.push(boxPart(u(10), u(0.4), u(5.0), ox + u(dx), by(12.736), bz(9.5), CONCRETE_DARK));
    }
  }
  p.push(boxPart(u(24), u(0.35), u(14.4), ox, by(18.371), bz(0), CONCRETE));           // 站厅底板
  p.push(boxPart(u(24), u(4.05), u(14.4), ox, by(20.546), bz(0), CONCRETE));           // 站厅体
  p.push(boxPart(u(24.8), u(0.3), u(15.2), ox, by(22.746), bz(0), CONCRETE_DARK));     // 站厅屋面
  return p;
}

/** 3 节编组列车的 fallback 部件（车体 / 裙板转向架 / 玻璃带三桶）。 */
function trainFallbackParts(cx: number, cz: number): { body: MergePart[]; dark: MergePart[]; glass: MergePart[] } {
  const body: MergePart[] = [];
  const dark: MergePart[] = [];
  const glass: MergePart[] = [];
  const CAR_L = 22.0, CAR_PITCH = 23.5, CAR_W = 2.65, BODY_H = 3.6, BODY_BOT = 12.79;
  for (let i = -1; i <= 1; i++) {
    const x = cx + u(i * CAR_PITCH);
    body.push(boxPart(u(CAR_L), u(BODY_H), u(CAR_W), x, by(BODY_BOT + BODY_H / 2), cz, TRAIN_BODY));
    // 车顶空调 ×2 + 裙板
    for (const k of [-1, 1]) {
      body.push(boxPart(u(2.4), u(0.3), u(2.2), x + u(k * 6.5), by(BODY_BOT + BODY_H + 0.15), cz, TRAIN_SKIRT));
    }
    // 转向架 ×2（轴箱 + 构架）+ 车轮
    for (const k of [-1, 1]) {
      const bx = x + u(k * 7.6);
      dark.push(boxPart(u(2.6), u(0.9), u(2.2), bx, by(12.35), cz, TRAIN_SKIRT));
      for (const w of [-1, 1]) {
        dark.push(cylPart(u(0.43), u(0.43), u(0.12), 10, bx + u(w * 0.95), by(12.266), cz + u(0.72), TRAIN_SKIRT));
        dark.push(cylPart(u(0.43), u(0.43), u(0.12), 10, bx + u(w * 0.95), by(12.266), cz - u(0.72), TRAIN_SKIRT));
      }
    }
    // 侧窗带（每侧 4 段）+ 车门 ×2 + 窗下线形色带
    for (const sy of [-1, 1]) {
      body.push(boxPart(u(CAR_L - 0.6), u(0.35), u(0.05), x, by(14.45), cz + sy * u(1.33), TRAIN_BAND));
      for (let k = 0; k < 4; k++) {
        const wx = x + u(-7.5 + k * 5.0);
        glass.push(boxPart(u(4.2), u(0.95), u(0.06), wx, by(15.05), cz + sy * u(1.32), TRAIN_GLASS));
      }
      for (const k of [-1, 1]) {
        glass.push(boxPart(u(1.3), u(2.1), u(0.05), x + u(k * 5.6), by(14.35), cz + sy * u(1.33), TRAIN_GLASS));
      }
    }
    // 端车司机室：前脸斜板 + 挡风玻璃
    if (i !== 0) {
      const dir = i > 0 ? 1 : -1;
      body.push(boxPart(u(1.8), u(2.9), u(2.5), x + dir * u(11.0), by(BODY_BOT + 1.55), cz, TRAIN_BODY));
      glass.push(boxPart(u(1.2), u(1.05), u(2.2), x + dir * u(11.3), by(BODY_BOT + 2.45), cz, TRAIN_GLASS));
      // 前照灯 ×2
      for (const sy of [-1, 1]) {
        body.push(boxPart(u(0.3), u(0.24), u(0.34), x + dir * u(11.6), by(BODY_BOT + 0.62), cz + sy * u(0.9), TRAIN_HEADLIGHT));
      }
    }
  }
  // 受电弓（中间车顶，升弓至接触线）
  const px = cx;
  dark.push(boxPart(u(1.9), u(0.12), u(1.5), px, by(16.60), cz, TRAIN_SKIRT));
  for (const sy of [-1, 1]) {
    dark.push(boxPart(u(0.08), u(1.5), u(0.08), px + u(sy * 0.7), by(17.4), cz, TRAIN_SKIRT));
  }
  return { body, dark, glass };
}

// ────────────────────────────────────────────────────────────────────────

export function RailViaduct() {
  const info = useObjectInfoProps('civic.rail-viaduct', { anchorY: 6 });
  const spanUrl = blenderModelsEnabled() ? modelUrl('civic', 'rail_span') : '';
  const stationUrl = blenderModelsEnabled() ? modelUrl('civic', 'rail_station') : '';
  const spanSize = useMemo(() => sizeTargetFor('railSpan', { label: 'civic/rail_span' }), []);
  const stationSize = useMemo(() => sizeTargetFor('railStation', { label: 'civic/rail_station' }), []);

  // 12 个区间跨实例（墩心对齐到世界桥墩位）
  const spanInstances = useMemo<GlbInstanceTRS[]>(() => {
    const out: GlbInstanceTRS[] = [];
    for (let k = 0; k < PIER_COUNT; k++) {
      if (STATION_SPANS.some((g) => g.includes(k))) continue;
      out.push({ position: [PIER_X0 + SPAN * k - RAIL_PIER_LOCAL_X, 0, RAIL_Z] });
    }
    return out;
  }, []);
  const stationInstances = useMemo<GlbInstanceTRS[]>(
    () => STATION_X.map((x) => ({ position: [x, 0, RAIL_Z] })),
    [],
  );

  // ── 昼夜：GLB 灯罩 / 站台灯 / 站名牌（按 url 共享 scene ⇒ 一次 traverse 全生效）──
  const { scene: spanScene } = useSharedGLTF(spanUrl, spanSize);
  const { scene: stationScene } = useSharedGLTF(stationUrl, stationSize);
  const spanLightRef = useRef<THREE.MeshStandardMaterial | null>(null);
  const stationLightRef = useRef<THREE.MeshStandardMaterial | null>(null);
  const signRef = useRef<THREE.MeshStandardMaterial | null>(null);
  useEffect(() => {
    spanLightRef.current = null;
    stationLightRef.current = null;
    signRef.current = null;
    for (const [s, key] of [
      [spanScene, 'span'],
      [stationScene, 'station'],
    ] as const) {
      if (!s) continue;
      s.traverse((o) => {
        const m = (o as THREE.Mesh).material as THREE.Material | THREE.Material[] | undefined;
        if (!m) return;
        const mat = (Array.isArray(m) ? m[0] : m) as THREE.MeshStandardMaterial;
        const n = mat?.name ?? '';
        if (!n.includes('Rail_Light')) return;
        mat.emissive ??= new THREE.Color(STATION_LAMP);
        if (key === 'span') spanLightRef.current = mat;
        else stationLightRef.current = mat;
      });
    }
    if (stationScene) {
      stationScene.traverse((o) => {
        const m = (o as THREE.Mesh).material as THREE.Material | THREE.Material[] | undefined;
        if (!m) return;
        const mat = (Array.isArray(m) ? m[0] : m) as THREE.MeshStandardMaterial;
        if (mat?.name?.includes('RailStation_Sign')) {
          mat.emissive ??= new THREE.Color('#1a4f9c');
          signRef.current = mat;
        }
      });
    }
  }, [spanScene, stationScene]);

  // 停靠列车：站台 A 的上行车道（Blender y = -1.8 ⇒ three z = RAIL_Z + 1.8）
  const trainX = STATION_X[0];
  const trainZ = bz(-TRACK_C);

  // ── fallback 几何（GLB 缺失/加载中/失败时渲染）──────────────────────
  const fbSpanGeo = useMemo(
    () => mergeParts(spanInstances.flatMap((it) => spanFallbackParts(it.position[0] + RAIL_PIER_LOCAL_X))),
    [spanInstances],
  );
  useEffect(() => () => fbSpanGeo.dispose(), [fbSpanGeo]);

  const fbStationGeo = useMemo(
    () => mergeParts(stationInstances.flatMap((it) => stationFallbackParts(it.position[0]))),
    [stationInstances],
  );
  useEffect(() => () => fbStationGeo.dispose(), [fbStationGeo]);

  const trainParts = useMemo(() => trainFallbackParts(trainX, trainZ), [trainX, trainZ]);
  const fbTrainBody = useMemo(() => mergeParts(trainParts.body), [trainParts]);
  const fbTrainDark = useMemo(() => mergeParts(trainParts.dark), [trainParts]);
  const fbTrainGlass = useMemo(() => mergeParts(trainParts.glass), [trainParts]);
  useEffect(() => () => {
    fbTrainBody.dispose(); fbTrainDark.dispose(); fbTrainGlass.dispose();
  }, [fbTrainBody, fbTrainDark, fbTrainGlass]);

  const fbSpanMat = useMemo(
    () => new THREE.MeshStandardMaterial({ vertexColors: true, roughness: 0.88, metalness: 0.06 }),
    [],
  );
  useEffect(() => () => fbSpanMat.dispose(), [fbSpanMat]);

  useFrame((_s, delta) => {
    const day = getDayNight()?.dayFactor01 ?? 1;
    const night = 1 - day;
    const k = Math.min(1, delta * 2);
    const approach = (mat: THREE.MeshStandardMaterial | null, d: number, n: number) => {
      if (mat) mat.emissiveIntensity += (d + (n - d) * night - mat.emissiveIntensity) * k;
    };
    approach(spanLightRef.current, SPAN_LIGHT_DAY, SPAN_LIGHT_NIGHT);
    approach(stationLightRef.current, STATION_LIGHT_DAY, STATION_LIGHT_NIGHT);
    approach(signRef.current, SIGN_DAY, SIGN_NIGHT);
  });

  return (
    <group {...info}>
      {/* 区间 12 跨（6 draw call，与实例数无关） */}
      <GlbInstanced
        url={spanUrl}
        instances={spanInstances}
        sizeTarget={spanSize}
        receiveShadow
        fallback={<mesh geometry={fbSpanGeo} material={fbSpanMat} receiveShadow />}
      />
      {/* 车站 ×2（7 draw call） */}
      <GlbInstanced
        url={stationUrl}
        instances={stationInstances}
        sizeTarget={stationSize}
        receiveShadow
        fallback={<mesh geometry={fbStationGeo} material={fbSpanMat} receiveShadow />}
      />
      {/* 停靠列车：车体 + 转向架/受电弓 + 玻璃带（程序化，3 mesh） */}
      <mesh geometry={fbTrainBody} castShadow>
        <meshStandardMaterial vertexColors roughness={0.42} metalness={0.45} />
      </mesh>
      <mesh geometry={fbTrainDark} castShadow>
        <meshStandardMaterial vertexColors roughness={0.62} metalness={0.35} />
      </mesh>
      <mesh geometry={fbTrainGlass}>
        <meshStandardMaterial
          vertexColors roughness={0.12} metalness={0.5}
          transparent opacity={0.78}
        />
      </mesh>
    </group>
  );
}
