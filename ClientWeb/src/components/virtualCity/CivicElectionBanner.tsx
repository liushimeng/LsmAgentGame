/**
 * CivicElectionBanner — 市长选举当选横幅（批次 20 文档 3 A4 / FE-2 F2-5）。
 *
 * 挂在房间页顶栏下方：市长席位 + 姓名 + 任期进度条（48 月一届）+ 下届选举月。
 * 渲染门控：election_enabled=false 或 mayor_seat=-1（尚无市长）→ 整条不渲染。
 * 可手动关闭：dismiss 按「届」记账（key = mayor_seat:next_election_month），
 * 下一届选举产生（next_election_month 前进 48）即自动重现，无需持久化。
 * 津贴停发徽标：直接消费后端权威字段 public_services.stipend_stopped（批次 20
 * 收口：false 时 omitempty 不下发）。旧实现是在当月 policy 事件流里探测中文文本
 * 「津贴停发」—— best-effort 且依赖文案，已删除。
 */

import { useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  WEALTH_ELECTION_INTERVAL_MONTHS,
  virtualCityElectionTermElapsed,
  type VirtualCityPublicServices,
} from '@/types/virtualCity';
import './virtualCity-batch20.css';

interface Props {
  /** game.state.public_services（选举段；缺省/未启用 = 不渲染）。 */
  election?: VirtualCityPublicServices | null;
  /** 房间当前月（game.state.month，任期进度分子）。 */
  month: number;
  /** 现任市长昵称（players 查表；缺省时只显示座位号）。 */
  mayorNickname?: string;
}

export function CivicElectionBanner({ election, month, mayorNickname }: Props) {
  const t = useT();
  const [dismissedKey, setDismissedKey] = useState<string | null>(null);

  const enabled = !!election?.election_enabled;
  const mayorSeat = election?.mayor_seat ?? -1;
  if (!enabled || mayorSeat < 0) return null;

  // 后端显式下发 true 才算停发（omitempty：false 不下发 → undefined）。
  const stipendStopped = election?.stipend_stopped === true;

  const next = election?.next_election_month ?? 0;
  const termElapsed = virtualCityElectionTermElapsed(month, next);
  const termPct = Math.min(100, Math.round((termElapsed / WEALTH_ELECTION_INTERVAL_MONTHS) * 100));

  const bannerKey = `${mayorSeat}:${next}`;
  if (dismissedKey === bannerKey) return null;

  return (
    <div className="virtualCity-election-banner" role="status" data-testid="virtualCity-election-banner">
      <span className="virtualCity-election-banner__title">🗳 {t('virtualCity.election.title' as TKey)}</span>
      <span data-testid="virtualCity-election-mayor">
        {t('virtualCity.election.mayor' as TKey, {
          seat: mayorSeat + 1,
          name: mayorNickname ? ` · ${mayorNickname}` : '',
        })}
      </span>
      <span className="virtualCity-election-banner__term" title={t('virtualCity.election.termProgress' as TKey, { elapsed: termElapsed, interval: WEALTH_ELECTION_INTERVAL_MONTHS })}>
        <span
          className="virtualCity-election-banner__track"
          role="progressbar"
          aria-valuenow={termPct}
          aria-valuemin={0}
          aria-valuemax={100}
        >
          <span className="virtualCity-election-banner__fill" style={{ width: `${termPct}%` }} aria-hidden="true" />
        </span>
        {termPct}%
      </span>
      {next > 0 && (
        <span>{t('virtualCity.election.nextElection' as TKey, { m: next })}</span>
      )}
      {stipendStopped && (
        <span className="virtualCity-election-banner__stipend" data-testid="virtualCity-election-stipend">
          ⚠ {t('virtualCity.election.stipendStopped' as TKey)}
        </span>
      )}
      <button
        type="button"
        className="virtualCity-election-banner__close"
        onClick={() => setDismissedKey(bannerKey)}
        aria-label={t('virtualCity.election.bannerClose' as TKey)}
        data-testid="virtualCity-election-banner-close"
      >
        ✕ {t('virtualCity.election.bannerClose' as TKey)}
      </button>
    </div>
  );
}

export default CivicElectionBanner;
