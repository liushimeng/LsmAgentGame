/**
 * AgentToken — 玩家 token：圆柱（r=0.12 h=0.25，职业色）+ drei Billboard 头像
 * sprite（512 PNG，加载失败降级职业色圆环 + emoji）+ Html 名牌（座位号 + 昵称 +
 * 职业 emoji + 净资产档）。useFrame lerp 平滑迁移（district 变化约 1s 到位；
 * prefers-reduced-motion 下直接吸附）。
 *
 * 2026-09-21 高度系统：整体按 ~0.36× 缩放（标记物 ~2.5m 真实高度，
 * 见 cityScale.ts）——圆柱 / 光环 / Billboard / Html 标签全部联动缩放。
 */

import { useEffect, useRef, useState } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { Billboard, Html } from '@react-three/drei';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { professionAvatar } from '@/assets/images/virtualCity';
import { useSharedTexture } from '@/engine3d';
import {
  formatCny,
  professionColor,
  professionEmoji,
  type VirtualCityPlayer,
} from '@/types/virtualCity';

const REDUCED_MOTION =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/**
 * 同城区多 token 的环形落位（地图 / 小地图共用，保证点位一致）。
 *
 * 2026-09-16 §财商流10–12座位：房间容量 8 → 12，单城区最坏情况可挤进 12 个
 * token。原「>3 一律 2.4」下 12 个 token 的圆周间距仅 ≈1.26 单位（名牌互相压盖）
 * → 按人数分档扩环，最大 3.3 仍落在 8×8 城区底板内（3.3 + token 半径 0.12 < 4，
 * 2026-09-21 高度系统后 token 更矮更窄，环半径不变）。
 */
export function districtSeatOffset(index: number, total: number): { dx: number; dz: number } {
  if (total <= 1) return { dx: 0, dz: 0.9 };
  let radius: number;
  if (total <= 3) radius = 1.9;
  else if (total <= 6) radius = 2.4;
  else if (total <= 9) radius = 2.9;
  else radius = 3.3;
  const angle = (index / total) * Math.PI * 2 - Math.PI / 2;
  return { dx: Math.cos(angle) * radius, dz: Math.sin(angle) * radius };
}

/** 同城区 token 数超过此阈值 → 名牌走紧凑态（隐藏昵称，只留座位号 + emoji + 净资产）。 */
export const TOKEN_TAG_CROWD_THRESHOLD = 6;

const STATUS_ICON_EMOJI: Record<string, string> = {
  working: '💼', idle: '💤', trading: '📊', resting: '😴', moved: '🚚',
};

/** 批次 23：语音气泡存活时长（与 CityVoiceBubbleLayer 同值）。 */
export const SPEECH_BUBBLE_TTL_MS = 12000;

/** 名牌 Html 锚点 y（0.67）；气泡在其上方 +0.9，避免遮挡职业色 token 与金环。 */
const TAG_Y = 0.67;

/** 气泡文本截断（store 原文可达 100 字，展示只留 60 字）。 */
function clipText(text: string, max: number): string {
  return text.length > max ? `${text.slice(0, max)}…` : text;
}

interface Props {
  player: VirtualCityPlayer;
  /** 城区中心（世界坐标）。 */
  cx: number;
  cz: number;
  /** 同城区内的落位序号 / 总数。 */
  index: number;
  total: number;
  isMe: boolean;
  /** 批次 23：座位居民公开发话气泡（store.speechBubbles[seat]；null = 无发言）。 */
  speech?: { text: string; ts: number } | null;
}

