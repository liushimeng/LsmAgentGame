/**
 * 辩论房间创建弹窗 (2026-08-31 §20260831-01)
 *
 * 对齐 docs/辩论比赛/04 §2.2 创建房间弹窗设计。
 *
 * 简化策略:
 *   - 辩题池下拉(内置 30+ 题)
 *   - 模式单选(双队/三队/四队/五队)
 *   - 阶段参数预设(快速/标准/深度)
 *   - 自动分配模型(默认)
 */
import { useEffect, useState } from 'react';
import { debateService } from '@/api/debate';
import type {
  DebateCreateRoomRequest,
  DebateMode,
  DebatePhaseConfig,
  DebateTopic,
} from '@/types/debate';
import { reportGlobalError } from '@/services/globalError';

interface Props {
  onClose: () => void;
  onCreated: (roomId: string) => void;
}

type Preset = 'quick' | 'standard' | 'deep' | 'custom';

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

export default function DebateRoomCreateModal({ onClose, onCreated }: Props) {
  const [topics, setTopics] = useState<DebateTopic[]>([]);
  const [topicId, setTopicId] = useState('');
  const [customTopic, setCustomTopic] = useState('');
  const [mode, setMode] = useState<DebateMode>('two_team');
  const [preset, setPreset] = useState<Preset>('standard');
  const [phaseConfig, setPhaseConfig] = useState<DebatePhaseConfig>(PRESET_CONFIGS.standard!);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');

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

  const handleCreate = () => {
    setErr('');
    if (!topicId && !customTopic) {
      setErr('请选择辩题或输入自定义辩题');
      return;
    }

    const req: DebateCreateRoomRequest = {
      mode,
      agent_assignment: 'auto',
      phase_config: phaseConfig,
      spectator_config: {
        allow_chat: true,
        reveal_agent_thought: true,
        allow_spectator_question: true,
        show_score_realtime: false,
        show_model_name: true,
      },
    };
    if (topicId) {
      req.topic_id = topicId;
    } else if (customTopic) {
      req.topic_text = customTopic;
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
      <div className="modal-card" onClick={(e) => e.stopPropagation()}>
        <header className="modal-header">
          <h2>创建辩论房间</h2>
          <button className="modal-close" onClick={onClose}>×</button>
        </header>

        <div className="modal-body">
          {/* 辩题选择 */}
          <fieldset>
            <legend>① 辩题</legend>
            <div className="form-row">
              <label>
                <input
                  type="radio"
                  name="topic-source"
                  checked={!!topicId}
                  onChange={() => setCustomTopic('')}
                />
                从辩题池选择
              </label>
              <select
                value={topicId}
                onChange={(e) => {
                  setTopicId(e.target.value);
                  setCustomTopic('');
                }}
                disabled={!!customTopic}
              >
                {topics.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.text} ({t.type})
                  </option>
                ))}
              </select>
            </div>
            <div className="form-row">
              <label>
                <input
                  type="radio"
                  name="topic-source"
                  checked={!!customTopic}
                  onChange={() => setTopicId('')}
                />
                自定义辩题
              </label>
              <input
                type="text"
                placeholder="输入自定义辩题文本..."
                value={customTopic}
                onChange={(e) => {
                  setCustomTopic(e.target.value);
                  setTopicId('');
                }}
                disabled={!!topicId}
              />
            </div>
          </fieldset>

          {/* 模式 */}
          <fieldset>
            <legend>② 辩论模式</legend>
            <div className="radio-group">
              {[
                { v: 'two_team', l: '双队对抗 (2队×4人)' },
                { v: 'two_team' as DebateMode, l: '双队精简 (2队×3人)' },
                { v: 'three_team', l: '三队辩论 (3队×3人)' },
                { v: 'four_team', l: 'BP 制 (4队×2人)' },
                { v: 'five_team', l: '五队发散 (5队×2人)' },
              ].map((m, i) => (
                <label key={i}>
                  <input
                    type="radio"
                    name="mode"
                    value={m.v}
                    checked={mode === m.v}
                    onChange={(e) => setMode(e.target.value as DebateMode)}
                  />
                  {m.l}
                </label>
              ))}
            </div>
          </fieldset>

          {/* 阶段参数 */}
          <fieldset>
            <legend>③ 阶段参数</legend>
            <div className="form-row">
              <label>预设:</label>
              <select
                value={preset}
                onChange={(e) => setPreset(e.target.value as Preset)}
              >
                <option value="quick">快速 (~10 分钟)</option>
                <option value="standard">标准 (~25 分钟)</option>
                <option value="deep">深度 (~45 分钟)</option>
                <option value="custom">自定义</option>
              </select>
            </div>
            <div className="form-grid">
              <label>立论时长:{phaseConfig.opening_argument_sec}秒</label>
              <label>驳论时长:{phaseConfig.rebuttal_sec}秒</label>
              <label>质询时长:{phaseConfig.cross_exam_sec}秒</label>
              <label>自由辩时长:{phaseConfig.free_debate_sec}秒</label>
              <label>总结时长:{phaseConfig.closing_argument_sec}秒</label>
            </div>
          </fieldset>

          {/* 模型分配说明 */}
          <fieldset>
            <legend>④ 模型分配</legend>
            <div className="info-line">
              ● 自动分配(推荐) — 系统从 8 个可用模型中公平分配给辩方和裁判
            </div>
          </fieldset>

          {/* 观众设置 */}
          <fieldset>
            <legend>⑤ 观众设置</legend>
            <div className="checkbox-group">
              <label><input type="checkbox" defaultChecked /> 允许观众聊天</label>
              <label><input type="checkbox" defaultChecked /> 展示 Agent 思考过程</label>
              <label><input type="checkbox" /> 实时显示评分</label>
            </div>
          </fieldset>

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