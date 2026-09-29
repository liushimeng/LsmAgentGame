/**
 * AgentHuman — 批次 35 §4.3：座位居民（LLM 决策的焦点层 ≤12 人）真实 3D 人物，
 * **替换** AgentToken 的圆柱 token 渲染（AgentToken.tsx 保留 —— 小地图仍用其
 * districtSeatOffset 落位公式，CityVoiceBubbleLayer 仍用其 SPEECH_BUBBLE_TTL_MS）。
 *
 * 身体：`characters/<archetype>.glb`（批次 34 八原型）+ 材质槽换色（克隆/换色/dispose
 * 套路照抄 PedestrianV3），appearance 由 `crowdFormula.appearanceFor` 从
 * `game.state.players[].avatar` 推导（与背景行人同源 ⇒ 观感连续）；
 * 降级链：GLB 缺失 / `disable-blender-models=1` → 程序化 6-mesh 人形（站姿，
 * 几何常量复用 PedestrianV3）→ avatar 缺失 → char_casual + 职业色上衣（§4.3）。
 *
 * 站位（§4.1）：`districtCenter + (local_pos − 0.5) × 5.6`（城区底板 8×8 内缩
 * ±2.8，与人行道带同量级）；local_pos 缺失（旧房）→ districtSeatOffset 环形落位。
 *
 * 移动（§4.3）：目标点变化 → 以 `clamp(dist/8, 0.12, 1.8)` 世界单位/s 步行前往，
 * 朝向 = `atan2(dx, dz)`（与 PedestrianV3 同式）；行走 mixer timeScale=1，
 * 到位后 0.2（原地轻微晃动 =「等公交」观感）。REDUCED_MOTION 直接吸附。
 *
 * 冒泡（§5）：speak 气泡 12s（批次 23 链路不动）+ 动作气泡 8s（action/move/sense，
 * 批次 35 新增），动作气泡渲染在 speech 之下（无 speech 时占其位）。
 * 保留 AgentToken 的：名牌 Html / isMe 金环（移到脚底）/ objectInfo 悬浮卡 / a11y。
 */

import { useEffect, useMemo, useRef, useState } from 'react';
import * as THREE from 'three';
import { useFrame } from '@react-three/fiber';
import { Html } from '@react-three/drei';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { useSharedGLTF, blenderModelsEnabled } from '@/engine3d';
import { modelUrl } from '@/assets/models';
import { useObjectInfoProps } from './objectInfo/useObjectInfoProps';
import { appearanceFor } from './crowdFormula';
import { districtSeatOffset, TOKEN_TAG_CROWD_THRESHOLD, SPEECH_BUBBLE_TTL_MS } from './AgentToken';
import { u, worldDims, sizeTargetFor, DISTRICT_SURFACE_Y } from './cityScale';
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

/** 状态图标（players[].status_icon；照抄 AgentToken）。 */
const STATUS_ICON_EMOJI: Record<string, string> = {
  working: '💼', idle: '💤', trading: '📊', resting: '😴', moved: '🚚',
};

/** 批次 35 §5.1：动作气泡存活时长（speak 气泡 12s 在其上方，两者可并存）。 */
export const ACTION_BUBBLE_TTL_MS = 8000;

/** 动作事件 → 图标（§5.1：hear/smell 文案自带语境，sense 统一 👀）。 */
const ACTION_ICON_EMOJI: Record<string, string> = {
  action: '💭', move: '🚶', sense: '👀',
};

/** local_pos 落位域：城区底板 8×8 内缩 ±2.8（§4.1）。 */
const LOCAL_POS_SPAN = 5.6;
/** 移动到位阈值（世界单位）：小于此值视为已到达。 */
const MOVE_EPS = 0.05;
/** 步行速度区间（世界单位/s；§4.3 clamp(dist/8, 0.12, 1.8)）。 */
const MOVE_SPEED_MIN = 0.12;
const MOVE_SPEED_MAX = 1.8;
/** 到位后 mixer timeScale（原地轻微晃动）。 */
const IDLE_TIMESCALE = 0.2;

