import { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { formatBalance } from '@/shared/utils/balance';
import { useI18nStore } from '@/store/i18n.store';
import { BattleReportHighlights, type BattleReportHighlight } from '@/components/werewolf/BattleReportHighlights';

export interface SettlementModalProps {
  /** 输赢类型。 */
  result: 'win' | 'lose' | 'draw';
  /** 底注。 */
  ante: number;
  /** 玩家本局净收益（已含手续费、连胜加成等，后端算好）。 */
  netGain: number;
  /** 连胜加成（> 0 时显示）。 */
  streakBonus?: number;
  /** 平台手续费（> 0 时显示，军棋用）。 */
  platformFee?: number;
  /** 最终余额。 */
  finalBalance?: number;
  /** 胜方阵营（狼人杀专用："wolf" | "good"），用于展示阵营胜负标语。 */
  winner?: string;
  /** 游戏类型（决定标题颜色/文案变体）。 */
  gameKind: 'xiangqi' | 'junqi' | 'chess' | 'werewolf';
  onClose: () => void;
  /** §20260811-01 U2 — 即刻原班重开回调（仅狼人杀）。 */
  onFastRestart?: () => void;
  /**
   * §20260811-02 U2 — 死者身份终局延时揭晓分钟数（0/5/15，仅狼人杀）。
   *
   * 接线修复：后端 §20260810-12 D2 起就在 `view.go:1191` 下发
   * `death_reveal_delay_min`，且注释声称本组件据此倒计时 —— 但该字段在
   * 前端从无任何引用（§130 反向复现）。本次补齐消费者。
   *
   * §135 无关：终局后身份**已经**是公开的（`RolePubliclyRevealed` 白名单第 1 条），
   * 这里纯粹是 UI 层的戏剧性延迟，不改变任何服务端脱敏判定。
   */
  deathRevealDelayMin?: number;
  /** §20260811-02 U2 — 观战者标识：只有观战者可见「⚡ 立即揭晓」按钮。 */
  spectator?: boolean;
  /** §20260811-07 U2 — 自动高光集锦战报（仅狼人杀终局时下发）。 */
  battleReportHighlights?: BattleReportHighlight[];
}

/**
 * SettlementModal —— 单局结束后的结算弹层。
 * 纯展示，所有数字由后端计算后通过 game.over / game.settlement 帧下发。
 *
 * 视觉处方（docs/狼人杀13人局金币系统设计.md §11）：
 *   - win  → 金色 + 金币 fly-in + 光晕脉冲 + netGain 显 +N（庆祝）
 *   - lose → 暗红 / 低饱和（克制，无粒子）
 *   - draw → 中性蓝灰（平静，无特效）
 * 所有动画尊重 prefers-reduced-motion（CSS 内降级为纯淡入）。
 */
export function SettlementModal({
  result,
  ante,
  netGain,
  streakBonus,
  platformFee,
  finalBalance,
  winner,
  gameKind,
  onClose,
  onFastRestart,
  deathRevealDelayMin,
  spectator = false,
  battleReportHighlights,
}: SettlementModalProps) {
  const t = useT();
  const lang = useI18nStore((s) => s.lang);

  // §20260811-02 U2 — 死者身份延时揭晓倒计时（秒）。
  // deathRevealDelayMin 未下发 / 为 0 时整段不渲染（后端 omitempty）。
  const [revealLeftSec, setRevealLeftSec] = useState<number>(
    deathRevealDelayMin && deathRevealDelayMin > 0 ? deathRevealDelayMin * 60 : 0,
  );
  useEffect(() => {
    if (revealLeftSec <= 0) return;
    const id = setInterval(() => setRevealLeftSec((s) => (s > 0 ? s - 1 : 0)), 1000);
    return () => clearInterval(id);
  }, [revealLeftSec > 0]);

  const revealMmSs = `${String(Math.floor(revealLeftSec / 60)).padStart(2, '0')}:${String(
    revealLeftSec % 60,
  ).padStart(2, '0')}`;

  const titleKey =
    result === 'win'
      ? 'settle.win'
      : result === 'lose'
        ? 'settle.lose'
        : 'settle.draw';

  const netCls =
    netGain > 0 ? 'amount--gain' : netGain < 0 ? 'amount--loss' : 'amount--zero';
  const netSign = netGain > 0 ? '+' : netGain < 0 ? '' : '±';

  // 狼人杀阵营胜负标语（仅狼人杀 + winner 为已知阵营时展示）。
  const factionKey =
    gameKind === 'werewolf' && (winner === 'wolf' || winner === 'good')
      ? winner === 'wolf'
        ? 'settle.wolfWin'
        : 'settle.goodWin'
      : null;

  return createPortal(
    <div className="settlement-overlay" onClick={onClose}>
      <div
        className={'settlement-modal settlement-modal--' + result}
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-label={t('settle.title' as TKey)}
      >
        {/* 金币 fly-in 装饰层（仅 win）—— 非交互、对屏幕阅读器隐藏。 */}
        {result === 'win' && (
          <div className="settlement-coins" aria-hidden="true">
            {Array.from({ length: 6 }).map((_, i) => (
              <span key={i} className={'settlement-coin settlement-coin--' + i}>
                🪙
              </span>
            ))}
          </div>
        )}

        <div className="settlement-modal__header">
          <span className={'settlement-result settlement-result--' + result}>
            {t(titleKey as TKey)}
          </span>
          {factionKey && (
            <div className="settlement-faction">{t(factionKey as TKey)}</div>
          )}
          <div className="settlement-game">🎮 {t((`${gameKind}.title` as unknown) as TKey)}</div>
        </div>

        <div className="settlement-modal__body">
          {/* 视觉焦点：本局净收益（大号 + 主题色 + 金币图标 + 明确符号）。 */}
          <div className={'settlement-hero settlement-hero--' + result}>
            <span className="settlement-hero__label">{t('settle.netGain' as TKey)}</span>
            <span className={'settlement-hero__amount ' + netCls}>
              <span className="settlement-hero__coin" aria-hidden="true">🪙</span>
              {netSign}
              {formatBalance(netGain, lang)}
            </span>
          </div>

          <div className="settlement-row">
            <span className="settlement-row__k">{t('settle.ante' as TKey)}</span>
            <span className="settlement-row__v">{ante}</span>
          </div>

          {streakBonus != null && streakBonus > 0 && (
            <div className="settlement-row settlement-row--bonus">
              <span className="settlement-row__k">{t('settle.streakBonus' as TKey)}</span>
              <span className="settlement-row__v amount--gain">
                +{formatBalance(streakBonus, lang)}
              </span>
            </div>
          )}

          {platformFee != null && platformFee > 0 && (
            <div className="settlement-row settlement-row--fee">
              <span className="settlement-row__k">{t('settle.platformFee' as TKey)}</span>
              <span className="settlement-row__v amount--loss">-{platformFee}</span>
            </div>
          )}

          {finalBalance != null && (
            <>
              <div className="settlement-divider" />
              <div className="settlement-row settlement-row--balance">
                <span className="settlement-row__k">{t('settle.finalBalance' as TKey)}</span>
                <span className={'settlement-row__v settlement-balance settlement-balance--' + result}>
                  💰 {formatBalance(finalBalance, lang)}
                </span>
              </div>
            </>
          )}
        </div>

        {/* §20260811-02 U2 — 死者身份终局延时揭晓倒计时。
            倒计时结束后整段消失（身份在其它面板正常揭晓）。
            「⚡ 立即揭晓」仅观战者可见 —— 玩家不应能跳过复盘悬念。 */}
        {revealLeftSec > 0 && (
          <div className="settlement-reveal" data-testid="settlement-reveal-countdown">
            <span className="settlement-reveal__text">
              {t('werewolf.settlement.revealCountdown' as TKey).replace('{time}', revealMmSs)}
            </span>
            {spectator && (
              <button
                type="button"
                className="settlement-reveal__now"
                onClick={() => setRevealLeftSec(0)}
              >
                {t('werewolf.settlement.revealNow' as TKey)}
              </button>
            )}
          </div>
        )}

        {/* §20260811-07 U2 — 自动高光集锦战报。
            仅狼人杀终局时由后端 view.go 下发 battle_report_highlights[]，
            渲染 3 张大卡片 + 折叠剩余。 */}
        {gameKind === 'werewolf' && battleReportHighlights && battleReportHighlights.length > 0 && (
          <BattleReportHighlights highlights={battleReportHighlights} />
        )}

        {/* §20260811-01 U2 — 即刻原班重开按钮（仅狼人杀） */}
        {onFastRestart && (
          <button
            type="button"
            className="btn btn-primary settlement-continue"
            onClick={onFastRestart}
            style={{ marginBottom: 8 }}
          >
            ⚡ 即刻原班重开
          </button>
        )}
        <button type="button" className={`btn ${onFastRestart ? 'btn-secondary' : 'btn-primary'} settlement-continue`} onClick={onClose}>
          {onFastRestart ? '查看房间' : t('settle.continue' as TKey)}
        </button>
      </div>
    </div>,
    document.body,
  );
}
