/**
 * SurveyPanel — 社会调研面板（P1 第二期，tab `survey`）。
 *
 * 契约：docs/财商流游戏/已实现/05-P1扩展/财商流游戏-P1-社会调研系统-v1.md §7。
 * 面向全体 Agent（模型玩家）的预测模拟：人类发起问题 → 存活 bot 在月度决策
 * 上下文中 answer_survey（零额外 LLM 成本）→ 聚合 → game.survey_result 广播。
 *
 * 四块：
 *   ① 发起表单：question（≤100 字）+ options 2-6 动态行；观战者也可发起
 *      （HTTP 与座位无关，仅登录态 + 房间 playing 校验）。
 *   ② 进行中（open ≤1）：问题 + 选项 + 作答进度（answers_count / 存活 bot 数）+ 截止月。
 *   ③ 最近 closed：结果水平条形图（percents，选项文本 + 票数）+ TopReasons 引用块。
 *   ④ 历史列表：最近 4 个 closed（问题 + 理由摘录）。
 *
 * 数据：挂载时 GET /api/games/wealth/rooms/:id/surveys 全量 + store.surveys
 * （game.state 种子 / game.survey_result 增量，useWealth.mergeSurvey 按 id 去重覆盖）。
 * 错误展示（CLAUDE.md §7.1）：表单内联红条优先（不关表单），未知/网络错误兜底
 * reportGlobalError 全局 toast；会话过期走 AuthModal 通道不重复展示。
 * 条形图：纯 CSS 横条零依赖，单色（#60a5fa）+ 直接标注百分比，不单靠颜色。
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { ApiError, isSessionExpiredError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import { fetchWealthSurveys, launchWealthSurvey } from '@/api/wealth';
import { useWealthStore } from '@/store/wealth.store';
import { formatPct, type WealthGameState, type WealthSurvey } from '@/types/wealth';

interface Props {
  roomId: string;
  gameState: WealthGameState | null;
}

/** 已知调研错误码 → i18n key（服务端 errcode 35016-35019 / 35002 / 35010）。 */
function surveyErrorKey(code: number): TKey | null {
  switch (code) {
    case 35016: return 'wealth.survey.invalidOptions';
    case 35017: return 'wealth.survey.limitOpen';
    case 35018: return 'wealth.survey.limitMonth';
    case 35002: return 'wealth.survey.notPlaying';
    case 35010: return 'wealth.survey.disabled';
    default: return null;
  }
}

/** 发起表单（观战者可用；仅 playing 窗口开放）。 */
function LaunchForm({
  roomId,
  playing,
  openExists,
  onLaunched,
}: {
  roomId: string;
  playing: boolean;
  openExists: boolean;
  onLaunched: (survey: WealthSurvey) => void;
}) {
  const t = useT();
  const [question, setQuestion] = useState('');
  const [options, setOptions] = useState<string[]>(['', '']);
  const [err, setErr] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const setOption = (i: number, v: string) => {
    setOptions((prev) => prev.map((x, idx) => (idx === i ? v : x)));
  };
  const addOption = () => {
    if (options.length >= 6) return;
    setOptions((prev) => [...prev, '']);
  };
  const removeOption = (i: number) => {
    if (options.length <= 2) return;
    setOptions((prev) => prev.filter((_, idx) => idx !== i));
    setErr(null);
  };

  const submit = useCallback(async () => {
    const q = question.trim();
    const opts = options.map((o) => o.trim());
    if (!q) {
      setErr(t('wealth.survey.questionRequired' as TKey));
      return;
    }
    if (opts.length < 2 || opts.length > 6 || opts.some((o) => !o)) {
      setErr(t('wealth.survey.invalidOptions' as TKey));
      return;
    }
    setErr(null);
    setSubmitting(true);
    try {
      const r = await launchWealthSurvey(roomId, q, opts);
      onLaunched(r.survey);
      setQuestion('');
      setOptions(['', '']);
    } catch (e) {
      if (isSessionExpiredError(e)) return; // AuthModal 通道已接手
      const key = e instanceof ApiError ? surveyErrorKey(e.code) : null;
      const msg = key ? t(key) : (e instanceof Error ? e.message : String(e));
      setErr(msg);
      // §7.1 兜底：已知业务校验错误就地展示即可；未知/网络错误补全局 toast。
      if (!key) reportGlobalError(e);
    } finally {
      setSubmitting(false);
    }
  }, [roomId, question, options, t, onLaunched]);

  return (
    <div className="wealth-survey__form">
      <div className="wealth-survey__form-title">{t('wealth.survey.launchTitle' as TKey)}</div>
      <label className="wealth-survey__row">
        <span>{t('wealth.survey.question' as TKey)}</span>
        <input
          type="text"
          maxLength={100}
          value={question}
          onChange={(e) => setQuestion(e.target.value)}
          disabled={submitting}
          placeholder={t('wealth.survey.questionPlaceholder' as TKey)}
          data-testid="wealth-survey-question"
        />
      </label>
      <div className="wealth-survey__options">
        {options.map((o, i) => (
          <div key={i} className="wealth-survey__option-row">
            <span className="wealth-survey__option-index">{String.fromCharCode(65 + i)}</span>
            <input
              type="text"
              maxLength={40}
              value={o}
              onChange={(e) => setOption(i, e.target.value)}
              disabled={submitting}
              placeholder={t('wealth.survey.optionPlaceholder' as TKey, { n: i + 1 })}
            />
            <button
              type="button"
              className="wealth-survey__option-remove"
              onClick={() => removeOption(i)}
              disabled={submitting || options.length <= 2}
              title={t('wealth.survey.removeOption' as TKey)}
              aria-label={t('wealth.survey.removeOption' as TKey)}
            >
              −
            </button>
          </div>
        ))}
        {options.length < 6 && (
          <button
            type="button"
            className="wealth-survey__option-add"
            onClick={addOption}
            disabled={submitting}
          >
            + {t('wealth.survey.addOption' as TKey)}
          </button>
        )}
      </div>
      {!playing && (
        <p className="wealth-survey__hint">{t('wealth.survey.notPlaying' as TKey)}</p>
      )}
      {playing && openExists && (
        <p className="wealth-survey__hint">{t('wealth.survey.limitOpen' as TKey)}</p>
      )}
      <button
        type="button"
        className="btn btn-primary wealth-survey__submit"
        onClick={() => void submit()}
        disabled={submitting || !playing || openExists}
        data-testid="wealth-survey-submit"
      >
        {submitting ? t('common.loading') : t('wealth.survey.submit' as TKey)}
      </button>
      <p className="wealth-survey__hint wealth-survey__hint--dim">
        {t('wealth.survey.spectatorHint' as TKey)}
      </p>
      {err && <div className="wealth-survey__error" role="alert">{err}</div>}
    </div>
  );
}

