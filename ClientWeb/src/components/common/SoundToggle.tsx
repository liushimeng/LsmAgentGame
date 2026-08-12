// SoundToggle — compact sound on/off + volume slider for game headers.
// §20260812-02 U4 — 音效开关 + 音量滑块。
//
// §136:放 components/common/(跨游戏共享)。

import type { useSoundEffects } from '@/hooks/useSoundEffects';

type SoundReturn = ReturnType<typeof useSoundEffects>;

interface Props {
  sound: SoundReturn;
}

export function SoundToggle({ sound }: Props) {
  if (sound.reducedMotion) return null;

  return (
    <div className="sound-toggle" data-testid="sound-toggle">
      <button
        type="button"
        className="sound-toggle__btn"
        onClick={() => sound.setEnabled(!sound.enabled)}
        title={sound.enabled ? '关闭音效' : '开启音效'}
        aria-label={sound.enabled ? '关闭音效' : '开启音效'}
      >
        {sound.enabled ? '🔊' : '🔇'}
      </button>
      {sound.enabled && (
        <input
          type="range"
          className="sound-toggle__slider"
          min={0}
          max={100}
          value={Math.round(sound.volume * 100)}
          onChange={(e) => sound.setVolume(Number(e.target.value) / 100)}
          title={`音量 ${Math.round(sound.volume * 100)}%`}
          aria-label="音量"
        />
      )}
    </div>
  );
}