/** 名牌锚点 y（世界单位；人物高 u(1.67)=0.167，名牌悬于头顶上方）。 */
const TAG_Y = 0.85;
/** 气泡锚点 y（头顶上方，不与名牌压盖）。 */
const BUBBLE_Y = 1.7;
/** 有 speak 气泡时动作气泡下移到此（名牌 0.85 与 speak 1.7 之间）。 */
const ACTION_BUBBLE_Y_STACKED = 1.25;

// ── 程序化 fallback 几何常量（照抄 PedestrianV3：全部由 REAL_DIMS_M.pedestrian 导出）──
const PED = worldDims('pedestrian');
/** 总高（表值）—— 头顶 = 总高，脚底 = 0。 */
const PED_H = PED.y;
/** 腿长 = 髋高。 */
const LEG_H = PED_H * 0.491;
/** 躯干高。 */
const TORSO_H = PED_H * 0.347;
/** 手臂长（肩枢轴向下）。 */
const ARM_H = PED_H * 0.311;
/** 头半径。 */
const HEAD_R = PED_H * 0.0719;
const ARM_W = u(0.09);
const SHOULDER_Y = LEG_H + ARM_H;
const HEAD_Y = PED_H - HEAD_R;
const TORSO_Y = LEG_H + TORSO_H / 2;
const ARM_X = PED.x / 2 - ARM_W / 2;
const LEG_X = u(0.07);
/** GLB 尺寸/落地校验目标（dev 态；与 PedestrianV3 同表值同口径）。 */
const PED_SIZE_TARGET = sizeTargetFor('pedestrian', { label: 'characters/agent-human' });

/** 气泡文本截断（store 原文可达 100 字，展示只留 60 字）。 */
function clipText(text: string, max: number): string {
  return text.length > max ? `${text.slice(0, max)}…` : text;
}

/** 批次 35 §5.1：动作气泡数据（store.actionBubbles[seat] 的形状）。 */
export interface AgentHumanActionBubble {
  text: string;
  type: string;
  ts: number;
}

interface Props {
  player: VirtualCityPlayer;
  /** 城区中心（世界坐标）。 */
  cx: number;
  cz: number;
  /** 同城区内的落位序号 / 总数（local_pos 缺失时环形落位兜底）。 */
  index: number;
  total: number;
  isMe: boolean;
  /** 批次 23：座位居民公开发话气泡（store.speechBubbles[seat]；12s TTL）。 */
  speech?: { text: string; ts: number } | null;
  /** 批次 35 §5.1：座位居民动作气泡（store.actionBubbles[seat]；8s TTL）。 */
  action?: AgentHumanActionBubble | null;
}

