// useSoundEffects — Web Audio API 音效 hook (§20260812-02 U4)
//
// 纯前端,零外部依赖,零后端改动。使用 OscillatorNode + GainNode 合成 7 类基础音效。
// 用户偏好(开关 + 音量)持久化到 localStorage。
//
// §136 跨游戏约束:本 hook 放 hooks/(跨游戏共享),不放狼人杀目录。
// §26.5 prefers-reduced-motion:检测到时自动禁用。
//
// Usage:
//   const { play, enabled, setEnabled, volume, setVolume } = useSoundEffects();
//   play('night');  // 在天黑阶段调用
//   play('death');  // 在玩家死亡时调用

import { useCallback, useEffect, useRef, useState } from 'react';

// ─── localStorage keys ───
const LS_ENABLED = 'lsm_sound_enabled';
const LS_VOLUME = 'lsm_sound_volume';

// ─── 音效类型 ───
export type SoundKind =
  | 'night'     // 天黑：低沉钟声
  | 'dawn'      // 天亮：清脆鸟鸣
  | 'vote'      // 投票放逐：沉重鼓点
  | 'death'     // 玩家死亡：倒地声
  | 'prop'      // 道具命中：弹射音
  | 'chat'      // 发言气泡：轻微弹出
  | 'reveal';   // 身份揭晓：翻牌声

// ─── 音效合成参数 ───
interface SoundSpec {
  freq: number;      // 起始频率 Hz
  endFreq?: number;  // 结束频率(滑音)
  duration: number;  // 持续时间 ms
  type: OscillatorType;
  gain: number;      // 峰值增益 0~1
  attack: number;    // 起音 ms
  decay: number;     // 衰减 ms
}

const SOUND_SPECS: Record<SoundKind, SoundSpec> = {
  night:  { freq: 220, endFreq: 180, duration: 800, type: 'sine',     gain: 0.3, attack: 50,  decay: 600 },
  dawn:   { freq: 800, endFreq: 1200, duration: 600, type: 'sine',    gain: 0.2, attack: 100, decay: 400 },
  vote:   { freq: 100, endFreq: 80,  duration: 500, type: 'triangle', gain: 0.4, attack: 10,  decay: 400 },
  death:  { freq: 300, endFreq: 100, duration: 700, type: 'sawtooth', gain: 0.25, attack: 20, decay: 600 },
  prop:   { freq: 600, endFreq: 1000, duration: 300, type: 'square',  gain: 0.2, attack: 5,   decay: 250 },
  chat:   { freq: 1000, endFreq: 800, duration: 150, type: 'sine',    gain: 0.15, attack: 5,  decay: 120 },
  reveal: { freq: 500, endFreq: 700, duration: 400, type: 'triangle', gain: 0.3, attack: 30,  decay: 300 },
};

function loadPref(key: string, fallback: string): string {
  try { return localStorage.getItem(key) ?? fallback; } catch { return fallback; }
}

export function useSoundEffects() {
  // Reduced-motion detection
  const [reducedMotion, setReducedMotion] = useState(false);
  useEffect(() => {
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)');
    setReducedMotion(mq.matches);
    const handler = (e: MediaQueryListEvent) => setReducedMotion(e.matches);
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  }, []);

  const [enabled, setEnabledState] = useState(() => loadPref(LS_ENABLED, '1') === '1');
  const [volume, setVolumeState] = useState(() => {
    const v = parseFloat(loadPref(LS_VOLUME, '0.5'));
    return isNaN(v) ? 0.5 : Math.min(1, Math.max(0, v));
  });

  // AudioContext lazy init (created on first play to satisfy autoplay policy)
  const ctxRef = useRef<AudioContext | null>(null);

  const setEnabled = useCallback((v: boolean) => {
    setEnabledState(v);
    try { localStorage.setItem(LS_ENABLED, v ? '1' : '0'); } catch { /* */ }
  }, []);

  const setVolume = useCallback((v: number) => {
    const clamped = Math.min(1, Math.max(0, v));
    setVolumeState(clamped);
    try { localStorage.setItem(LS_VOLUME, String(clamped)); } catch { /* */ }
  }, []);

  const play = useCallback((kind: SoundKind) => {
    if (!enabled || reducedMotion || volume <= 0) return;

    // Lazy-create AudioContext on user gesture
    if (!ctxRef.current) {
      try { ctxRef.current = new AudioContext(); } catch { return; }
    }
    const ctx = ctxRef.current;
    if (!ctx) return;

    // Resume if suspended (autoplay policy)
    if (ctx.state === 'suspended') {
      ctx.resume().catch(() => { /* */ });
    }

    const spec = SOUND_SPECS[kind];
    const now = ctx.currentTime;

    // Oscillator
    const osc = ctx.createOscillator();
    osc.type = spec.type;
    osc.frequency.setValueAtTime(spec.freq, now);
    if (spec.endFreq) {
      osc.frequency.exponentialRampToValueAtTime(
        Math.max(spec.endFreq, 20), // exponentialRamp 不能到 0
        now + spec.duration / 1000,
      );
    }

    // Gain envelope (attack → sustain → decay)
    const gain = ctx.createGain();
    gain.gain.setValueAtTime(0, now);
    gain.gain.linearRampToValueAtTime(spec.gain * volume, now + spec.attack / 1000);
    gain.gain.linearRampToValueAtTime(0, now + spec.duration / 1000);

    osc.connect(gain);
    gain.connect(ctx.destination);
    osc.start(now);
    osc.stop(now + spec.duration / 1000 + 0.05);
  }, [enabled, reducedMotion, volume]);

  // Cleanup AudioContext on unmount
  useEffect(() => {
    return () => { ctxRef.current?.close().catch(() => { /* */ }); };
  }, []);

  return { play, enabled, setEnabled, volume, setVolume, reducedMotion };
}
