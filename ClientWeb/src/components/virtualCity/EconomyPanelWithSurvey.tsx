/**
 * EconomyPanelWithSurvey — 批次 69 拆分（原 VirtualCityGamePage 内联组件）。
 *
 * 阶段 Q 遗留：经济 Tab 内含调研入口 —— EconomyPanel + 顶部「📋 调研」小按钮，
 * 点击触发 SurveyPanel 模态（避免占独立 Tab）。
 *
 * 批次 69「实时看板统一重构」：对局页不再直接渲染业务面板（全部收进
 * VirtualCityDashboard），本组件随之从页面文件里搬出，便于看板内引用。
 */

import { useState } from 'react';
import type { VirtualCityAction, VirtualCityGameState } from '@/types/virtualCity';
import { EconomyPanel } from '@/components/virtualCity/EconomyPanel';
import { SurveyPanel } from '@/components/virtualCity/SurveyPanel';

export interface EconomyPanelWithSurveyProps {
  gameState: VirtualCityGameState | null;
  roomId: string;
  onFamilyAction: (action: VirtualCityAction) => void;
  spectator: boolean;
}

export function EconomyPanelWithSurvey({
  gameState,
  roomId,
  onFamilyAction,
  spectator,
}: EconomyPanelWithSurveyProps) {
  const [surveyOpen, setSurveyOpen] = useState(false);
  return (
    <div className="virtualCity-economy-wrap">
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', marginBottom: 4 }}>
        <button
          type="button"
          className="virtualCity-tab-extra-btn"
          onClick={() => setSurveyOpen(true)}
          aria-label="打开调研"
        >
          📋 调研
        </button>
      </div>
      <EconomyPanel gameState={gameState} onFamilyAction={onFamilyAction} spectator={spectator} />
      {surveyOpen && (
        <div
          className="virtualCity-modal-overlay"
          role="dialog"
          aria-modal="true"
          onClick={() => setSurveyOpen(false)}
        >
          {/* 18/04 AB-2：改统一 virtualCity-modal 三段结构（head sticky / body 内滚），
              去掉内联 maxWidth/maxHeight —— 尺寸契约收口到 virtualCity-city3d.css。 */}
          <div
            className="virtualCity-modal"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="virtualCity-modal__head">
              <strong>📋 调研</strong>
              <button
                type="button"
                className="virtualCity-tab-extra-btn"
                onClick={() => setSurveyOpen(false)}
              >
                ✕ 关闭
              </button>
            </div>
            <div className="virtualCity-modal__body">
              <SurveyPanel roomId={roomId} gameState={gameState} />
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default EconomyPanelWithSurvey;
