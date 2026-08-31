/**
 * 辩论房间创建弹窗 (2026-08-31 §20260831-01 / §20260831-02)
 *
 * 对齐 docs/辩论比赛/04 §2.2 创建房间弹窗设计。
 * §20260831-02 界面优化(tmpPlan/辩论房间创建弹窗-界面优化-20260831-02.md):
 *   - 弹窗加宽至 960px(.debate-create-modal 作用域,不动 .modal-card 基类)
 *   - 双栏响应式布局(≥900px 双栏, <900px 单栏),消除留白
 *   - 封装 RadioOption / SectionFieldset 展示控件,单选按钮与说明文字稳定一行
 *   - 观众设置 checkbox 接线到 spectator_config(修复§130「声明了却从不接线」)
 *   - 辩题来源改为显式 topicSource 状态机;阶段参数区新增整局预计耗时
 *
 * 简化策略:
 *   - 辩题池下拉(内置 30+ 题)
 *   - 模式单选(双队/三队/四队/五队)
 *   - 阶段参数预设(快速/标准/深度)
 *   - 自动分配模型(默认)
 */
import { useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { debateService } from '@/api/debate';
import type {
  DebateCreateRoomRequest,
  DebateMode,
  DebatePhaseConfig,
  DebateSpectatorConfig,
  DebateTopic,
} from '@/types/debate';
import { reportGlobalError } from '@/services/globalError';

interface Props {
  onClose: () => void;
  onCreated: (roomId: string) => void;
}

type Preset = 'quick' | 'standard' | 'deep' | 'custom';
type TopicSource = 'pool' | 'custom';

const PRESET_CONFIGS: Record<Preset, DebatePhaseConfig | null> = {
  quick: {
    preparation_sec: 15,
    opening_argument_sec: 90,
    rebuttal_sec: 60,
    cross_exam_sec: 45,
    cross_exam_summary_sec: 30,
    free_debate_sec: 120,
    closing_argument_sec: 90,
    judging_sec: 30,
    result_show_sec: 20,
    max_speech_chars: 400,
    max_rebuttal_chars: 320,
    max_cross_exam_q_chars: 40,
    max_cross_exam_a_chars: 80,
    max_free_debate_chars: 60,
    max_closing_chars: 480,
  },
  standard: {
    preparation_sec: 30,
    opening_argument_sec: 180,
    rebuttal_sec: 120,
    cross_exam_sec: 90,
    cross_exam_summary_sec: 60,
    free_debate_sec: 240,
    closing_argument_sec: 180,
    judging_sec: 60,
    result_show_sec: 30,
    max_speech_chars: 500,
    max_rebuttal_chars: 400,
    max_cross_exam_q_chars: 50,
    max_cross_exam_a_chars: 100,
    max_free_debate_chars: 80,
    max_closing_chars: 600,
  },
  deep: {
    preparation_sec: 60,
    opening_argument_sec: 300,
    rebuttal_sec: 240,
    cross_exam_sec: 180,
    cross_exam_summary_sec: 120,
    free_debate_sec: 480,
    closing_argument_sec: 300,
    judging_sec: 120,
    result_show_sec: 60,
    max_speech_chars: 700,
    max_rebuttal_chars: 560,
    max_cross_exam_q_chars: 60,
    max_cross_exam_a_chars: 120,
    max_free_debate_chars: 100,
    max_closing_chars: 800,
  },
  custom: null,
};

const MODE_OPTIONS: { value: DebateMode; label: string }[] = [
  { value: 'two_team', label: '双队对抗 (2队×4人)' },
  { value: 'three_team', label: '三队辩论 (3队×3人)' },
  { value: 'four_team', label: 'BP 制 (4队×2人)' },
  { value: 'five_team', label: '五队发散 (5队×2人)' },
];

const DEFAULT_SPECTATOR_CONFIG: DebateSpectatorConfig = {
  allow_chat: true,
  reveal_agent_thought: true,
  allow_spectator_question: true,
  show_score_realtime: false,
  show_model_name: true,
};

/** 单选一行控件:按钮 + 说明文字稳定一行显示,右侧可挂 select/input 等扩展控件 */
function RadioOption({
  name,
  checked,
  onSelect,
  label,
  children,
}: {
  name: string;
  checked: boolean;
  onSelect: () => void;
  label: string;
  children?: ReactNode;
}) {
  return (
    <div className="radio-option-row">
      <label className="radio-option" onClick={onSelect}>
        <input type="radio" name={name} checked={checked} onChange={onSelect} />
        <span>{label}</span>
      </label>
      {children}
    </div>
  );
}

/** 分区外壳:fieldset + 序号徽章 legend */
function SectionFieldset({ index, title, children }: { index: string; title: string; children: ReactNode }) {
  return (
    <fieldset className="debate-create-section">
      <legend>
        <span className="section-badge">{index}</span>
        {title}
      </legend>
      {children}
    </fieldset>
  );
}

export default function DebateRoomCreateModal({ onClose, onCreated }: Props) {
  const [topics, setTopics] = useState<DebateTopic[]>([]);
  const [topicSource, setTopicSource] = useState<TopicSource>('pool');
  const [topicId, setTopicId] = useState('');
  const [customTopic, setCustomTopic] = useState('');
  const [mode, setMode] = useState<DebateMode>('two_team');
  const [preset, setPreset] = useState<Preset>('standard');
  const [phaseConfig, setPhaseConfig] = useState<DebatePhaseConfig>(PRESET_CONFIGS.standard!);
  const [spectatorConfig, setSpectatorConfig] = useState<DebateSpectatorConfig>(DEFAULT_SPECTATOR_CONFIG);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');

  // §20260831-10(P2-1):选中"自定义辩题"radio 后自动聚焦输入框,无需二次点击。
  const customTopicRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (topicSource === 'custom' && customTopicRef.current) {
      customTopicRef.current.focus();
    }
  }, [topicSource]);

  useEffect(() => {
    debateService
      .listTopics()
      .then((ts) => {
        setTopics(ts ?? []);
        if ((ts ?? []).length > 0) {
          setTopicId(ts[0].id);
        }
      })
      .catch((e) => reportGlobalError({ message: e.message, severity: 'error' }));
  }, []);

  useEffect(() => {
    const cfg = PRESET_CONFIGS[preset];
    if (cfg) setPhaseConfig(cfg);
  }, [preset]);

  /** 整局预计耗时:全部阶段秒数求和 → 分钟(§20260831-02 P6) */
  const totalMinutes = useMemo(() => {
    const c = phaseConfig;
    const sec =
      c.preparation_sec +
      c.opening_argument_sec +
      c.rebuttal_sec +
      c.cross_exam_sec +
      c.cross_exam_summary_sec +
      c.free_debate_sec +
      c.closing_argument_sec +
      c.judging_sec +
      c.result_show_sec;
    return Math.round(sec / 60);
  }, [phaseConfig]);

  const toggleSpectator = (key: keyof DebateSpectatorConfig) => {
    setSpectatorConfig((prev) => ({ ...prev, [key]: !prev[key] }));
  };

  const handleCreate = () => {
    setErr('');
    const usePool = topicSource === 'pool';
    if (usePool && !topicId) {
      setErr('请从辩题池选择一个辩题');
      return;
    }
    if (!usePool && !customTopic.trim()) {
      setErr('请输入自定义辩题文本');
      return;
    }

    const req: DebateCreateRoomRequest = {
      mode,
      agent_assignment: 'auto',
      phase_config: phaseConfig,
      spectator_config: spectatorConfig,
    };
    if (usePool) {
      req.topic_id = topicId;
    } else {
      req.topic_text = customTopic.trim();
      req.topic_type = 'custom';
    }

    setLoading(true);
    debateService
      .createRoom(req)
      .then((res) => {
        setLoading(false);
        onCreated(res.room_id);
      })
      .catch((e: Error) => {
        setLoading(false);
        setErr(e.message);
        reportGlobalError({ message: e.message, severity: 'error' });
      });
  };

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-card debate-create-modal" onClick={(e) => e.stopPropagation()}>
        <header className="modal-header">
          <h2>创建辩论房间</h2>
          <button className="modal-close" onClick={onClose}>×</button>
        </header>

        <div className="modal-body debate-create-body">
          {/* 辩题选择(整行) */}
          <SectionFieldset index="①" title="辩题">
            <RadioOption
              name="topic-source"
              checked={topicSource === 'pool'}
              onSelect={() => {
                setTopicSource('pool');
                if (!topicId && topics.length > 0) setTopicId(topics[0].id);
              }}
              label="从辩题池选择"
            >
              <select
                className="debate-create-flex-input"
                value={topicId}
                onChange={(e) => setTopicId(e.target.value)}
                disabled={topicSource !== 'pool'}
              >
                {topics.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.text} ({t.type})
                  </option>
                ))}
              </select>
            </RadioOption>
            <RadioOption
              name="topic-source"
              checked={topicSource === 'custom'}
              onSelect={() => setTopicSource('custom')}
              label="自定义辩题"
            >
              <input
                ref={customTopicRef}
                className="debate-create-flex-input"
                type="text"
                placeholder="输入自定义辩题文本..."
                value={customTopic}
                onChange={(e) => setCustomTopic(e.target.value)}
                disabled={topicSource !== 'custom'}
              />
            </RadioOption>
          </SectionFieldset>

          <div className="debate-create-grid">
            {/* 模式 */}
            <SectionFieldset index="②" title="辩论模式">
              <div className="mode-grid">
                {MODE_OPTIONS.map((m) => (
                  <label key={m.value} className="radio-option">
                    <input
                      type="radio"
                      name="mode"
                      value={m.value}
                      checked={mode === m.value}
                      onChange={(e) => setMode(e.target.value as DebateMode)}
                    />
                    <span>{m.label}</span>
                  </label>
                ))}
              </div>
            </SectionFieldset>

            {/* 阶段参数 */}
            <SectionFieldset index="③" title="阶段参数">
              <div className="preset-row">
                <label className="preset-label" htmlFor="debate-preset">预设:</label>
                <select
                  id="debate-preset"
                  className="debate-create-flex-input"
                  value={preset}
                  onChange={(e) => setPreset(e.target.value as Preset)}
                >
                  <option value="quick">快速 (~10 分钟)</option>
                  <option value="standard">标准 (~25 分钟)</option>
                  <option value="deep">深度 (~45 分钟)</option>
                  <option value="custom">自定义</option>
                </select>
              </div>
              <div className="phase-summary">
                <span>立论 {phaseConfig.opening_argument_sec}s</span>
                <span>驳论 {phaseConfig.rebuttal_sec}s</span>
                <span>质询 {phaseConfig.cross_exam_sec}s</span>
                <span>质询小结 {phaseConfig.cross_exam_summary_sec}s</span>
                <span>自由辩 {phaseConfig.free_debate_sec}s</span>
                <span>总结 {phaseConfig.closing_argument_sec}s</span>
              </div>
              <div className="phase-total">整局预计耗时 ≈ {totalMinutes} 分钟</div>
            </SectionFieldset>

            {/* 模型分配说明 */}
            <SectionFieldset index="④" title="模型分配">
              <div className="info-line">
                ● 自动分配(推荐) — 系统从 8 个可用模型中公平分配给辩方和裁判
              </div>
            </SectionFieldset>

            {/* 观众设置(已接线 spectator_config) */}
            <SectionFieldset index="⑤" title="观众设置">
              <div className="spectator-checks">
                <label className="check-option">
                  <input
                    type="checkbox"
                    checked={spectatorConfig.allow_chat}
                    onChange={() => toggleSpectator('allow_chat')}
                  />
                  <span>允许观众聊天</span>
                </label>
                <label className="check-option">
                  <input
                    type="checkbox"
                    checked={spectatorConfig.reveal_agent_thought}
                    onChange={() => toggleSpectator('reveal_agent_thought')}
                  />
                  <span>展示 Agent 思考过程</span>
                </label>
                <label className="check-option">
                  <input
                    type="checkbox"
                    checked={spectatorConfig.allow_spectator_question}
                    onChange={() => toggleSpectator('allow_spectator_question')}
                  />
                  <span>允许观众提问</span>
                </label>
                <label className="check-option">
                  <input
                    type="checkbox"
                    checked={spectatorConfig.show_score_realtime}
                    onChange={() => toggleSpectator('show_score_realtime')}
                  />
                  <span>实时显示评分</span>
                </label>
              </div>
            </SectionFieldset>
          </div>

          {err && <div className="form-error">{err}</div>}
        </div>

        <footer className="modal-footer">
          <button className="btn-secondary" onClick={onClose}>取消</button>
          <button
            className="btn-primary"
            onClick={handleCreate}
            disabled={loading}
          >
            {loading ? '创建中...' : '创建房间'}
          </button>
        </footer>
      </div>
    </div>
  );
}
