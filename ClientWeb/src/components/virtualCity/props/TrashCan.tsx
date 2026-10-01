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

/**
 * 分类三色（批次 30 P1-14 语义统一）：0=蓝·可回收 / 1=灰·其他 / 2=绿·厨余
 * —— 与 RoadsideBins 的 GLB 绿/蓝两变体同语义（绿=厨余 / 蓝=可回收），
 * 程序化回退不再出现"同屏红桶 vs GLB 绿桶"两套口径。
 */
const BIN_COLORS = ['#3a78c8', '#8a8d96', '#4a9a55']; // 蓝 / 灰 / 绿

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
  2: { 'zh-CN': '厨余垃圾（绿）', en: 'Kitchen waste (green)', ja: '生ごみ（緑）' },
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
  // anchorY：悬浮卡相对命中点的上浮（批次 30 P1-14：原 0.6=6 m 对 1 m 高桶偏高 6×
  // ⇒ 0.2=2 m 量级；与 RoadsideBins 同值）
  const info = useObjectInfoProps('prop.trash-can', {
    anchorY: 0.2,
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
      {/* 顶盖（批次 30 P1-14：原 R*1.05 使 fallback ⌀0.525 超表值 +5.0% ⇒ 齐平 R） */}
      <mesh position={[0, H + CAP_T / 2, 0]}>
        <cylinderGeometry args={[R, R, CAP_T, 6]} />
        <meshStandardMaterial color={color} roughness={0.5} metalness={0.3} />
      </mesh>
      {/* 投口标识：白色小条（前面） */}
      <mesh position={[0, H * 0.7, R + 0.001]}>
        <planeGeometry args={[u(0.15), u(0.04)]} />
        <meshStandardMaterial color="#ffffff" roughness={0.6} />
      </mesh>
      {/* 批次 42 D1：桶沿分色环（深色压圈嵌在盖顶，不越出表值总高）+ 脚踏板 */}
      <mesh position={[0, H + CAP_T - u(0.008), 0]}>
        <cylinderGeometry args={[R * 0.96, R * 0.96, u(0.016), 12]} />
        <meshStandardMaterial color="#2a2e36" roughness={0.55} metalness={0.35} />
      </mesh>
      <mesh position={[0, u(0.03), R * 0.72]}>
        <boxGeometry args={[u(0.14), u(0.025), u(0.06)]} />
        <meshStandardMaterial color="#2a2e36" roughness={0.7} metalness={0.3} />
      </mesh>
    </group>
  );
});

export default TrashCan;