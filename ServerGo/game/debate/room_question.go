// Package debate — 观众提问队列(§20260831-06)。
//
// 设计依据:
//   - docs/辩论比赛/01-辩论比赛游戏流程设计.md §6.1:观众「可向裁判 Agent 提问
//     (裁判可选择性回应)」。
//   - docs/辩论比赛/03-辩论比赛房间创建与配置设计.md §6.2:提问权限。
//   - docs/辩论比赛/06-辩论比赛公平性与评审系统设计.md §5:评审异常处理。
//
// 首期(§20260831-01)只实现了「提问广播给观众」;本期补全闭环:
//
//	观众发 debate.spectator_question 帧
//	  → ws 层调 DebateRoom.AddSpectatorQuestion 入队(≤ 20 条环形上限)
//	  → 评审阶段裁判 prompt 注入未回答提问(§02 §3.5)
//	  → 裁判可选调 answer_spectator 工具回答
//	  → DebateRoom.AnswerSpectatorQuestion 写回答案 + emitSpectatorAnswer
//	  → ws 广播 debate.spectator_answer 帧给全体观众
package debate

import (
	"errors"
)

// 提问队列约束。
const (
	// maxSpectatorQuestions 单房间提问环形上限(超过丢弃最旧未回答项)。
	maxSpectatorQuestions = 20

	// maxSpectatorQuestionChars 单条提问字数上限(与前端面板一致)。
	maxSpectatorQuestionChars = 200
)

// ErrQuestionTooLong 提问超长。
var ErrQuestionTooLong = errors.New("debate: spectator question too long")

// ErrQuestionRejected 房间状态不允许提问(已结束)。
var ErrQuestionRejected = errors.New("debate: spectator question rejected")

// ErrQuestionNotFound 提问 ID 不存在。
var ErrQuestionNotFound = errors.New("debate: spectator question not found")

// AddSpectatorQuestion 把一条观众提问写入房间队列。
//
// 约束:
//   - 文本非空且 ≤ 200 字(rune 计);
//   - 房间未进入 PhaseGameOver(结束后的提问无意义);
//   - 队列满 20 条时优先淘汰最旧的「未回答」提问;全部已回答则淘汰最旧。
//
// 返回入队后的完整记录(含生成的 ID),供 ws 层广播。
func (r *DebateRoom) AddSpectatorQuestion(userID, text string) (SpectatorQuestion, error) {
	text = TruncateRune(text, maxSpectatorQuestionChars)
	if CountRune(text) == 0 {
		return SpectatorQuestion{}, ErrQuestionRejected
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.currentPhase == PhaseGameOver {
		return SpectatorQuestion{}, ErrQuestionRejected
	}

	q := SpectatorQuestion{
		ID:            "q_" + fmtInt(len(r.spectatorQuestions)+1) + "_" + fmtInt(r.questionSeq),
		UserID:        userID,
		Text:          text,
		TimestampMS:   WallNowMS(),
		AnswerJudgeID: -1,
	}
	r.questionSeq++

	// 环形上限:优先丢最旧未回答;全已回答则丢最旧
	if len(r.spectatorQuestions) >= maxSpectatorQuestions {
		dropIdx := -1
		for i, item := range r.spectatorQuestions {
			if item.Answer == "" {
				dropIdx = i
				break
			}
		}
		if dropIdx < 0 {
			dropIdx = 0
		}
		r.spectatorQuestions = append(r.spectatorQuestions[:dropIdx], r.spectatorQuestions[dropIdx+1:]...)
	}
	r.spectatorQuestions = append(r.spectatorQuestions, q)
	return q, nil
}

// SpectatorQuestions 返回提问队列快照(按时间序)。
func (r *DebateRoom) SpectatorQuestions() []SpectatorQuestion {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]SpectatorQuestion, len(r.spectatorQuestions))
	copy(out, r.spectatorQuestions)
	return out
}

// UnansweredSpectatorQuestions 返回未回答提问(供裁判 prompt 注入)。
func (r *DebateRoom) UnansweredSpectatorQuestions() []SpectatorQuestion {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []SpectatorQuestion{}
	for _, q := range r.spectatorQuestions {
		if q.Answer == "" {
			out = append(out, q)
		}
	}
	return out
}

// AnswerSpectatorQuestion 裁判回答指定提问(仅允许回答一次)。
//
// 成功返回更新后的提问记录;提问不存在或已回答返回错误。
// 写回成功后在锁外 emitSpectatorAnswer 外抛广播钩子。
func (r *DebateRoom) AnswerSpectatorQuestion(judgeID int, questionID, answer string) (SpectatorQuestion, error) {
	answer = TruncateRune(answer, 200)
	if CountRune(answer) == 0 {
		return SpectatorQuestion{}, ErrQuestionRejected
	}

	r.mu.Lock()
	idx := -1
	for i, q := range r.spectatorQuestions {
		if q.ID == questionID {
			idx = i
			break
		}
	}
	if idx < 0 {
		r.mu.Unlock()
		return SpectatorQuestion{}, ErrQuestionNotFound
	}
	if r.spectatorQuestions[idx].Answer != "" {
		// 已回答:幂等返回已有记录(不再重复广播)
		existing := r.spectatorQuestions[idx]
		r.mu.Unlock()
		return existing, nil
	}
	r.spectatorQuestions[idx].Answer = answer
	r.spectatorQuestions[idx].AnswerJudgeID = judgeID
	r.spectatorQuestions[idx].AnsweredAtMS = WallNowMS()
	updated := r.spectatorQuestions[idx]
	r.mu.Unlock()

	r.emitSpectatorAnswer(updated)
	return updated, nil
}