/** 进行中调研卡：选项列表 + 作答进度 + 截止月。 */
function OpenCard({ survey, aliveBots }: { survey: WealthSurvey; aliveBots: number }) {
  const t = useT();
  const pct = aliveBots > 0 ? Math.min(100, (survey.answers_count / aliveBots) * 100) : 0;
  return (
    <div className="wealth-survey__card wealth-survey__card--open">
      <div className="wealth-survey__card-head">
        <span className="wealth-badge wealth-survey__badge--open">{t('wealth.survey.openTag' as TKey)}</span>
        <span className="wealth-survey__card-id">{survey.id}</span>
        <span className="wealth-survey__card-month">
          {t('wealth.survey.deadline' as TKey, { m: survey.deadline_month })}
        </span>
      </div>
      <p className="wealth-survey__question">{survey.question}</p>
      <ol className="wealth-survey__options-list">
        {survey.options.map((o, i) => (
          <li key={i}>
            <span className="wealth-survey__option-index">{String.fromCharCode(65 + i)}</span>
            {o}
          </li>
        ))}
      </ol>
      <div className="wealth-survey__progress" aria-label={t('wealth.survey.progress' as TKey)}>
        <div className="wealth-survey__progress-head">
          <span>{t('wealth.survey.progress' as TKey)}</span>
          <span>
            {t('wealth.survey.answersCount' as TKey, { n: survey.answers_count })}
            {aliveBots > 0 ? ` / ${aliveBots}` : ''}
          </span>
        </div>
        <div className="wealth-survey__progress-track">
          <div className="wealth-survey__progress-fill" style={{ width: `${pct}%` }} />
        </div>
        <p className="wealth-survey__hint wealth-survey__hint--dim">
          {t('wealth.survey.waitingBots' as TKey)}
        </p>
      </div>
    </div>
  );
}

