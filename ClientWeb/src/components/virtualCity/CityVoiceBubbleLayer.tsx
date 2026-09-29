/**
 * CityVoiceBubbleLayer — 批次 23：市民之声（背景居民）3D 语音气泡层；
 * 批次 35 §5.3 升级：气泡**锚定到发声居民本人**头顶（此前恒挂市政厅上空）。
 *
 * 数据源：store.eventFeed 中 type === 'city_voice' 的条目（与 MonthTicker /
 * CityStatsPanel 同一数据源，零新订阅 —— game.event 已由 useVirtualCity 推进 store，
 * pushEvent 入队时补 ts 到达时间戳）。取最近 2 条未过期（12s TTL）事件冒泡。
 *
 * 锚定链（§5.3）：事件 text 形如「名字:正文」（room_city.go），按第一个冒号拆出
 * name → 在 `gameState.city.crowd.entries` 里按 name 匹配**上街**居民（首个命中）
 * → crowdRegistry（PedestrianV3 trackIndex 每帧写入的 live 坐标）拿当前位置，
 * 气泡以该 x/z、y≈2.0 定位（每秒随 1s tick 跟随一次）。未命中 / 户内 / 未注册 →
 * 维持市政厅 CITY_HALL_BUBBLE_ANCHOR 堆叠现状（新在上）。
 *
 * 该层不依赖座位，挂在 VirtualCityCityMap 场景内（StreetPropsLayer 附近）。
 */

import { useEffect, useMemo, useState } from 'react';
import { Html } from '@react-three/drei';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import { SPEECH_BUBBLE_TTL_MS } from './AgentToken';
import { getCrowdPosition } from './crowdRegistry';
import { CITY_HALL_BUBBLE_ANCHOR } from './civic/CityHall';
import type { VirtualCityCrowdEntry } from '@/types/virtualCity';

/** 同时堆叠的最大气泡数（市民之声月结后 4~8 条脉冲到达，只留最新 2 条）。 */
const MAX_VOICE_BUBBLES = 2;

/** 锚定到行人时气泡的 y（世界单位；§5.3「头顶 y≈2.0」，与市政厅锚点同量级）。 */
const VOICE_ANCHOR_Y = 2.0;

interface VoiceItem {
  key: string;
  name: string;
  text: string;
  ts: number;
}

/** 「名字:正文」按第一个冒号（半角/全角）拆分；拆不出时整段当正文。 */
function splitVoiceText(raw: string): { name: string; text: string } {
  const m = raw.match(/^([^:：]{1,24})[:：]([\s\S]*)$/);
  if (!m) return { name: '', text: raw };
  return { name: m[1].trim(), text: m[2].trim() };
}

/**
 * 批次 35 §5.3：按名字匹配上街居民 → live 坐标。
 * 命中条件：entry.indoor === false 且 crowdRegistry 已注册（渲染中的行人）。
 */
function findVoiceAnchor(
  name: string,
  entries: VirtualCityCrowdEntry[] | undefined,
): { x: number; z: number } | null {
  if (!name || !entries) return null;
  const hit = entries.find((e) => !e.indoor && e.name === name);
  if (!hit) return null;
  return getCrowdPosition(hit.index) ?? null;
}

export function CityVoiceBubbleLayer() {
  const eventFeed = useVirtualCityStore((s) => s.eventFeed);
  // 批次 35 §5.3：crowd entries（名字匹配源）。组件在 Canvas 内，直接读 store。
  const crowdEntries = useVirtualCityStore(
    (s) => s.gameState?.city?.crowd?.entries,
  );

  // 每秒 tick 一次驱动过期重渲（eventFeed 本身不随时间变化，TTL 判定需要时钟；
  // 锚定坐标也借此每秒跟随一次行走的行人）。
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  const voices = useMemo<VoiceItem[]>(() => {
    const fresh: VoiceItem[] = [];
    // 倒序取最近 MAX_VOICE_BUBBLES 条未过期 city_voice。
    for (let i = eventFeed.length - 1; i >= 0 && fresh.length < MAX_VOICE_BUBBLES; i--) {
      const ev = eventFeed[i];
      if (ev.type !== 'city_voice') continue;
      const ts = ev.ts ?? 0;
      if (!ts || now - ts > SPEECH_BUBBLE_TTL_MS) continue;
      const { name, text } = splitVoiceText(ev.text);
      if (!text) continue;
      fresh.push({ key: `${ts}-${i}`, name, text, ts });
    }
    return fresh;
  }, [eventFeed, now]);

  // 批次 35 §5.3：锚点解算（随 voices / entries / 1s tick 重算 → 跟随行走居民）。
  const anchors = useMemo(
    () => voices.map((v) => findVoiceAnchor(v.name, crowdEntries)),
    [voices, crowdEntries],
  );

  if (voices.length === 0) return null;

  const anchored = voices.map((v, i) => ({ v, pos: anchors[i] }));
  const hallVoices = anchored.filter((x) => !x.pos);

  return (
    <>
      {/* 批次 35 §5.3：锚定本人的气泡（独立 Html，跟随行人当前位置） */}
      {anchored.map(({ v, pos }) =>
        pos ? (
          <Html
            key={v.key}
            center
            distanceFactor={12}
            position={[pos.x, VOICE_ANCHOR_Y, pos.z]}
            zIndexRange={[40, 0]}
          >
            <div className="virtualCity-speech-bubble virtualCity-speech-bubble--voice" role="status">
              {v.name && <span className="virtualCity-speech-bubble__name">🗣 {v.name}</span>}
              <span className="virtualCity-speech-bubble__text">
                {v.text.length > 60 ? `${v.text.slice(0, 60)}…` : v.text}
              </span>
            </div>
          </Html>
        ) : null,
      )}
      {/* 未命中的气泡：维持市政厅锚点堆叠现状（新在上，每条向上 0.55 世界单位） */}
      {hallVoices.length > 0 && (
        <group position={CITY_HALL_BUBBLE_ANCHOR}>
          {hallVoices.map((x, i) => (
            <Html
              key={x.v.key}
              center
              distanceFactor={12}
              position={[0, i * 0.55, 0]}
              zIndexRange={[40 - i, 0]}
            >
              <div className="virtualCity-speech-bubble virtualCity-speech-bubble--voice" role="status">
                {x.v.name && <span className="virtualCity-speech-bubble__name">🗣 {x.v.name}</span>}
                <span className="virtualCity-speech-bubble__text">
                  {x.v.text.length > 60 ? `${x.v.text.slice(0, 60)}…` : x.v.text}
                </span>
              </div>
            </Html>
          ))}
        </group>
      )}
    </>
  );
}
