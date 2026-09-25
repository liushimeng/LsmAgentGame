/**
 * CityVoiceBubbleLayer — 批次 23：市民之声（背景居民）3D 语音气泡层。
 *
 * 数据源：store.eventFeed 中 type === 'city_voice' 的条目（与 MonthTicker /
 * CityStatsPanel 同一数据源，零新订阅 —— game.event 已由 useVirtualCity 推进 store，
 * pushEvent 入队时补 ts 到达时间戳）。取最近 2 条未过期（12s TTL）事件，
 * 在市政厅（city_hall）上空锚点堆叠冒泡，新在上。
 *
 * 事件 text 形如「名字:正文」（room_city.go），按第一个冒号拆出 name 与正文。
 * 该层不依赖座位，挂在 VirtualCityCityMap 场景内（StreetPropsLayer 附近）。
 */

import { useEffect, useMemo, useState } from 'react';
import { Html } from '@react-three/drei';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import { SPEECH_BUBBLE_TTL_MS } from './AgentToken';
import { CITY_HALL_BUBBLE_ANCHOR } from './civic/CityHall';

/** 同时堆叠的最大气泡数（市民之声月结后 4~8 条脉冲到达，只留最新 2 条）。 */
const MAX_VOICE_BUBBLES = 2;

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

export function CityVoiceBubbleLayer() {
  const eventFeed = useVirtualCityStore((s) => s.eventFeed);

  // 每秒 tick 一次驱动过期重渲（eventFeed 本身不随时间变化，TTL 判定需要时钟）。
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

  if (voices.length === 0) return null;

  return (
    <group position={CITY_HALL_BUBBLE_ANCHOR}>
      {voices.map((v, i) => (
        // 新在上（i=0 最新），每条向上堆叠 0.55 世界单位。
        <Html
          key={v.key}
          center
          distanceFactor={12}
          position={[0, i * 0.55, 0]}
          zIndexRange={[40 - i, 0]}
        >
          <div className="virtualCity-speech-bubble virtualCity-speech-bubble--voice" role="status">
            {v.name && <span className="virtualCity-speech-bubble__name">🗣 {v.name}</span>}
            <span className="virtualCity-speech-bubble__text">
              {v.text.length > 60 ? `${v.text.slice(0, 60)}…` : v.text}
            </span>
          </div>
        </Html>
      ))}
    </group>
  );
}
