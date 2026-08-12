import { useState } from 'react';
import { useT } from '@/hooks/useT';
import { useDoudizhuStore, type DoudizhuStyle } from '@/store/doudizhu.store';
import { STYLE_ICONS } from '@/assets/images/doudizhu';
import { RulesButton } from '@/components/rules/RulesButton';
import { ConfirmModal } from '@/components/ui/ConfirmModal';
import type { DoudizhuGameState } from '@/types/doudizhu';
import type { TKey } from '@/i18n';

interface Props {
  gameState: DoudizhuGameState | null;
  mySeat: number;
  style: DoudizhuStyle;
  /** When true, hides the resign button and shows the spectator badge. */
  spectator?: boolean;
  onResign: () => void;
  onLeave: () => void;
}

/**
 * GameInfoPanel — 游戏信息侧栏。
 *
 * 显示：
 *   - 三位玩家座位（自己高亮）
 *   - 地主/农民标识与倍数
 *   - 本局底注（顶部小字）
 *   - 炸弹/火箭触发飘字动画入口
 *   - 风格切换器（传统地主 / 都市打工仔）—— 运行时实时切换
 *   - 认输 / 退出房间按钮
 *   - 对局结束结果（含净输赢 / 首胜 / 参与奖）
 */
export function GameInfoPanel({ gameState, mySeat, style: _ignored, spectator, onResign, onLeave }: Props) {
  const t = useT();
  const { style, setStyle, gameOver, anteHint } = useDoudizhuStore();
  const [confirmLeave, setConfirmLeave] = useState(false);

  const seatLabel = (seat: number) => {
    if (seat === mySeat) return t('doudizhu.seatYou' as TKey);
    return `${t('doudizhu.seat' as TKey)}${seat}`;
  };

  return (
    <div className="game-info-panel">
      {/* 本局底注 — 顶部小字 */}
      {(anteHint != null && anteHint > 0) && (
        <div className="panel-section ante-hint">
          <span className="label">{t('doudizhu.ante.yourAnte' as TKey)}:</span>
          <span className="value">{anteHint}</span>
        </div>
      )}

      {/* 座位列表 */}
      <div className="panel-section">
        {[0, 1, 2].map((seat) => (
          <div
            key={seat}
            className={`player-info ${gameState?.turn === seat ? 'active' : ''}`}
          >
            <span className={`dot ${gameState?.landlord_seat === seat ? 'landlord' : ''}`} />
            <span>
              {seatLabel(seat)}
              {gameState?.landlord_seat === seat ? ` 👑${t('doudizhu.landlord' as TKey)}` : ''}
              {gameState?.landlord_seat === seat ? '' : (gameState?.landlord_seat ?? -1) >= 0 ? ` (${t('doudizhu.farmer' as TKey)})` : ''}
            </span>
            {gameState && (
              <span className="badge">{gameState.hand_counts[seat]}张</span>
            )}
          </div>
        ))}
      </div>

      {/* 阶段与倍数 */}
      <div className="panel-section">
        {gameState && (
          <>
            <div className="status">
              {gameState.phase === 'bidding'
                ? t('doudizhu.phaseBidding' as TKey)
                : gameState.phase === 'playing'
                  ? gameState.turn === mySeat
                    ? t('doudizhu.yourTurn' as TKey)
                    : t('doudizhu.opponentTurn' as TKey)
                  : t('doudizhu.phaseOver' as TKey)}
            </div>
            <div className="multiplier">
              {t('doudizhu.multiplier' as TKey)}: ×{gameState.multiplier}
              {gameState.bomb_count > 0 && ` (${gameState.bomb_count} 🎆)`}
            </div>
          </>
        )}
      </div>

      {/* 风格切换器 — 运行时实时切换卡面风格 */}
      <StyleSwitcher style={style} setStyle={setStyle} />

      {/* 操作按钮 */}
      <div className="panel-section actions">
        {spectator ? (
          <div className="spectator-badge">
            <span className="dot" /> {t('doudizhu.spectating' as TKey)}
          </div>
        ) : gameOver ? (
          <div className="game-over-msg">
            <p>
              {gameOver.winner === 'landlord'
                ? t('doudizhu.landlordWin' as TKey)
                : t('doudizhu.farmerWin' as TKey)}
            </p>
            <p className="reason">{gameOver.reason}</p>
          </div>
        ) : (
          gameState?.status === 'playing' && (
            <button className="btn btn-danger" onClick={onResign}>
              {t('doudizhu.resign' as TKey)}
            </button>
          )
        )}
        <button className="btn btn-secondary" onClick={() => setConfirmLeave(true)}>
          {t('spectator.exitRoom')}
        </button>
        <RulesButton kind="doudizhu" />
      </div>

      {confirmLeave && (
        <ConfirmModal
          messageKey={'doudizhu.confirmLeave' as TKey}
          danger
          onConfirm={() => { setConfirmLeave(false); onLeave(); }}
          onCancel={() => setConfirmLeave(false)}
        />
      )}
    </div>
  );
}

function StyleSwitcher({
  style,
  setStyle,
}: {
  style: DoudizhuStyle;
  setStyle: (s: DoudizhuStyle) => void;
}) {
  const t = useT();
  const styles: DoudizhuStyle[] = ['traditional_landlord', 'urban_worker'];
  return (
    <div className="panel-section style-switcher">
      <span className="label">{t('doudizhu.style' as TKey)}:</span>
      {styles.map((s) => (
        <button
          key={s}
          className={`btn btn-sm ${style === s ? 'btn-primary' : 'btn-ghost'}`}
          onClick={() => setStyle(s)}
        >
          {STYLE_ICONS[s]} {t(`doudizhu.style_${s}` as TKey)}
        </button>
      ))}
    </div>
  );
}
