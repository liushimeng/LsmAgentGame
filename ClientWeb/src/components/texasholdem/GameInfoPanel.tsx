import { useState } from 'react';
import { useT } from '@/hooks/useT';
import { useTexasHoldemStore, type TexasStyle } from '@/store/texasholdem.store';
import { STYLE_ICONS } from '@/assets/images/texasholdem';
import { RulesButton } from '@/components/rules/RulesButton';
import { ConfirmModal } from '@/components/ui/ConfirmModal';
import type { TexasHoldemGameState } from '@/types/texasholdem';
import type { TKey } from '@/i18n';

interface Props {
  gameState: TexasHoldemGameState | null;
  mySeat: number;
  style: TexasStyle;
  /** When true, hides the resign button and shows the spectator badge. */
  spectator?: boolean;
  onResign: () => void;
  onLeave: () => void;
}

export function GameInfoPanel({ gameState, mySeat, style, spectator, onResign, onLeave }: Props) {
  const t = useT();
  const { setStyle, gameOver } = useTexasHoldemStore();
  const [confirmLeave, setConfirmLeave] = useState(false);

  return (
    <div className="game-info-panel">
      {/* 座位列表 */}
      <div className="panel-section">
        {gameState?.players.map((p, seat) => (
          <div key={seat} className={`player-info ${gameState.turn === seat ? 'active' : ''}`}>
            <span className="dot" />
            <span>
              {seat === mySeat ? t('texasholdem.seatYou' as TKey) : p.user_id.slice(0, 6)}
              {seat === gameState.button ? ' 👑' : ''}
            </span>
            <span className="badge">${p.stack}</span>
          </div>
        ))}
      </div>

      {/* 牌局信息 */}
      <div className="panel-section">
        {gameState && (
          <>
            <div className="status">
              {gameState.turn === mySeat
                ? t('texasholdem.yourTurn' as TKey)
                : t('texasholdem.opponentTurn' as TKey)}
            </div>
            <div className="texas-pot-info">
              {t('texasholdem.pot' as TKey)}: ${gameState.pot}
            </div>
            <div className="texas-hand-info">
              {t('texasholdem.hand' as TKey).replace('{}', String(gameState.hand_number))}
            </div>
          </>
        )}
      </div>

      {/* 风格切换 */}
      <StyleSwitcher style={style} setStyle={setStyle} />

      {/* 操作按钮 */}
      <div className="panel-section actions">
        {gameOver && (
          <div className="game-over-msg">
            {(gameOver.winners ?? []).length > 0
              ? `${t('texasholdem.winners' as TKey)} ${gameOver.winners!.map(w => `#${w}`).join(', ')}`
              : t('texasholdem.splitPot' as TKey)}
          </div>
        )}
        {spectator ? (
          <div className="spectator-badge">
            <span className="dot" /> {t('texasholdem.spectating' as TKey)}
          </div>
        ) : gameState?.status === 'playing' ? (
          <button className="btn btn-danger" onClick={onResign}>
            {t('texasholdem.resign' as TKey)}
          </button>
        ) : null}
        {/* §20260823-01 — 离开按钮按身份区分文案：
            观战者显示「离开观战」，玩家显示「离开游戏」。
            行为分支（onLeave 内部 unspectate vs leaveGame）由父组件 TexasHoldemGamePage
            根据 spectator prop 处理，本按钮只负责渲染正确的语义文案。 */}
        <button className="btn btn-secondary" onClick={() => setConfirmLeave(true)}>
          {t(spectator ? ('spectator.exitRoom' as TKey) : ('texasholdem.exitRoomAsPlayer' as TKey))}
        </button>
        <RulesButton kind="texasholdem" />
      </div>

      {confirmLeave && (
        <ConfirmModal
          messageKey={'texasholdem.confirmLeave' as TKey}
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
  style: TexasStyle;
  setStyle: (s: TexasStyle) => void;
}) {
  const t = useT();
  const styles: TexasStyle[] = ['western_cowboy', 'wilderness_escape'];
  return (
    <div className="panel-section style-switcher">
      <span className="label">{t('texasholdem.style' as TKey)}:</span>
      {styles.map((s) => (
        <button
          key={s}
          className={`btn btn-sm ${style === s ? 'btn-primary' : 'btn-ghost'}`}
          onClick={() => setStyle(s)}
        >
          {STYLE_ICONS[s]} {t(`texasholdem.style_${s}` as TKey)}
        </button>
      ))}
    </div>
  );
}
