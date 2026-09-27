/**
 * TrashCan — 垃圾箱（15-3D城市全面真实感深化 · 阶段 M）：
 *
 * 圆桶 + 顶盖 + 分类色（蓝色可回收 / 灰色其他 / 红色有害）。
 * 米制统一经 cityScale.u()。
 *
 * 契约：lag_docs/虚拟城市/已实现/15-3D城市渲染深化/02-架构设计 §4.1。
 */

import { memo } from 'react';
import { useI18nStore } from '@/store/i18n.store';
import type { Lang } from '@/i18n';
import { u } from '../cityScale';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

const BIN_COLORS = ['#3a78c8', '#8a8d96', '#c8453a']; // 蓝 / 灰 / 红（3 分类）

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
  const R = u(0.18);
  const H = u(0.55);
  const CAP_T = u(0.05);
  const color = BIN_COLORS[variant];
  const lang = useI18nStore((s) => s.lang);
  const info = useObjectInfoProps('prop.trash-can', {
    anchorY: 0.6,
    extra: [{ label: 'category', value: BIN_CATEGORY[variant][lang] }],
  });

  return (
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