export function AgentHuman({ player, cx, cz, index, total, isMe, speech, action }: Props) {
  const t = useT();
  const groupRef = useRef<THREE.Group>(null);
  const color = professionColor(player.profession.id);
  const emoji = professionEmoji(player.profession.id);
  const crowded = total > TOKEN_TAG_CROWD_THRESHOLD;

  // ── 外观（§4.1：与 crowd 同源推导 ⇒ 座位居民与背景行人观感连续）──
  // 依赖全部收敛为原语（avatar 引用随每次 game.state 重建，原语比较避免重复 clone）。
  const avArchetype = player.avatar?.archetype;
  const avAge = player.avatar?.age;
  const avDomain = player.avatar?.domain;
  const avWealth = player.avatar?.wealth;
  const avGender = player.avatar?.gender;
  const appearance = useMemo(() => {
    const a = appearanceFor({
      archetype: avArchetype ?? 'char_casual', // avatar 缺失 → char_casual 兜底
      age: avAge ?? player.age ?? 35,
      domain: avDomain ?? -1,
      wealth: avWealth ?? 0,
      gender: avGender,
      health: 'B', // 座位居民健康档未知，恒 B（不做 C 档缩身）
      index: player.seat, // index 用 seat ⇒ 同座稳定观感
    });
    // §4.3 兜底：avatar 缺失 → 上衣染职业色（char_casual + 职业色）
    return player.avatar ? a : { ...a, top: color };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [avArchetype, avAge, avDomain, avWealth, avGender, player.age, player.seat, color, !!player.avatar]);

  // ── GLB 身体（批次 34 八原型；disable-blender-models=1 / 资产缺失 → 程序化兜底）──
  const url = blenderModelsEnabled() ? modelUrl('characters', appearance.archetype) : '';
  const { scene: glbScene, animations } = useSharedGLTF(url, PED_SIZE_TARGET);

  /**
   * 克隆 + 换色必须同处一个 useMemo（照抄 PedestrianV3）：`clone(true)` 共享材质
   * 引用，直接改色会污染同 URL 的所有实例；先克隆材质再改色，cleanup 统一 dispose。
   */
  const glbCloned = useMemo(() => {
    if (!glbScene) return null;
    const c = glbScene.clone(true);
    const tinted: THREE.Material[] = [];
    c.traverse((obj) => {
      const mesh = obj as THREE.Mesh;
      if (!mesh.isMesh) return;
      const apply = (src: THREE.Material): THREE.Material => {
        const m = src.clone();
        tinted.push(m);
        const std = m as THREE.MeshStandardMaterial;
        if (std.color) {
          // 材质槽名约定（3d_script/build_character.py 固定）：
          // PedestrianBody / PedestrianPants / PedestrianHead / PedestrianShoes
          const name = m.name || '';
          if (name.includes('Body')) std.color.set(appearance.top);
          else if (name.includes('Pants')) std.color.set(appearance.pants);
          else if (name.includes('Head')) std.color.set(appearance.skin);
          else if (name.includes('Shoes')) std.color.set('#1a1a1a');
          else std.color.set(appearance.top);
        }
        return m;
      };
      mesh.material = Array.isArray(mesh.material)
        ? mesh.material.map(apply)
        : apply(mesh.material);
    });
    c.userData.__tintedMats = tinted;
    return c;
  }, [glbScene, appearance]);

  useEffect(() => () => {
    const mats = glbCloned?.userData.__tintedMats as THREE.Material[] | undefined;
    if (mats) for (const m of mats) m.dispose();
  }, [glbCloned]);

  // GLB 模式 mixer（per-instance，独立推进 walk clip；timeScale 由移动状态驱动）
  const mixerRef = useRef<THREE.AnimationMixer | null>(null);
  useEffect(() => {
    if (!glbCloned || animations.length === 0) return;
    const m = new THREE.AnimationMixer(glbCloned);
    m.timeScale = IDLE_TIMESCALE;
    m.clipAction(animations[0]).play();
    mixerRef.current = m;
    return () => {
      m.stopAllAction();
      m.uncacheRoot(glbCloned);
      mixerRef.current = null;
    };
  }, [glbCloned, animations]);

  // ── 站位（§4.1）：local_pos → 中心 + (p−0.5)×5.6；缺失 → 环形落位（旧房兼容）──
  const { dx, dz } = districtSeatOffset(index, total);
  const lp = player.local_pos;
  const targetX = lp ? cx + (lp[0] - 0.5) * LOCAL_POS_SPAN : cx + dx;
  const targetZ = lp ? cz + (lp[1] - 0.5) * LOCAL_POS_SPAN : cz + dz;
  // 目标经 ref 传递（useFrame 读最新值）；position prop 只在挂载时定格一次 ——
  // 若随渲染重设会瞬移到目标，行走动画就没了。
  const targetRef = useRef({ x: targetX, z: targetZ });
  targetRef.current = { x: targetX, z: targetZ };
  const initialPosRef = useRef<[number, number, number] | null>(null);
  if (initialPosRef.current === null) {
    initialPosRef.current = [targetX, DISTRICT_SURFACE_Y, targetZ];
  }

  /** 批次 28 A4 同款：动画/位移节流 ~30Hz（acc 合并 delta，隔帧零工作）。 */
  const stepAccRef = useRef(0);
  useFrame((_state, delta) => {
    stepAccRef.current += delta;
    if (stepAccRef.current < 1 / 30) return;
    const step = stepAccRef.current;
    stepAccRef.current = 0;
    const g = groupRef.current;
    if (!g) return;
    const { x: tx, z: tz } = targetRef.current;
    if (REDUCED_MOTION) {
      g.position.x = tx;
      g.position.z = tz;
      g.position.y = DISTRICT_SURFACE_Y;
      return;
    }
    const mdx = tx - g.position.x;
    const mdz = tz - g.position.z;
    const dist = Math.hypot(mdx, mdz);
    const mixer = mixerRef.current;
    if (dist > MOVE_EPS) {
      // §4.3：clamp(dist/8, 0.12, 1.8) 世界单位/s 步行前往（近了减速，到位不冲过头）
      const speed = Math.min(MOVE_SPEED_MAX, Math.max(MOVE_SPEED_MIN, dist / 8));
      const move = Math.min(dist, speed * step);
      g.position.x += (mdx / dist) * move;
      g.position.z += (mdz / dist) * move;
      // 朝向 = 行进方向（与 PedestrianV3 的 atan2(dx, dz) 同式，+Z 向前）
      g.rotation.y = Math.atan2(mdx, mdz);
      if (mixer) mixer.timeScale = 1;
    } else if (mixer) {
      mixer.timeScale = IDLE_TIMESCALE;
    }
    g.position.y = DISTRICT_SURFACE_Y;
    mixer?.update(step);
  });

  // ── 气泡过期（批次 23 speech 逻辑照抄；action 同款 8s）──
  // hook 必须位于下方 !player.alive 早退之前（Rules of Hooks）。
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

  const [actionExpired, setActionExpired] = useState(false);
  useEffect(() => {
    if (!action) {
      setActionExpired(false);
      return;
    }
    setActionExpired(false);
    const remain = ACTION_BUBBLE_TTL_MS - (Date.now() - action.ts);
    const timer = window.setTimeout(() => setActionExpired(true), Math.max(remain, 0));
    return () => window.clearTimeout(timer);
  }, [action]);

  // 批次 28 B2：玩家信息交互（命中落在身体 mesh 上；名牌 Html 保持
  // pointer-events:none 不吃点击 —— 与 AgentToken 同口径）。
  const info = useObjectInfoProps('actor.player', {
    anchorY: 0.6,
    extra: [
      { label: 'name', value: player.nickname || player.account },
      { label: 'profession', value: player.profession.title },
      { label: 'netWorth', value: formatCny(player.net_worth) },
      { label: 'seat', value: String(player.seat + 1) },
    ],
  });

  // ── 程序化 fallback 几何（站姿：不摆臂；useMemo + 卸载 dispose，照抄 PedestrianV3）──
  const torsoGeo = useMemo(() => new THREE.BoxGeometry(u(0.32), TORSO_H, u(0.18)), []);
  const headGeo = useMemo(() => new THREE.SphereGeometry(HEAD_R, 12, 10), []);
  const armGeo = useMemo(() => {
    const g = new THREE.BoxGeometry(ARM_W, ARM_H, ARM_W);
    g.translate(0, -ARM_H / 2, 0);
    return g;
  }, []);
  const legGeo = useMemo(() => {
    const g = new THREE.BoxGeometry(u(0.11), LEG_H, u(0.11));
    g.translate(0, -LEG_H / 2, 0);
    return g;
  }, []);
  useEffect(() => () => torsoGeo.dispose(), [torsoGeo]);
  useEffect(() => () => headGeo.dispose(), [headGeo]);
  useEffect(() => () => armGeo.dispose(), [armGeo]);
  useEffect(() => () => legGeo.dispose(), [legGeo]);

  if (!player.alive) {
    // 出局居民移出地图（§9.1：players[].alive=false 不渲染）。
    return null;
  }

  const showSpeech = !!speech && !speechExpired;

  return (
    <group
      {...info}
      ref={groupRef}
      position={initialPosRef.current}
      userData={{ bucket: 'agents' }}
    >
      {/* 身体（GLB 优先；缺失 → 程序化 6-mesh 人形站姿；scale = 原型身高系数） */}
      <group scale={appearance.scale}>
        {glbCloned ? (
          <primitive object={glbCloned} />
        ) : (
          <>
            {/* 躯干（上衣色；avatar 缺失时上衣已染职业色，见 appearance 推导） */}
            <mesh geometry={torsoGeo} position={[0, TORSO_Y, 0]}>
              <meshStandardMaterial color={appearance.top} roughness={0.75} metalness={0.05} />
            </mesh>
            {/* 头（肤色；头顶 = 总高 1.67 m） */}
            <mesh geometry={headGeo} position={[0, HEAD_Y, 0]}>
              <meshStandardMaterial color={appearance.skin} roughness={0.7} metalness={0.02} />
            </mesh>
            {/* 左/右臂（上衣色；pivot 落肩，站姿不摆） */}
            <mesh geometry={armGeo} position={[-ARM_X, SHOULDER_Y, 0]}>
              <meshStandardMaterial color={appearance.top} roughness={0.75} metalness={0.05} />
            </mesh>
            <mesh geometry={armGeo} position={[ARM_X, SHOULDER_Y, 0]}>
              <meshStandardMaterial color={appearance.top} roughness={0.75} metalness={0.05} />
            </mesh>
            {/* 左/右腿（裤色；pivot 落髋，脚端贴地） */}
            <mesh geometry={legGeo} position={[-LEG_X, LEG_H, 0]}>
              <meshStandardMaterial color={appearance.pants} roughness={0.8} metalness={0.03} />
            </mesh>
            <mesh geometry={legGeo} position={[LEG_X, LEG_H, 0]}>
              <meshStandardMaterial color={appearance.pants} roughness={0.8} metalness={0.03} />
            </mesh>
          </>
        )}
      </group>
      {/* 我 = 脚底金环（对比 §26.2 反模式 4：明度差 + 光晕双通道；半径按人形
          肩宽 0.55 m 量级放大，俯视可辨识） */}
      {isMe && (
        <mesh position={[0, 0.012, 0]} rotation={[-Math.PI / 2, 0, 0]}>
          <ringGeometry args={[0.05, 0.075, 32]} />
          <meshBasicMaterial color="#d4a017" transparent opacity={0.95} side={THREE.DoubleSide} />
        </mesh>
      )}
      {/* 名牌：座位号 + 昵称 + 职业图标 + 状态图标 + 净资产 + 🤖（照抄 AgentToken） */}
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
      {/* 批次 35 §5.1：动作气泡（💭/🚶/👀；8s TTL）。有 speak 气泡时下移一层，
          无 speak 时占其位 —— 两者可并存。 */}
      {action && !actionExpired && (
        <Html
          center
          distanceFactor={12}
          position={[0, showSpeech ? ACTION_BUBBLE_Y_STACKED : BUBBLE_Y, 0]}
          zIndexRange={[39, 0]}
        >
          <div className="virtualCity-speech-bubble virtualCity-speech-bubble--action" role="status">
            <span className="virtualCity-speech-bubble__icon" aria-hidden="true">
              {ACTION_ICON_EMOJI[action.type] ?? ACTION_ICON_EMOJI.action}
            </span>
            <span className="virtualCity-speech-bubble__text">{clipText(action.text, 60)}</span>
          </div>
        </Html>
      )}
      {/* 批次 23：speak 气泡（公开发话，12s TTL 后消失） */}
      {speech && !speechExpired && (
        <Html center distanceFactor={12} position={[0, BUBBLE_Y, 0]} zIndexRange={[40, 0]}>
          <div className="virtualCity-speech-bubble" role="status">
            <span className="virtualCity-speech-bubble__text">{clipText(speech.text, 60)}</span>
          </div>
        </Html>
      )}
      {/* a11y：屏幕阅读器可读名牌（视觉隐藏由 CSS 处理） */}
      <Html position={[0, -0.05, 0]} center style={{ pointerEvents: 'none' }} zIndexRange={[0, 0]}>
        <span className="virtualCity-sr-only">
          {`${player.seat + 1} ${player.nickname} ${player.profession.title} ${t('virtualCity.netWorth' as TKey)} ${formatCny(player.net_worth)}`}
        </span>
      </Html>
    </group>
  );
}
