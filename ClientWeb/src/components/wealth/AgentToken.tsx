/**
 * AgentToken — 玩家 token：圆柱（r=0.35 h=0.7，职业色）+ drei Billboard 头像
 * sprite（512 PNG，加载失败降级职业色圆环 + emoji）+ Html 名牌（座位号 + 昵称 +
 * 职业 emoji + 净资产档）。useFrame lerp 平滑迁移（district 变化约 1s 到位；
 * prefers-reduced-motion 下直接吸附）。
 */

import { useEffect, useRef, useState } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { Billboard, Html } from '@react-three/drei';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { professionAvatar } from '@/assets/images/wealth';
import {
  formatCny,
  professionColor,
  professionEmoji,
  type WealthPlayer,
} from '@/types/wealth';

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/** 同城区多 token 的环形落位（地图 / 小地图共用，保证点位一致）。 */
export function districtSeatOffset(index: number, total: number): { dx: number; dz: number } {
  if (total <= 1) return { dx: 0, dz: 0.9 };
  const radius = total <= 3 ? 1.9 : 2.4;
  const angle = (index / total) * Math.PI * 2 - Math.PI / 2;
  return { dx: Math.cos(angle) * radius, dz: Math.sin(angle) * radius };
}

const STATUS_ICON_EMOJI: Record<string, string> = {
  working: '💼', idle: '💤', trading: '📊', resting: '😴', moved: '🚚',
};

interface Props {
  player: WealthPlayer;
  /** 城区中心（世界坐标）。 */
  cx: number;
  cz: number;
  /** 同城区内的落位序号 / 总数。 */
  index: number;
  total: number;
  isMe: boolean;
}

export function AgentToken({ player, cx, cz, index, total, isMe }: Props) {
  const t = useT();
  const groupRef = useRef<THREE.Group>(null);
  const [tex, setTex] = useState<THREE.Texture | null>(null);
  const color = professionColor(player.profession.id);
  const emoji = professionEmoji(player.profession.id);
  const avatarUrl = professionAvatar(player.profession.avatar || player.profession.id);

  // 头像纹理（失败 → null → 圆环 + emoji 兜底）。
  useEffect(() => {
    if (!avatarUrl) return;
    let disposed = false;
    const loader = new THREE.TextureLoader();
    loader.load(
      avatarUrl,
      (loaded) => {
        if (disposed) {
          loaded.dispose();
          return;
        }
        loaded.colorSpace = THREE.SRGBColorSpace;
        setTex(loaded);
      },
      undefined,
      () => {
        // onError：保持 null。
      },
    );
    return () => {
      disposed = true;
    };
  }, [avatarUrl]);

  const { dx, dz } = districtSeatOffset(index, total);
  const targetX = cx + dx;
  const targetZ = cz + dz;

  useFrame(() => {
    const g = groupRef.current;
    if (!g) return;
    if (REDUCED_MOTION) {
      g.position.x = targetX;
      g.position.z = targetZ;
      return;
    }
    g.position.x += (targetX - g.position.x) * 0.06;
    g.position.z += (targetZ - g.position.z) * 0.06;
  });

  if (!player.alive) {
    // 出局 token 移出地图（§9.1：players[].alive=false 不渲染）。
    return null;
  }

  return (
    <group ref={groupRef} position={[targetX, 0, targetZ]}>
      {/* 圆柱 token（职业色；观战/他人浅色描边区分） */}
      <mesh castShadow position={[0, 0.35, 0]}>
        <cylinderGeometry args={[0.35, 0.35, 0.7, 24]} />
        <meshStandardMaterial
          color={color}
          roughness={0.5}
          metalness={0.2}
          emissive={color}
          emissiveIntensity={isMe ? 0.45 : 0.15}
        />
      </mesh>
      {/* 我 = 金环高亮（对比 §26.2 反模式 4：明度差 + 光晕双通道） */}
      {isMe && (
        <mesh position={[0, 0.03, 0]} rotation={[-Math.PI / 2, 0, 0]}>
          <ringGeometry args={[0.5, 0.68, 32]} />
          <meshBasicMaterial color="#d4a017" transparent opacity={0.95} side={THREE.DoubleSide} />
        </mesh>
      )}
      {/* 头像 sprite（Billboard 始终面向相机；缺失降级 emoji 圆片） */}
      <Billboard position={[0, 1.25, 0]}>
        {tex ? (
          <mesh>
            <planeGeometry args={[0.9, 0.9]} />
            <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
          </mesh>
        ) : (
          <mesh>
            <circleGeometry args={[0.45, 24]} />
            <meshBasicMaterial color={color} transparent opacity={0.92} />
          </mesh>
        )}
      </Billboard>
      {/* 头像缺失时的 emoji 兜底（永远叠一层 Html 太贵 → 仅降级时渲染） */}
      {!tex && (
        <Html position={[0, 1.25, 0]} center distanceFactor={14} zIndexRange={[8, 0]}>
          <div className="wealth-token-emoji" aria-hidden="true">{emoji}</div>
        </Html>
      )}
      {/* 名牌：座位号 + 昵称 + 职业图标 + 状态图标 */}
      <Html position={[0, 1.85, 0]} center distanceFactor={12} zIndexRange={[9, 0]}>
        <div
          className={
            'wealth-token-tag' + (isMe ? ' wealth-token-tag--me' : '') + (player.is_bot ? ' wealth-token-tag--bot' : '')
          }
        >
          <span className="wealth-token-tag__seat">{player.seat + 1}</span>
          <span className="wealth-token-tag__name">{player.nickname || player.account}</span>
          <span className="wealth-token-tag__emoji">{emoji}</span>
          <span className="wealth-token-tag__status">
            {STATUS_ICON_EMOJI[player.status_icon] ?? ''}
          </span>
          <span className="wealth-token-tag__nw">{formatCny(player.net_worth)}</span>
          {player.is_bot && (
            <span className="wealth-token-tag__bot" title={player.model_display}>🤖</span>
          )}
        </div>
      </Html>
      {/* a11y：屏幕阅读器可读名牌（视觉隐藏由 CSS 处理） */}
      <Html position={[0, -0.1, 0]} center style={{ pointerEvents: 'none' }} zIndexRange={[0, 0]}>
        <span className="wealth-sr-only">
          {`${player.seat + 1} ${player.nickname} ${player.profession.title} ${t('wealth.netWorth' as TKey)} ${formatCny(player.net_worth)}`}
        </span>
      </Html>
    </group>
  );
}
