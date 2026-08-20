import { TexasCardView, TexasCardBack } from './CardView';
import type { TexasPlayerInfo } from '@/types/texasholdem';
import type { StyleKey } from '@/assets/images/texasholdem';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

interface Props {
  /** 可能为 undefined：座位索引越界时(见下方纵深防御注释)。 */
  player: TexasPlayerInfo | undefined;
  style: StyleKey;
  isMe: boolean;
  isTurn: boolean;
  isButton: boolean;
  isSB: boolean;
  isBB: boolean;
  // 2026-08-19 §德州扑克Agent — 新增 Bot 字段(可选,默认 false)
  isBot?: boolean;
  botModelName?: string;
  botThinking?: boolean;
  botHeartThought?: string;
}

export function PlayerSeat({
  player,
  style,
  isMe,
  isTurn,
  isButton,
  isSB,
  isBB,
  isBot = false,
  botModelName,
  botThinking = false,
  botHeartThought,
}: Props) {
  const t = useT();

  // 2026-08-20 §德州扑克观战崩溃 — 纵深防御：座位索引越界(观战者 mySeat=-1 旋转、
  // 服务端 players 数组短于 6 等)会让 player 为 undefined，此处解引用曾直接打穿
  // 错误边界导致整页「页面渲染异常」。缺座一律按空座渲染，绝不抛错。
  if (!player || !player.has_player) {
    return <div className="texas-seat empty" />;
  }

  const classList = [
    'texas-seat',
    isTurn ? 'turn' : '',
    player.folded ? 'folded' : '',
    player.all_in ? 'allin' : '',
    isBot ? 'bot' : '',
    botThinking ? 'thinking' : '',
  ].filter(Boolean).join(' ');

  // 服务端 hole 字段可能为 null（非自己座位、观察者、已弃牌等），统一兜底为空数组，
  // 避免 `Cannot read properties of null (reading 'length')`（BUG-TEXAS-HOLE-NULL）。
  const holeCards = Array.isArray(player.hole) ? player.hole : [];

  return (
    <div className={classList}>
      <div className="seat-info">
        <span className="seat-name">
          {isMe ? t('texasholdem.seatYou' as TKey) : player.user_id.slice(0, 6)}
        </span>
        {isButton && <span className="badge dealer">D</span>}
        {isSB && <span className="badge sb">SB</span>}
        {isBB && <span className="badge bb">BB</span>}
        {player.all_in && <span className="badge allin-badge">ALL IN</span>}
        {/* 2026-08-19 §德州扑克Agent — Bot 徽章 */}
        {isBot && (
          <span
            className="badge bot-badge"
            title={botModelName || t('texasholdem.bot.badge' as TKey)}
            data-testid="thp-seat-bot-badge"
          >
            🤖 {botModelName || t('texasholdem.bot.badge' as TKey)}
          </span>
        )}
      </div>
      <div className="seat-stack">${player.stack}</div>
      {/* 2026-08-19 §德州扑克Agent — 思考中指示器 */}
      {isBot && botThinking && (
        <div className="seat-thinking" data-testid="thp-seat-thinking">
          ⏳ {t('texasholdem.bot.thinking' as TKey)}
        </div>
      )}
      {/* 2026-08-19 §德州扑克Agent — 内心独白(hover 弹全文) */}
      {isBot && botHeartThought && (
        <div
          className="seat-heart-thought"
          title={botHeartThought}
          data-testid="thp-seat-heart-thought"
        >
          💭 {botHeartThought.length > 30 ? botHeartThought.slice(0, 30) + '…' : botHeartThought}
        </div>
      )}
      <div className="seat-holes">
        {isMe || holeCards.length > 0 ? (
          holeCards.length > 0 ? (
            holeCards.map((card, i) => (
              <TexasCardView key={i} card={card} style={style} small />
            ))
          ) : (
            <>
              <TexasCardBack style={style} small />
              <TexasCardBack style={style} small />
            </>
          )
        ) : (
          <span className="hole-count">2</span>
        )}
      </div>
      {player.chips_committed > 0 && (
        <div className="seat-committed">${player.chips_committed}</div>
      )}
    </div>
  );
}