export function AgentToken({ player, cx, cz, index, total, isMe, speech }: Props) {
  const t = useT();
  const groupRef = useRef<THREE.Group>(null);
  const color = professionColor(player.profession.id);
  const emoji = professionEmoji(player.profession.id);
  const avatarUrl = professionAvatar(player.profession.avatar || player.profession.id);

  // 14-3D渲染深化：头像纹理走共享缓存（失败 → null → 圆环 + emoji 兜底链不变）。
  const tex = useSharedTexture(avatarUrl);

  const { dx, dz } = districtSeatOffset(index, total);
  const targetX = cx + dx;
  const targetZ = cz + dz;
  const crowded = total > TOKEN_TAG_CROWD_THRESHOLD;

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

  // 批次 23：气泡过期 —— 按 speech.ts 设 timeout 置 expired；新发言（ts 变化）重置。
  // 同座位新发言顶掉旧气泡由 store 保证（speechBubbles 每座位只留最新一条）。
  // 注意：hook 必须位于下方 !player.alive 早退之前（Rules of Hooks）。
  const [speechExpired, setSpeechExpired] = useState(false);
  useEffect(() => {
    if (!speech) {
      setSpeechExpired(false);
      return;
    }
    setSpeechExpired(false);
    const remain = SPEECH_BUBBLE_TTL_MS - (Date.now() - speech.ts);
    const timer = window.setTimeout(() => setSpeechExpired(true), Math.max(remain, 0));
    return () => window.clearTimeout(timer);
  }, [speech]);

  if (!player.alive) {
    // 出局 token 移出地图（§9.1：players[].alive=false 不渲染）。
    return null;
  }

  return (
    <group ref={groupRef} position={[targetX, 0, targetZ]}>
      {/* 圆柱 token（职业色；观战/他人浅色描边区分）。高 0.25 / 半径 0.12 ≈ 2.5m 标记物 */}
      <mesh castShadow position={[0, 0.125, 0]}>
        <cylinderGeometry args={[0.12, 0.12, 0.25, 24]} />
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
        <mesh position={[0, 0.012, 0]} rotation={[-Math.PI / 2, 0, 0]}>
          <ringGeometry args={[0.18, 0.24, 32]} />
          <meshBasicMaterial color="#d4a017" transparent opacity={0.95} side={THREE.DoubleSide} />
        </mesh>
      )}
      {/* 头像 sprite（Billboard 始终面向相机；缺失降级 emoji 圆片）。
          底缘 0.29 > 圆柱顶 0.25，不与几何体重叠。 */}
      <Billboard position={[0, 0.45, 0]}>
        {tex ? (
          <mesh>
            <planeGeometry args={[0.32, 0.32]} />
            <meshBasicMaterial map={tex} transparent alphaTest={0.05} />
          </mesh>
        ) : (
          <mesh>
            <circleGeometry args={[0.16, 24]} />
            <meshBasicMaterial color={color} transparent opacity={0.92} />
          </mesh>
        )}
      </Billboard>
      {/* 头像缺失时的 emoji 兜底（永远叠一层 Html 太贵 → 仅降级时渲染） */}
      {!tex && (
        <Html position={[0, 0.45, 0]} center distanceFactor={14} zIndexRange={[8, 0]}>
          <div className="virtualCity-token-emoji" aria-hidden="true">{emoji}</div>
        </Html>
      )}
      {/* 名牌：座位号 + 昵称 + 职业图标 + 状态图标（0.67 > 头像顶 0.61，不压盖） */}
      <Html position={[0, TAG_Y, 0]} center distanceFactor={12} zIndexRange={[9, 0]}>
        <div
          className={
            'virtualCity-token-tag' +
            (isMe ? ' virtualCity-token-tag--me' : '') +
            (player.is_bot ? ' virtualCity-token-tag--bot' : '') +
            (crowded ? ' virtualCity-token-tag--compact' : '')
          }
        >
          <span className="virtualCity-token-tag__seat">{player.seat + 1}</span>
          {(!crowded || isMe) && (
            <span className="virtualCity-token-tag__name" title={player.nickname || player.account}>
              {player.nickname || player.account}
            </span>
          )}
          <span className="virtualCity-token-tag__emoji">{emoji}</span>
          <span className="virtualCity-token-tag__status">
            {STATUS_ICON_EMOJI[player.status_icon] ?? ''}
          </span>
          <span className="virtualCity-token-tag__nw">{formatCny(player.net_worth)}</span>
          {player.is_bot && (
            <span className="virtualCity-token-tag__bot" title={player.model_display}>🤖</span>
          )}
        </div>
      </Html>
      {/* 批次 23：3D 语音气泡（居民公开发话，名牌上方 +0.9；12s TTL 后消失） */}
      {speech && !speechExpired && (
        <Html center distanceFactor={12} position={[0, TAG_Y + 0.9, 0]} zIndexRange={[40, 0]}>
          <div className="virtualCity-speech-bubble" role="status">
            <span className="virtualCity-speech-bubble__text">{clipText(speech.text, 60)}</span>
          </div>
        </Html>
      )}
      {/* a11y：屏幕阅读器可读名牌（视觉隐藏由 CSS 处理） */}
      <Html position={[0, -0.1, 0]} center style={{ pointerEvents: 'none' }} zIndexRange={[0, 0]}>
        <span className="virtualCity-sr-only">
          {`${player.seat + 1} ${player.nickname} ${player.profession.title} ${t('virtualCity.netWorth' as TKey)} ${formatCny(player.net_worth)}`}
        </span>
      </Html>
    </group>
  );
}