/** 已关闭调研卡：结果水平条形图（单色 + 直接标注）+ TopReasons 引用块。 */
function ResultCard({ survey, latest }: { survey: WealthSurvey; latest: boolean }) {
  const t = useT();
  const r = survey.result;
  return (
    <div className={'wealth-survey__card' + (latest ? ' wealth-survey__card--latest' : '')}>
      <div className="wealth-survey__card-head">
        <span className="wealth-badge wealth-survey__badge--closed">{t('wealth.survey.closedTag' as TKey)}</span>
        <span className="wealth-survey__card-id">{survey.id}</span>
        <span className="wealth-survey__card-month">
          {t('wealth.survey.launchedAt' as TKey, { m: survey.launch_month })}
        </span>
      </div>
      <p className="wealth-survey__question">{survey.question}</p>
      {r && r.options.length > 0 ? (
        <>
          <div className="wealth-survey__result">
            {r.options.map((o, i) => (
              <div key={i} className="wealth-survey__result-row">
                <span className="wealth-survey__result-label" title={o}>
                  <span className="wealth-survey__option-index">{String.fromCharCode(65 + i)}</span>
                  {o}
                </span>
                <span className="wealth-survey__result-track">
                  <span
                    className="wealth-survey__result-fill"
                    style={{ width: `${Math.max(0.5, (r.percents[i] ?? 0) * 100)}%` }}
                  />
                </span>
                <span className="wealth-survey__result-value">
                  {formatPct(r.percents[i] ?? 0, 0)} · {r.counts[i] ?? 0}
                </span>
              </div>
            ))}
          </div>
          <div className="wealth-survey__total">
            {t('wealth.survey.total' as TKey, { n: r.total })}
          </div>
          {r.top_reasons.length > 0 && (
            <div className="wealth-survey__reasons">
              <div className="wealth-survey__reasons-title">{t('wealth.survey.topReasons' as TKey)}</div>
              {r.top_reasons.map((s, i) => (
                <blockquote key={i} className="wealth-survey__reason">{s}</blockquote>
              ))}
            </div>
          )}
        </>
      ) : (
        <p className="wealth-survey__hint wealth-survey__hint--dim">
          {t('wealth.survey.noAnswers' as TKey)}
        </p>
      )}
    </div>
  );
}

export function SurveyPanel({ roomId, gameState }: Props) {
  const t = useT();
  const surveys = useWealthStore((s) => s.surveys);
  const setSurveys = useWealthStore((s) => s.setSurveys);
  const mergeSurvey = useWealthStore((s) => s.mergeSurvey);
  const [loadErr, setLoadErr] = useState<string | null>(null);

  // 挂载 / 换房：拉取全部历史调研（≤20）；失败就地展示 + 全局 toast（§7.1）。
  useEffect(() => {
    let alive = true;
    setLoadErr(null);
    fetchWealthSurveys(roomId)
      .then((r) => {
        if (alive) setSurveys(r.surveys ?? []);
      })
      .catch((e) => {
        if (!alive || isSessionExpiredError(e)) return;
        setLoadErr(t('wealth.survey.loadFailed' as TKey));
        reportGlobalError(e);
      });
    return () => {
      alive = false;
    };
  }, [roomId, setSurveys, t]);

  const playing = gameState?.status === 'playing';
  // 作答进度分母：存活 bot 数（回答主体是模型玩家；人类在座不能作答，P1）。
  const aliveBots = useMemo(
    () => (gameState?.players ?? []).filter((p) => p.alive && p.is_bot).length,
    [gameState],
  );

  const open = useMemo(
    () => surveys.find((s) => s.status === 'open') ?? null,
    [surveys],
  );
  const closed = useMemo(
    () =>
      surveys
        .filter((s) => s.status === 'closed')
        .sort((a, b) => b.launch_month - a.launch_month || b.id.localeCompare(a.id)),
    [surveys],
  );
  const latestClosed = closed.length > 0 ? closed[0] : null;
  const history = closed.slice(1, 5); // 最近 4 个（除最新已展开的以外）

  return (
    <div className="wealth-survey">
      <div className="wealth-survey__head">
        <span className="wealth-survey__title">{t('wealth.survey.title' as TKey)}</span>
      </div>

      <LaunchForm
        roomId={roomId}
        playing={playing}
        openExists={!!open}
        onLaunched={mergeSurvey}
      />

      {loadErr && <div className="wealth-survey__error" role="alert">{loadErr}</div>}

      {open && <OpenCard survey={open} aliveBots={aliveBots} />}

      {latestClosed && <ResultCard survey={latestClosed} latest />}

      {history.length > 0 && (
        <div className="wealth-survey__history">
          <div className="wealth-survey__history-title">{t('wealth.survey.history' as TKey)}</div>
          {history.map((s) => (
            <details key={s.id} className="wealth-survey__history-item">
              <summary>
                <span className="wealth-survey__card-id">{s.id}</span>
                {s.question}
                {s.result && (
                  <span className="wealth-survey__history-pct">
                    {formatPct(Math.max(...s.result.percents, 0), 0)}
                  </span>
                )}
              </summary>
              {s.result && s.result.top_reasons.length > 0 && (
                <p className="wealth-survey__history-reason">
                  {t('wealth.survey.topReasons' as TKey)}：{s.result.top_reasons[0]}
                </p>
              )}
            </details>
          ))}
        </div>
      )}

      {!open && !latestClosed && !loadErr && (
        <div className="wealth-panel__empty">
          <p>{t('wealth.survey.empty' as TKey)}</p>
        </div>
      )}
    </div>
  );
}
