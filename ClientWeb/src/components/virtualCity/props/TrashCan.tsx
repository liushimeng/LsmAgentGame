/**
 * TrashCan — 垃圾箱（15-3D城市全面真实感深化 · 阶段 M）：
 *
 * 圆桶 + 顶盖 + 分类色（蓝色可回收 / 灰色其他 / 红色有害）。
 * 米制统一经 cityScale.u()；**尺寸取自 cityScale.REAL_DIMS_M.trashCan**
 * （⌀0.50 × H1.00，批次 29 与 road/trash_can.glb 统一口径）。
 *
 * 契约：lag_docs/虚拟城市/已实现/15-3D城市渲染深化/02-架构设计 §4.1。
 */

import { memo } from 'react';
import { useI18nStore } from '@/store/i18n.store';
import type { Lang } from '@/i18n';
import { u, worldDims } from '../cityScale';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const BIN_COLORS = ['#3a78c8', '#8a8d96', '#c8453a']; // 蓝 / 灰 / 红（3 分类）

/**
 * 桶尺寸取自 cityScale.REAL_DIMS_M.trashCan（⌀0.50 × H1.00 含盖，唯一事实来源；
 * 与 road/trash_can.glb 同行表值 —— 此前组件内的 u(0.18)/u(0.55) 硬编码已删除，
 * 那组值（⌀0.36 × 0.60）既非真实分类桶、也与 GLB 口径不一致）。
 */
const BIN = worldDims('trashCan');
/** 桶盖厚度（5 cm，构图常量；桶身 = 总高 − 盖厚 ⇒ 总高恰好 = 表值）。 */
const CAP_T = u(0.05);
/** 桶身半径 / 高度（世界单位）。 */
const BIN_R = BIN.x / 2;
const BIN_BODY_H = BIN.y - CAP_T;

/** 程序化桶分类行（按 BIN_COLORS 下标；三语与 catalog 同策略不进 i18n/Dict）。 */
const BIN_CATEGORY: Record<0 | 1 | 2, Record<Lang, string>> = {
  0: { 'zh-CN': '可回收物（蓝）', en: 'Recyclable (blue)', ja: 'リサイクル（青）' },
  1: { 'zh-CN': '其他垃圾（灰）', en: 'General waste (grey)', ja: 'その他（灰）' },
  2: { 'zh-CN': '有害垃圾（红）', en: 'Hazardous (red)', ja: '有害ゴミ（赤）' },
};

interface Props {
  x: number;
  z: number;
  rotation?: number;
  /** 分类（0=蓝/可回收, 1=灰/其他, 2=红/有害）；缺省按 district id 散列。 */
  variant?: 0 | 1 | 2;
}

// 批次 28 A1/A3：memo + 街具默认不投影（阴影 pass caster 裁剪）。
export const TrashCan = memo(function TrashCan({ x, z, rotation = 0, variant = 0 }: Props) {
  const R = BIN_R;
  const H = BIN_BODY_H;
  const color = BIN_COLORS[variant];
  const lang = useI18nStore((s) => s.lang);
  // anchorY：悬浮卡相对命中点的上浮（世界单位；≈ 桶高 × 6，与街具同量级）
  const info = useObjectInfoProps('prop.trash-can', {
    anchorY: 0.6,
    extra: [{ label: 'category', value: BIN_CATEGORY[variant][lang] }],
  });

  return (
    // 落地：桶底 = group 原点（y=0 贴地），总高 = BIN_BODY_H + CAP_T = 表值 1.00 m
    <group {...info} position={[x, 0, z]} rotation={[0, rotation, 0]}>
      {/* 桶身 */}
      <mesh position={[0, H / 2, 0]}>
        <cylinderGeometry args={[R, R * 0.92, H, 12]} />
        <meshStandardMaterial color={color} roughness={0.6} metalness={0.2} />
      </mesh>
      {/* 顶盖（略外扩，色深一档） */}
      <mesh position={[0, H + CAP_T / 2, 0]}>
        <cylinderGeometry args={[R * 1.05, R * 1.05, CAP_T, 6]} />
        <meshStandardMaterial color={color} roughness={0.5} metalness={0.3} />
      </mesh>
      {/* 投口标识：白色小条（前面） */}
      <mesh position={[0, H * 0.7, R + 0.001]}>
        <planeGeometry args={[u(0.15), u(0.04)]} />
        <meshStandardMaterial color="#ffffff" roughness={0.6} />
      </mesh>
    </group>
  );
});

export default TrashCan;